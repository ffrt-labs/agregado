// Package langfuse connects Agregado to a self-hosted Langfuse. It is an
// optional runtime dependency (spec #198): with any of the base URL and key
// pair missing, everything here is a no-op, and no Langfuse failure is ever
// returned to a caller on the request path; failures are logged at warn level
// with component=langfuse.
package langfuse

import (
	"context"
	"encoding/base64"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Config locates a Langfuse project. All three fields must be set for
// Langfuse to be used.
type Config struct {
	BaseURL   string // e.g. http://langfuse-web:3000
	PublicKey string // pk-lf-...
	SecretKey string // sk-lf-...
}

// Enabled reports whether Langfuse is configured.
func (c Config) Enabled() bool {
	return c.BaseURL != "" && c.PublicKey != "" && c.SecretKey != ""
}

// basicAuth is the Authorization header value Langfuse's public API takes.
func (c Config) basicAuth() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(c.PublicKey+":"+c.SecretKey))
}

// Tracing owns the tracer provider spans are exported through.
type Tracing struct {
	provider trace.TracerProvider
	sdk      *sdktrace.TracerProvider // nil when disabled
}

// NewTracing builds an OTLP/HTTP exporter to {BaseURL}/api/public/otel/v1/traces
// behind a batch span processor, so ending a span never waits on Langfuse;
// when the queue is full spans are dropped. With Langfuse unconfigured the
// tracer provider is a no-op and nothing is ever exported.
func NewTracing(ctx context.Context, cfg Config) (*Tracing, error) {
	if !cfg.Enabled() {
		return &Tracing{provider: noop.NewTracerProvider()}, nil
	}
	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(strings.TrimRight(cfg.BaseURL, "/")+"/api/public/otel/v1/traces"),
		otlptracehttp.WithHeaders(map[string]string{
			"Authorization": cfg.basicAuth(),
			// Without it Langfuse v4 can take up to 10 minutes to show OTel data.
			"x-langfuse-ingestion-version": "4",
		}),
	)
	if err != nil {
		return nil, err
	}
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		slog.Warn("langfuse: span export failed", "component", "langfuse", "err", err)
	}))
	sdk := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "agregado"))),
	)
	return &Tracing{provider: sdk, sdk: sdk}, nil
}

// Enabled reports whether spans are exported to Langfuse.
func (t *Tracing) Enabled() bool { return t.sdk != nil }

// TracerProvider is what instrumented packages create spans from.
func (t *Tracing) TracerProvider() trace.TracerProvider { return t.provider }

// Shutdown flushes pending spans to Langfuse, giving up when ctx ends.
func (t *Tracing) Shutdown(ctx context.Context) error {
	if t.sdk == nil {
		return nil
	}
	return t.sdk.Shutdown(ctx)
}
