package telemetry

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type failingSink struct{}

func (failingSink) Emit(context.Context, ScoreEvent) error { return errors.New("queue full") }

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestEmitScoreLogsAFailedEmitAtWarnUnderLangfuse(t *testing.T) {
	logs := captureLogs(t)
	EmitScore(context.Background(), failingSink{}, BooleanScore(ArticleIndexKey("a1"), "opened", true, time.Time{}))

	line := logs.String()
	for _, want := range []string{"level=WARN", "component=langfuse", "score=opened", "trace_key=article-index:a1", `err="queue full"`} {
		if !strings.Contains(line, want) {
			t.Fatalf("log = %q, want it to contain %s", line, want)
		}
	}
}

func TestEmitScoreWithANilSinkDiscardsTheScore(t *testing.T) {
	logs := captureLogs(t)
	EmitScore(context.Background(), nil, BooleanScore(ArticleIndexKey("a1"), "opened", true, time.Time{}))
	if logs.Len() != 0 {
		t.Fatalf("log = %q, want nothing", logs.String())
	}
}
