// Package logging is Agregado's single logging seam. It builds a structured
// slog handler (JSON for the deploy, text for local dev), installs it as the
// process default, and hands back the logger. Everything downstream uses the
// default logger plus per-subsystem child loggers bound with a "component"
// field — no logger is threaded through constructors.
//
// The contract with the (future, separate) central collector is exactly one
// thing: structured JSON on stdout. This package owns that contract and
// nothing else — no transport, no agent, no in-app storage.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// Setup builds the process logger from a level string and a format string,
// installs it as the slog default (so slog.Info/Error and child loggers pick
// it up everywhere), and returns it. Output always goes to stdout: where the
// logs end up is the collector's problem, not the app's.
func Setup(level, format string) *slog.Logger {
	logger := slog.New(buildHandler(os.Stdout, level, format))
	slog.SetDefault(logger)
	return logger
}

// buildHandler selects the concrete slog handler. "text" gives a
// human-readable handler for local development; anything else (including the
// default "json") gives the JSON handler the collector parses. Split out from
// Setup so it can be tested without touching the global default or stdout.
func buildHandler(w io.Writer, level, format string) slog.Handler {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}
	if strings.EqualFold(strings.TrimSpace(format), "text") {
		return slog.NewTextHandler(w, opts)
	}
	return slog.NewJSONHandler(w, opts)
}

// parseLevel maps a canonical level string to an slog.Level, falling back to
// info on anything it doesn't recognize.
func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		// info, plus anything unrecognized (or empty): a misconfigured
		// LOG_LEVEL degrades to the safe default rather than silencing or
		// crashing the process.
		return slog.LevelInfo
	}
}
