package langfuse

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/felipeafreitas/agregado/internal/telemetry"
)

// ErrScoreDropped is returned by Emit when the queue is full or the sink has
// shut down. The event is lost; callers only log it.
var ErrScoreDropped = errors.New("langfuse: score dropped")

const (
	defaultScoreQueueSize = 256
	scoreRequestTimeout   = 10 * time.Second
)

// Scores is the Langfuse telemetry.ScoreSink. Emit only enqueues; one worker
// posts each event to {BaseURL}/api/public/scores, so a slow or down
// Langfuse never holds up the caller. With Langfuse unconfigured it discards
// every event.
type Scores struct {
	endpoint string
	auth     string
	client   *http.Client

	mu     sync.RWMutex // guards closed against sends on a closed queue
	closed bool
	queue  chan telemetry.ScoreEvent
	ctx    context.Context // cancelled when Shutdown gives up
	cancel context.CancelFunc
	done   chan struct{}
}

var _ telemetry.ScoreSink = (*Scores)(nil)

// ScoresOption configures NewScores.
type ScoresOption func(*scoresOptions)

type scoresOptions struct{ queueSize int }

// WithScoreQueueSize sets how many events may wait while one is being sent.
func WithScoreQueueSize(n int) ScoresOption { return func(o *scoresOptions) { o.queueSize = n } }

// NewScores starts the score worker when cfg is enabled.
func NewScores(cfg Config, opts ...ScoresOption) *Scores {
	if !cfg.Enabled() {
		return &Scores{}
	}
	o := scoresOptions{queueSize: defaultScoreQueueSize}
	for _, opt := range opts {
		opt(&o)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Scores{
		endpoint: strings.TrimRight(cfg.BaseURL, "/") + "/api/public/scores",
		auth:     cfg.basicAuth(),
		client:   &http.Client{Timeout: scoreRequestTimeout},
		queue:    make(chan telemetry.ScoreEvent, o.queueSize),
		ctx:      ctx,
		cancel:   cancel,
		done:     make(chan struct{}),
	}
	go s.run()
	return s
}

// Enabled reports whether scores are sent to Langfuse.
func (s *Scores) Enabled() bool { return s.queue != nil }

// Emit queues event without waiting. ctx is not used for sending: the
// request it belongs to has usually finished by then.
func (s *Scores) Emit(_ context.Context, event telemetry.ScoreEvent) error {
	if s.queue == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return fmt.Errorf("%w: sink shut down", ErrScoreDropped)
	}
	select {
	case s.queue <- event:
		return nil
	default:
		slog.Warn("langfuse: score queue full, dropping score", "component", "langfuse", "score", event.Name, "trace_key", event.TraceKey)
		return fmt.Errorf("%w: queue full", ErrScoreDropped)
	}
}

// Shutdown stops accepting events and sends the queued ones, giving up when
// ctx ends.
func (s *Scores) Shutdown(ctx context.Context) error {
	if s.queue == nil {
		return nil
	}
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.queue)
	}
	s.mu.Unlock()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		s.cancel()
		return ctx.Err()
	}
}

func (s *Scores) run() {
	defer close(s.done)
	defer s.cancel()
	for event := range s.queue {
		if s.ctx.Err() != nil {
			continue // Shutdown gave up: drain without sending
		}
		if err := s.post(event); err != nil {
			slog.Warn("langfuse: score not sent", "component", "langfuse", "score", event.Name, "trace_key", event.TraceKey, "err", err)
		}
	}
}

func (s *Scores) post(event telemetry.ScoreEvent) error {
	body, err := json.Marshal(scoreBody(event))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(s.ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", s.auth)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("status %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
	}
	return nil
}

// scoreBody is Langfuse's CreateScoreRequest for a single score.
func scoreBody(e telemetry.ScoreEvent) map[string]any {
	body := map[string]any{
		"id":       ScoreID(e.TraceKey, e.Name),
		"traceId":  telemetry.TraceIDHex(e.TraceKey),
		"name":     e.Name,
		"dataType": string(e.DataType),
	}
	if e.DataType == telemetry.Categorical {
		body["value"] = e.StringValue
	} else {
		body["value"] = e.Value
	}
	if e.Comment != "" {
		body["comment"] = e.Comment
	}
	if !e.Timestamp.IsZero() {
		// Langfuse keeps milliseconds; truncating here keeps re-sends equal.
		body["timestamp"] = e.Timestamp.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z07:00")
	}
	return body
}

// ScoreID is the Langfuse score ID for name on the trace keyed traceKey: the
// first 32 hex chars of sha256(traceKey + "/" + name). Re-sending it (with
// the same timestamp) overwrites the score, so retries are idempotent.
func ScoreID(traceKey, name string) string {
	sum := sha256.Sum256([]byte(traceKey + "/" + name))
	return hex.EncodeToString(sum[:16])
}
