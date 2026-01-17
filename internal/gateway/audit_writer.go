package gateway

import (
	"encoding/json"
	"io"
	"os"
	"sync"

	"github.com/maybedont/maybe-dont/internal/config"
	"gopkg.in/natefinch/lumberjack.v2"
)

// AuditWriter defines the interface for writing audit entries
type AuditWriter interface {
	// Write writes an audit entry. The entry may be filtered based on configuration.
	// Returns true if the entry was written, false if it was filtered out.
	Write(entry *AuditEntry) (bool, error)

	// Close closes the writer and releases any resources
	Close() error
}

// JSONLAuditWriter writes audit entries as JSON Lines (one JSON object per line)
// with optional filtering and log rotation support.
type JSONLAuditWriter struct {
	writer     io.WriteCloser
	filter     string // "all" or "deny_only"
	mu         sync.Mutex
	isStdout   bool
	isStderr   bool
}

// NewJSONLAuditWriter creates a new JSONL audit writer.
//
// Parameters:
//   - auditPath: the file path, "stdout", or "stderr"
//   - logDir: the directory for log files (used when auditPath is a filename)
//   - rotationCfg: rotation configuration (only used for file paths)
//   - filter: "all" to write all entries, "deny_only" to only write denied entries
func NewJSONLAuditWriter(auditPath string, logDir string, rotationCfg config.RotationConfig, filter string) (*JSONLAuditWriter, error) {
	w := &JSONLAuditWriter{
		filter: filter,
	}

	// Default filter to "all" if empty
	if w.filter == "" {
		w.filter = "all"
	}

	switch auditPath {
	case "stdout":
		w.writer = nopCloser{os.Stdout}
		w.isStdout = true
	case "stderr":
		w.writer = nopCloser{os.Stderr}
		w.isStderr = true
	default:
		// File path - use lumberjack for rotation
		fullPath := auditPath
		if logDir != "" && !isAbsolutePath(auditPath) {
			fullPath = logDir + "/" + auditPath
		}

		w.writer = &lumberjack.Logger{
			Filename:   fullPath,
			MaxSize:    rotationCfg.MaxSizeMB,
			MaxBackups: rotationCfg.MaxBackups,
			MaxAge:     rotationCfg.MaxAgeDays,
			Compress:   rotationCfg.Compress,
		}
	}

	return w, nil
}

// Write writes an audit entry if it passes the filter.
// Returns (true, nil) if written, (false, nil) if filtered, or (false, error) on error.
func (w *JSONLAuditWriter) Write(entry *AuditEntry) (bool, error) {
	if entry == nil {
		return false, nil
	}

	// Apply filter
	if w.filter == "deny_only" && entry.Action != string(config.PolicyActionDeny) {
		return false, nil
	}

	// Marshal to JSON
	data, err := json.Marshal(entry)
	if err != nil {
		return false, err
	}

	// Append newline for JSONL format
	data = append(data, '\n')

	// Write with mutex protection
	w.mu.Lock()
	defer w.mu.Unlock()

	_, err = w.writer.Write(data)
	if err != nil {
		return false, err
	}

	return true, nil
}

// Close closes the underlying writer
func (w *JSONLAuditWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.writer != nil {
		return w.writer.Close()
	}
	return nil
}

// nopCloser wraps an io.Writer and provides a no-op Close method
type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error { return nil }

// isAbsolutePath checks if a path is absolute
func isAbsolutePath(path string) bool {
	if len(path) == 0 {
		return false
	}
	// Unix absolute path
	if path[0] == '/' {
		return true
	}
	// Windows absolute path (e.g., C:\)
	if len(path) >= 2 && path[1] == ':' {
		return true
	}
	return false
}
