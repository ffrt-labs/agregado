package ai

import (
	"encoding/json"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/felipeafreitas/agregado/internal/telemetry"
)

// DefaultBaseURL is Cloudflare's API root; Workers AI calls go to
// {base}/accounts/{account}/ai/run/{model}.
const DefaultBaseURL = "https://api.cloudflare.com/client/v4"

// CloudflareOption configures an optional CloudflareProvider seam.
type CloudflareOption func(*CloudflareProvider)

// WithBaseURL points the provider at another Workers AI root (tests use an
// httptest stub). Empty keeps DefaultBaseURL.
func WithBaseURL(baseURL string) CloudflareOption {
	return func(p *CloudflareProvider) {
		if baseURL != "" {
			p.baseURL = strings.TrimRight(baseURL, "/")
		}
	}
}

// WithTracerProvider records every model call as a Langfuse generation span.
// Without it (or with nil) the provider's tracer is a no-op.
func WithTracerProvider(tp trace.TracerProvider) CloudflareOption {
	return func(p *CloudflareProvider) {
		if tp != nil {
			p.tracer = tp.Tracer(telemetry.TracerName)
		}
	}
}

// generationAttrs describe a model call before it is made: what Langfuse
// shows as the generation's model, parameters and input. Cost is not sent;
// Langfuse prices the usage with its model definition (in neurons).
//
// A call compiled from a prompt template records the variables as its input,
// so the trace can become a dataset item the eval harness compiles against
// another prompt version, and the compiled messages as metadata.messages; a
// Langfuse-managed version (not an in-code default, version 0) is also
// linked by name and version. Other calls record the messages as the input.
func generationAttrs(operation, model, systemPrompt, userPrompt string, opts callOptions, from *compiledPrompt) []attribute.KeyValue {
	messages, _ := json.Marshal([]Messages{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	})
	input := messages
	if from != nil {
		input, _ = json.Marshal(from.vars)
	}
	attrs := []attribute.KeyValue{
		attribute.String(telemetry.AttrObsType, "generation"),
		telemetry.ObservationMetadata("operation").String(operation),
		attribute.String(telemetry.AttrGenAIModel, model),
		attribute.String(telemetry.AttrObsInput, string(input)),
	}
	if from != nil {
		attrs = append(attrs, telemetry.ObservationMetadata("messages").String(string(messages)))
	}
	if from != nil && from.prompt.Version > 0 {
		attrs = append(attrs,
			attribute.String(telemetry.AttrObsPromptName, from.prompt.Name),
			attribute.Int(telemetry.AttrObsPromptVersion, from.prompt.Version),
		)
	}
	if opts.MaxCompletionTokens > 0 {
		attrs = append(attrs, attribute.Int(telemetry.AttrGenAIMaxTokens, opts.MaxCompletionTokens))
	}
	return attrs
}

// recordGeneration adds what the call returned: usage (also on a failed call
// when Workers AI reported it, e.g. a truncated answer), then the output, or
// the error at Langfuse ERROR level.
func recordGeneration(span trace.Span, response string, used usage, err error) {
	if used.Prompt+used.Completion > 0 {
		span.SetAttributes(
			attribute.Int(telemetry.AttrGenAIInputToks, used.Prompt),
			attribute.Int(telemetry.AttrGenAIOutputToks, used.Completion),
		)
	}
	if err != nil {
		telemetry.Fail(span, err.Error())
		return
	}
	span.SetAttributes(attribute.String(telemetry.AttrObsOutput, response))
}
