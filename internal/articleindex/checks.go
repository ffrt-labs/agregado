package articleindex

import (
	"context"
	"log/slog"
	"time"

	"github.com/felipeafreitas/agregado/internal/telemetry"
)

// Code-check score names: free, deterministic quality signals recorded on
// every article-enrichment trace. They observe enrichment; they never steer it.
const (
	scoreSummaryValid    = "summary_valid"    // summarize returned usable structured JSON
	scoreSummaryFallback = "summary_fallback" // the Summary is the lead extract, whatever the cause
	scoreSlugAllowed     = "slug_allowed"     // categorize answered one of the live allowed slugs
)

// codeChecks emits one enrichment's code checks. at is fixed when the trace
// opens so a re-emitted score overwrites rather than duplicates (Langfuse
// matches on ID and timestamp).
type codeChecks struct {
	sink     telemetry.ScoreSink
	traceKey string
	at       time.Time
}

// emit records a Boolean check. A sink error is only logged: scores must not
// change the enrichment result.
func (c codeChecks) emit(ctx context.Context, name string, value bool) {
	if err := c.sink.Emit(ctx, telemetry.BooleanScore(c.traceKey, name, value, c.at)); err != nil {
		slog.Warn("langfuse: dropping code-check score", "component", "articleindex", "score", name, "err", err)
	}
}
