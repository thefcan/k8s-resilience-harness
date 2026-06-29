// Package logger builds the structured slog.Logger used across the harness.
//
// Every experiment run carries a run-id so that, once injectors and probes
// land in later milestones, all events for a single run can be correlated in
// the log stream and in the JSON report.
package logger

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// Format selects the slog handler output encoding.
type Format string

const (
	// FormatText is human-friendly key=value output for local runs.
	FormatText Format = "text"
	// FormatJSON is line-delimited JSON for CI and machine processing.
	FormatJSON Format = "json"
)

// ParseLevel maps a case-insensitive level name to an slog.Level.
// It returns an error for unknown names so the CLI can fail fast instead of
// silently defaulting.
func ParseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q (want debug|info|warn|error)", name)
	}
}

// ParseFormat validates a handler format name. An empty name defaults to text.
func ParseFormat(name string) (Format, error) {
	switch f := Format(strings.ToLower(strings.TrimSpace(name))); f {
	case "":
		return FormatText, nil
	case FormatText, FormatJSON:
		return f, nil
	default:
		return "", fmt.Errorf("unknown log format %q (want text|json)", name)
	}
}

// New builds a slog.Logger writing to w at the given level and format.
func New(w io.Writer, level slog.Level, format Format) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	switch format {
	case FormatJSON:
		handler = slog.NewJSONHandler(w, opts)
	default:
		handler = slog.NewTextHandler(w, opts)
	}
	return slog.New(handler)
}
