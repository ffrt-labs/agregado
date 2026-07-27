package logging

import (
	"fmt"
	"io"
	"log/slog"
	"testing"
)

// TestParseLevel pins the canonical level-string → slog.Level mapping and the
// crucial fallback: an unrecognized string must degrade to info, never panic
// or silence the process. This is the one behavioral decision in the logging
// module worth testing (the JSON/text formatting itself is an slog detail).
func TestParseLevel(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  slog.Level
	}{
		{"debug", "debug", slog.LevelDebug},
		{"info", "info", slog.LevelInfo},
		{"warn", "warn", slog.LevelWarn},
		{"error", "error", slog.LevelError},
		{"uppercase is accepted", "ERROR", slog.LevelError},
		{"surrounding space is trimmed", "  warn  ", slog.LevelWarn},
		{"unrecognized falls back to info", "loud", slog.LevelInfo},
		{"empty falls back to info", "", slog.LevelInfo},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseLevel(tc.input); got != tc.want {
				t.Errorf("parseLevel(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestBuildHandler pins format-string → handler type: "text" yields a text
// handler, and anything else (including the default "json") yields the JSON
// handler the collector expects. Guards against a typo silently flipping the
// deploy into text output.
func TestBuildHandler(t *testing.T) {
	cases := []struct {
		name   string
		format string
		want   string // %T of the returned handler
	}{
		{"json", "json", "*slog.JSONHandler"},
		{"text", "text", "*slog.TextHandler"},
		{"text is case-insensitive", "TEXT", "*slog.TextHandler"},
		{"unrecognized defaults to json", "yaml", "*slog.JSONHandler"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := buildHandler(io.Discard, "info", tc.format)
			if got := fmt.Sprintf("%T", h); got != tc.want {
				t.Errorf("buildHandler format=%q = %s, want %s", tc.format, got, tc.want)
			}
		})
	}
}
