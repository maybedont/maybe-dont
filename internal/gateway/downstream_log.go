package gateway

import (
	"context"
	"log/slog"
	"regexp"
	"slices"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// urlPattern matches an http or https URL. A URL in double quotes, as Go's
// %q prints it in *url.Error and url.Parse errors, runs to the closing quote,
// with backslash escapes and spaces inside it; the quotes are part of the
// match. A bare URL runs to whitespace, a quote or an angle bracket.
var urlPattern = regexp.MustCompile(`"(?i:https?)://(?:\\.|[^"\\])*"|(?i:https?)://(?:\\.|[^\s"'<>\\])+`)

// redactURLs drops the userinfo, query string and fragment, the parts that
// can hold credentials, from every http or https URL in text, keeping scheme,
// host and path. Errors from net/http quote the request URL with its query,
// and the URL they quote isn't always the configured one (a redirect target,
// an SSE endpoint announced by the server), so logs pass error text through
// here rather than matching known URLs.
func redactURLs(text string) string {
	return urlPattern.ReplaceAllStringFunc(text, func(match string) string {
		if strings.HasPrefix(match, `"`) {
			return `"` + redactURL(match[1:len(match)-1]) + `"`
		}
		return redactURL(match)
	})
}

// redactURL redacts one URL for redactURLs.
func redactURL(rawURL string) string {
	authorityStart := strings.Index(rawURL, "://") + len("://")
	rest := rawURL[authorityStart:]
	if i := strings.IndexAny(rest, "?#"); i >= 0 {
		rest = rest[:i]
	}
	authority, path, _ := strings.Cut(rest, "/")
	if i := strings.LastIndex(authority, "@"); i >= 0 {
		authority = authority[i+1:]
	}
	if path != "" {
		path = "/" + path
	}
	// A path segment may start with "@" (/users/@me, /@org/mcp), and
	// url.String() leaves it unescaped. One inside a segment can be a
	// userinfo with a raw "/" in it (https://user/name:token@host/mcp parses
	// as host "user"), so the path is dropped rather than risk keeping the
	// token.
	for segment := range strings.SplitSeq(path, "/") {
		if strings.LastIndex(segment, "@") > 0 {
			path = ""
			break
		}
	}
	return rawURL[:authorityStart] + authority + path
}

// downstreamErrorField returns err as the "error" log field with URLs
// redacted. Every log of a downstream connect or call error goes through here.
func downstreamErrorField(err error) zap.Field {
	return zap.String("error", redactURLs(err.Error()))
}

// newDownstreamSlogLogger returns the logger handed to mcp-go's http and sse
// transports. Without one they log to slog.Default, outside the gateway's log
// configuration and with raw errors that quote the URL and its token.
func newDownstreamSlogLogger(logger *zap.Logger, clientName string) *slog.Logger {
	return slog.New(&redactingZapHandler{logger: logger.With(zap.String("client", clientName))})
}

// redactingZapHandler is a slog.Handler that writes to a zap logger at the
// matching level, with URLs redacted in the message and every attribute.
type redactingZapHandler struct {
	logger *zap.Logger
	fields []zap.Field // attributes bound with WithAttrs
	group  string      // key prefix from WithGroup, e.g. "retry."
}

func (h *redactingZapHandler) Enabled(_ context.Context, level slog.Level) bool {
	return h.logger.Core().Enabled(downstreamZapLevel(level))
}

func (h *redactingZapHandler) Handle(_ context.Context, record slog.Record) error {
	entry := h.logger.Check(downstreamZapLevel(record.Level), redactURLs(record.Message))
	if entry == nil {
		return nil
	}
	fields := slices.Clone(h.fields)
	record.Attrs(func(attr slog.Attr) bool {
		fields = appendRedactedAttr(fields, h.group, attr)
		return true
	})
	entry.Write(fields...)
	return nil
}

func (h *redactingZapHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	fields := slices.Clone(h.fields)
	for _, attr := range attrs {
		fields = appendRedactedAttr(fields, h.group, attr)
	}
	return &redactingZapHandler{logger: h.logger, fields: fields, group: h.group}
}

func (h *redactingZapHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &redactingZapHandler{logger: h.logger, fields: h.fields, group: h.group + name + "."}
}

// appendRedactedAttr appends attr as string fields, flattening groups into
// dotted keys. Every value is logged as redacted text, since an error or URL
// value can carry a credential.
func appendRedactedAttr(fields []zap.Field, prefix string, attr slog.Attr) []zap.Field {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return fields
	}
	if attr.Value.Kind() == slog.KindGroup {
		if attr.Key != "" {
			prefix += attr.Key + "."
		}
		for _, member := range attr.Value.Group() {
			fields = appendRedactedAttr(fields, prefix, member)
		}
		return fields
	}
	return append(fields, zap.String(prefix+attr.Key, redactURLs(attr.Value.String())))
}

// downstreamZapLevel maps an mcp-go slog level to zap, capped at Warn. mcp-go
// logs expected conditions at Error (a failed session DELETE on every close
// of a client whose downstream has gone away), and the gateway keeps Error
// for unexpected problems.
func downstreamZapLevel(level slog.Level) zapcore.Level {
	switch {
	case level >= slog.LevelWarn:
		return zapcore.WarnLevel
	case level >= slog.LevelInfo:
		return zapcore.InfoLevel
	default:
		return zapcore.DebugLevel
	}
}
