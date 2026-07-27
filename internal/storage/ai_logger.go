package storage

import (
	"context"
	"log/slog"

	"github.com/felipeafreitas/agregado/internal/ai"
)

// AILogger bridges the AI provider to storage: it reads the logging flag and
// writes a log row only when logging is enabled. Satisfies ai.AILogSink.
type AILogger struct {
	settings *SettingsRepo
	logs     *AILogRepo
}

func NewAILogger(settings *SettingsRepo, logs *AILogRepo) *AILogger {
	return &AILogger{settings: settings, logs: logs}
}

func (l *AILogger) Record(ctx context.Context, entry ai.LogEntry) {
	enabled, err := l.settings.AILoggingEnabled(ctx)
	if err != nil {
		slog.Error("ai logger: reading flag", "component", "ai", "err", err)
		return
	}
	if !enabled {
		return
	}
	if err := l.logs.Insert(ctx, entry); err != nil {
		slog.Error("ai logger: insert", "component", "ai", "err", err)
	}
}
