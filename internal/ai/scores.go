package ai

import "github.com/felipeafreitas/agregado/internal/telemetry"

// ScoreSink receives Langfuse scores, as AILogSink receives AI log entries.
// It lives in telemetry so packages ai imports (digestartifact) can emit
// scores too; this alias is the name the spec (#198) uses.
type ScoreSink = telemetry.ScoreSink

// ScoreEvent is one Langfuse score; see telemetry.ScoreEvent.
type ScoreEvent = telemetry.ScoreEvent
