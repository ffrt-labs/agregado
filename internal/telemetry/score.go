package telemetry

import (
	"context"
	"log/slog"
	"time"
)

// ScoreDataType is a Langfuse score's data type. These are Langfuse scores,
// not an Article's Score (CONTEXT.md): a Langfuse score is any judgement
// attached to a trace, such as a user_vote or a code check.
type ScoreDataType string

const (
	Numeric     ScoreDataType = "NUMERIC"
	Boolean     ScoreDataType = "BOOLEAN"
	Categorical ScoreDataType = "CATEGORICAL"
)

// ScoreEvent is one Langfuse score to attach to the trace identified by
// TraceKey (ArticleIndexKey, DigestKey). A sink derives the score's ID from
// TraceKey and Name, so re-emitting the same name on the same trace replaces
// the earlier value rather than adding another score.
type ScoreEvent struct {
	TraceKey string
	Name     string
	DataType ScoreDataType
	// Value is the number for Numeric scores and 1 or 0 for Boolean ones.
	Value float64
	// StringValue is the category for Categorical scores, e.g. "up".
	StringValue string
	Comment     string // optional
	// Timestamp must be the same every time the same score is re-emitted
	// (e.g. the first vote's time): Langfuse only overwrites a score sent
	// with both the same ID and the same timestamp. Zero means "now", which
	// only overwrites within the same day.
	Timestamp time.Time
}

// ScoreSink accepts Langfuse score events. Emit must never block the caller
// on Langfuse: an implementation queues the event and returns. The returned
// error (event dropped, sink closed) is for logging only; callers on the
// request path must not change their response because of it.
type ScoreSink interface {
	Emit(ctx context.Context, event ScoreEvent) error
}

// NopScoreSink discards every event. It is the sink when Langfuse is
// unconfigured, and what EmitScore uses for a nil sink.
type NopScoreSink struct{}

func (NopScoreSink) Emit(context.Context, ScoreEvent) error { return nil }

// EmitScore sends event to sink, treating a nil sink as NopScoreSink, so
// holders of an optional sink need no default of their own. A failure is
// only logged, at warn with component=langfuse: a score never changes the
// caller's result.
func EmitScore(ctx context.Context, sink ScoreSink, event ScoreEvent) {
	if sink == nil {
		sink = NopScoreSink{}
	}
	if err := sink.Emit(ctx, event); err != nil {
		slog.Warn("langfuse: score not sent", "component", "langfuse",
			"score", event.Name, "trace_key", event.TraceKey, "err", err)
	}
}

// CategoricalScore is a Categorical ScoreEvent.
func CategoricalScore(traceKey, name, value string, at time.Time) ScoreEvent {
	return ScoreEvent{TraceKey: traceKey, Name: name, DataType: Categorical, StringValue: value, Timestamp: at}
}

// BooleanScore is a Boolean ScoreEvent (Langfuse stores it as 1 or 0).
func BooleanScore(traceKey, name string, value bool, at time.Time) ScoreEvent {
	v := 0.0
	if value {
		v = 1
	}
	return ScoreEvent{TraceKey: traceKey, Name: name, DataType: Boolean, Value: v, Timestamp: at}
}

// NumericScore is a Numeric ScoreEvent.
func NumericScore(traceKey, name string, value float64, at time.Time) ScoreEvent {
	return ScoreEvent{TraceKey: traceKey, Name: name, DataType: Numeric, Value: value, Timestamp: at}
}
