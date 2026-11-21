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

// urlPattern matches an http or https URL up to whitespace, a quote or angle
// bracket. A backslash escape counts as part of the URL, so a URL inside Go's
// %q quoting (as *url.Error prints it) isn't cut short by an escaped quote.
var urlPattern = regexp.MustCompile(`(?i)https?://(?:\\.|[^\s"'<>\\])+`)

// redactURLs drops the userinfo, query string and fragment, the parts that
// can hold credentials, from every http or https URL in text, keeping scheme,
// host and path. Errors from net/http quote the request URL with its query,
// and the URL they quote isn't always the configured one (a redirect target,
// an SSE endpoint announced by the server), so logs pass error text through
// here rather than matching known URLs.
func redactURLs(text string) string {
	return urlPattern.ReplaceAllStringFunc(text, func(match string) string {
		authorityStart := strings.Index(match, "://") + len("://")
		rest := match[authorityStart:]
		if i := strings.IndexAny(rest, "?#"); i >= 0 {
			rest = rest[:i]
		}
		// A well-formed URL escapes "@" everywhere but the userinfo
		// separator, so the last one ends the userinfo.
		if i := strings.LastIndex(rest, "@"); i >= 0 {
			rest = rest[i+1:]
		}
		return match[:authorityStart] + rest
	})
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
	return h.logger.Core().Enabled(zapLevel(level))
}

func (h *redactingZapHandler) Handle(_ context.Context, record slog.Record) error {
	entry := h.logger.Check(zapLevel(record.Level), redactURLs(record.Message))
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

func zapLevel(level slog.Level) zapcore.Level {
	switch {
	case level >= slog.LevelError:
		return zapcore.ErrorLevel
	case level >= slog.LevelWarn:
		return zapcore.WarnLevel
	case level >= slog.LevelInfo:
		return zapcore.InfoLevel
	default:
		return zapcore.DebugLevel
	}
}
