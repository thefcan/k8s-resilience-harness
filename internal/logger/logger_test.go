package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		"debug":          {in: "debug", want: slog.LevelDebug},
		"info":           {in: "INFO", want: slog.LevelInfo},
		"empty defaults": {in: "", want: slog.LevelInfo},
		"warn alias":     {in: "warning", want: slog.LevelWarn},
		"error":          {in: "  error  ", want: slog.LevelError},
		"unknown":        {in: "loud", wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ParseLevel(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseLevel(%q): expected error, got nil", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLevel(%q): unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ParseLevel(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseFormat(t *testing.T) {
	cases := map[string]struct {
		in      string
		want    Format
		wantErr bool
	}{
		"text":           {in: "text", want: FormatText},
		"json":           {in: "JSON", want: FormatJSON},
		"empty defaults": {in: "", want: FormatText},
		"unknown":        {in: "yaml", wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ParseFormat(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseFormat(%q): expected error, got nil", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseFormat(%q): unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ParseFormat(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestNewJSONEmitsStructuredFields(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, slog.LevelInfo, FormatJSON)
	log.Info("baseline complete", "run_id", "run-123", "success_rate", 0.99)

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("expected valid JSON log line, got error %v from %q", err, buf.String())
	}
	if rec["msg"] != "baseline complete" {
		t.Fatalf("msg = %v, want %q", rec["msg"], "baseline complete")
	}
	if rec["run_id"] != "run-123" {
		t.Fatalf("run_id = %v, want %q", rec["run_id"], "run-123")
	}
}

func TestNewTextRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, slog.LevelWarn, FormatText)
	log.Info("should be filtered")
	log.Warn("should appear")

	out := buf.String()
	if strings.Contains(out, "should be filtered") {
		t.Fatalf("info line leaked past warn level: %q", out)
	}
	if !strings.Contains(out, "should appear") {
		t.Fatalf("warn line missing: %q", out)
	}
}
