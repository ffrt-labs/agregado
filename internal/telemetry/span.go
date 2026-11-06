package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// TracerName is the instrumentation scope every Agregado span is created under.
const TracerName = "github.com/felipeafreitas/agregado"

// Langfuse OTel attribute names, checked against Langfuse v4.72.0's
// packages/shared/src/server/otel/attributes.ts.
const (
	AttrAsRoot          = "langfuse.internal.as_root"
	AttrTraceName       = "langfuse.trace.name"
	AttrTraceTags       = "langfuse.trace.tags"
	AttrTraceMetadata   = "langfuse.trace.metadata."
	AttrObsType         = "langfuse.observation.type"
	AttrObsLevel        = "langfuse.observation.level"
	AttrObsStatus       = "langfuse.observation.status_message"
	AttrObsMetadata     = "langfuse.observation.metadata."
	AttrObsInput        = "langfuse.observation.input"
	AttrObsOutput       = "langfuse.observation.output"
	AttrGenAIModel      = "gen_ai.request.model"
	AttrGenAIMaxTokens  = "gen_ai.request.max_tokens"
	AttrGenAIInputToks  = "gen_ai.usage.input_tokens"
	AttrGenAIOutputToks = "gen_ai.usage.output_tokens"
)

// TraceMetadata is a top-level, filterable Langfuse trace metadata key.
func TraceMetadata(key string) attribute.Key { return attribute.Key(AttrTraceMetadata + key) }

// ObservationMetadata is a Langfuse observation metadata key.
func ObservationMetadata(key string) attribute.Key { return attribute.Key(AttrObsMetadata + key) }

// StartTrace opens the root span of the trace identified by key, so its trace
// ID is TraceID(key) whatever IDs the tracer provider would generate. It does
// what the Langfuse Python SDK does for a seeded trace: parent the span on a
// synthetic, sampled span context carrying the wanted trace ID, and mark the
// span langfuse.internal.as_root so Langfuse treats it as the trace's root.
// Any span already in ctx is ignored: a trace key always starts a new trace.
func StartTrace(ctx context.Context, tracer trace.Tracer, name, key string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	sum := TraceID(key)
	var parentID trace.SpanID
	copy(parentID[:], sum[8:]) // any non-zero ID; deterministic keeps tests stable
	parentID[0] |= 1
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID(sum),
		SpanID:     parentID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx = trace.ContextWithSpanContext(ctx, parent)
	attrs = append([]attribute.KeyValue{
		attribute.String(AttrAsRoot, "true"),
		attribute.String(AttrTraceName, name),
	}, attrs...)
	return tracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

// Fail marks span (without ending it) as a Langfuse ERROR-level observation
// with msg as its status message. Both the OTel status and the Langfuse
// attribute are set so any backend reads it as a failure.
func Fail(span trace.Span, msg string) {
	span.SetStatus(codes.Error, msg)
	span.SetAttributes(
		attribute.String(AttrObsLevel, "ERROR"),
		attribute.String(AttrObsStatus, msg),
	)
}
