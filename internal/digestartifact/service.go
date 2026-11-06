// Package digestartifact owns the persisted, daily Digest. It intentionally
// has no scheduler or delivery code: n8n chooses when to retrieve/send it.
package digestartifact

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/felipeafreitas/agregado/internal/telemetry"
)

type Article struct {
	ID, CanonicalURL, Title, Summary, Source string
	Score                                    int
	Topics                                   []string
}
type Candidate struct {
	Article
	Exploration bool
}
// Choice is one selected Article. The frontier model returns only IDs; the
// human-facing text comes from the Article itself, not from the selection call.
type Choice struct {
	ArticleID string `json:"article_id"`
}
type Item struct {
	Candidate
	ReadURL, UpvoteURL, DownvoteURL string
}
// WindowStart/WindowEnd are the Enrichment window the candidates came from;
// the next Digest's window starts at this one's WindowEnd. EmptyReason is set
// only on an artifact with no items, which is never persisted.
type Artifact struct {
	ID                                            string
	Date                                          time.Time
	Subject, HTML, Text                           string
	Items                                         []Item
	CandidateCount, FloorPassCount, SelectedCount int
	WindowStart, WindowEnd                        time.Time
	EmptyReason                                   string
}

// Pool is the Articles whose Enrichment completed in [Since, Until): Since is
// the previous persisted Digest's WindowEnd, bounded by the fallback lookback.
type Pool struct {
	Since, Until time.Time
	Articles     []Article
}

// FallbackLookback bounds the candidate window when there is no previous
// Digest, or the previous one is older than this (e.g. after an outage).
const FallbackLookback = 72 * time.Hour

type Store interface {
	Candidates(ctx context.Context, day time.Time, lookback time.Duration) (Pool, error)
	Find(context.Context, time.Time) (Artifact, bool, error)
	Save(context.Context, Artifact) (Artifact, bool, error)
}

// Frontier is deliberately one call: it orders the prepared candidates and
// writes each selection's explanation together.
type Frontier interface {
	Select(context.Context, []Candidate) ([]Choice, error)
}
type Service struct {
	store      Store
	frontier   Frontier
	baseURL    string
	floor, max int
	tracer     trace.Tracer
}

// Option configures an optional Service seam.
type Option func(*Service)

// WithTracerProvider records each computed Digest as one Langfuse trace.
// Without it (or with nil) the Service's tracer is a no-op.
func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(s *Service) {
		if tp != nil {
			s.tracer = tp.Tracer(telemetry.TracerName)
		}
	}
}

func NewService(store Store, frontier Frontier, baseURL string, floor, max int, opts ...Option) *Service {
	if max <= 0 || max > 10 {
		max = 10
	}
	if floor <= 0 {
		floor = 3
	}
	s := &Service{store: store, frontier: frontier, baseURL: strings.TrimSuffix(baseURL, "/"), floor: floor, max: max,
		tracer: noop.NewTracerProvider().Tracer(telemetry.TracerName)}
	for _, opt := range opts {
		opt(s)
	}
	return s
}
// ForDate's intermediate steps are logged at Info — the frontier call in
// particular can legitimately run for tens of seconds to several minutes
// (a single request scoring/explaining several candidates against a
// reasoning model), so a live `docker logs` tail should be able to show
// that it's in flight rather than going silent until it finally succeeds or
// times out (agregado#145's live verification hit exactly that gap: a 90s
// wait with no visibility into how far the request had gotten).
func (s *Service) ForDate(ctx context.Context, day time.Time) (Artifact, bool, error) {
	day = date(day)
	if saved, ok, err := s.store.Find(ctx, day); err != nil || ok {
		return saved, false, err
	}
	// The trace opens only once the Digest has to be computed: a saved
	// artifact is served without a model call, so it leaves no trace. The key
	// is the date ID, fixed before Save swaps in the stored UUID.
	id := day.Format("20060102")
	ctx, span := telemetry.StartTrace(ctx, s.tracer, "digest", telemetry.DigestKey(id),
		telemetry.TraceMetadata("artifact_id").String(id))
	defer span.End()
	artifact, created, err := s.compute(ctx, span, day)
	if err != nil {
		telemetry.Fail(span, err.Error())
		return artifact, created, err
	}
	chosen := make([]string, len(artifact.Items))
	for i, item := range artifact.Items {
		chosen[i] = item.ID
	}
	span.SetAttributes(
		attribute.String(telemetry.AttrObsOutput, jsonIDs(chosen)),
		telemetry.TraceMetadata("selected_count").Int(artifact.SelectedCount),
	)
	if artifact.EmptyReason != "" {
		span.SetAttributes(telemetry.TraceMetadata("empty_reason").String(artifact.EmptyReason))
	}
	return artifact, created, nil
}

// compute builds and saves the Digest for day. What the select call is given
// goes on span as soon as it is known, so a failed call still shows it.
func (s *Service) compute(ctx context.Context, span trace.Span, day time.Time) (Artifact, bool, error) {
	dateStr := day.Format("2006-01-02")
	pool, err := s.store.Candidates(ctx, day, FallbackLookback)
	if err != nil {
		return Artifact{}, false, err
	}
	candidates := uniqueAndDiverse(pool.Articles, s.floor, s.max)
	candidateIDs := make([]string, len(candidates.items))
	for i, c := range candidates.items {
		candidateIDs[i] = c.ID
	}
	span.SetAttributes(
		attribute.String(telemetry.AttrObsInput, jsonIDs(candidateIDs)),
		telemetry.TraceMetadata("candidate_count").Int(candidates.total),
		telemetry.TraceMetadata("floor_pass_count").Int(candidates.floorPass),
		telemetry.TraceMetadata("window_since").String(pool.Since.Format(time.RFC3339)),
		telemetry.TraceMetadata("window_until").String(pool.Until.Format(time.RFC3339)),
	)
	slog.Info("digest: candidates prepared", "component", "digestartifact", "date", dateStr,
		"since", pool.Since, "until", pool.Until,
		"pool", len(pool.Articles), "unique", candidates.total, "floor_pass", candidates.floorPass, "selected", len(candidates.items))
	artifact := Artifact{ID: day.Format("20060102"), Date: day, CandidateCount: candidates.total, FloorPassCount: candidates.floorPass, Subject: "Your Daily Digest - " + day.Format("January 2, 2006"), WindowStart: pool.Since, WindowEnd: pool.Until}
	if len(candidates.items) > 0 {
		slog.Info("digest: calling frontier select", "component", "digestartifact", "date", dateStr, "candidates", len(candidates.items))
		start := time.Now()
		choices, err := s.frontier.Select(ctx, candidates.items)
		if err != nil {
			slog.Error("digest: frontier select failed", "component", "digestartifact", "date", dateStr, "elapsed", time.Since(start).String(), "err", err)
			return Artifact{}, false, err
		}
		slog.Info("digest: frontier select returned", "component", "digestartifact", "date", dateStr, "elapsed", time.Since(start).String(), "choices", len(choices))
		byID := make(map[string]Choice, len(choices))
		for _, choice := range choices {
			byID[choice.ArticleID] = choice
		}
		for _, candidate := range candidates.items {
			if _, ok := byID[candidate.ID]; ok {
				artifact.Items = append(artifact.Items, s.item(candidate))
			}
		}
	}
	artifact.SelectedCount = len(artifact.Items)
	if artifact.SelectedCount == 0 {
		// Still returned so n8n sends it (#79's AC12), but not persisted: the
		// endpoint is idempotent, and a saved empty artifact would be re-sent
		// for this date even after the pipeline recovers. Not persisting also
		// keeps the next window anchored to the last Digest that had Articles.
		artifact.EmptyReason = emptyReason(artifact)
		artifact.HTML, artifact.Text = render(artifact)
		slog.Warn("digest: empty, not persisted", "component", "digestartifact", "date", dateStr, "reason", artifact.EmptyReason)
		return artifact, false, nil
	}
	artifact.HTML, artifact.Text = render(artifact)
	return s.store.Save(ctx, artifact)
}

func jsonIDs(ids []string) string {
	data, _ := json.Marshal(ids)
	return string(data)
}

type prepared struct {
	items            []Candidate
	total, floorPass int
}

func uniqueAndDiverse(articles []Article, floor, max int) prepared {
	seen := map[string]bool{}
	unique := make([]Article, 0, len(articles))
	for _, a := range articles {
		if a.CanonicalURL != "" && !seen[a.CanonicalURL] {
			seen[a.CanonicalURL] = true
			unique = append(unique, a)
		}
	}
	sort.SliceStable(unique, func(i, j int) bool { return unique[i].Score > unique[j].Score })
	passing, rejected := []Article{}, []Article{}
	for _, a := range unique {
		if a.Score >= floor {
			passing = append(passing, a)
		} else {
			rejected = append(rejected, a)
		}
	}
	selected := diverse(passing, max)
	// Tops up to three even when nothing passes: low-scoring candidates must
	// never force an empty Digest (#79's AC12).
	if len(selected) < 3 {
		for _, a := range diverse(rejected, 3-len(selected)) {
			a.Exploration = true
			selected = append(selected, a)
		}
	}
	return prepared{items: selected, total: len(unique), floorPass: len(passing)}
}

// diverse greedily applies a small repeat penalty. It biases source/topic
// variety but never imposes quotas or excludes a high-scoring Article.
func diverse(articles []Article, limit int) []Candidate {
	remaining := append([]Article(nil), articles...)
	out := []Candidate{}
	sources, topics := map[string]int{}, map[string]int{}
	for len(remaining) > 0 && len(out) < limit {
		best := 0
		bestValue := -1 << 30
		for i, a := range remaining {
			value := a.Score*100 - sources[a.Source]*10
			for _, topic := range a.Topics {
				value -= topics[topic] * 5
			}
			if value > bestValue {
				best, bestValue = i, value
			}
		}
		a := remaining[best]
		remaining = append(remaining[:best], remaining[best+1:]...)
		out = append(out, Candidate{Article: a})
		sources[a.Source]++
		for _, topic := range a.Topics {
			topics[topic]++
		}
	}
	return out
}
func (s *Service) item(c Candidate) Item {
	return Item{Candidate: c, ReadURL: s.baseURL + "/r/" + c.ID, UpvoteURL: s.baseURL + "/f/" + c.ID + "/up", DownvoteURL: s.baseURL + "/f/" + c.ID + "/down"}
}
func emptyReason(a Artifact) string {
	if a.CandidateCount == 0 {
		// The window is on the database clock (TIMESTAMP columns), so no zone
		// is claimed here.
		return fmt.Sprintf("No Articles finished Enrichment between %s and %s.", a.WindowStart.Format("Jan 2 15:04"), a.WindowEnd.Format("Jan 2 15:04"))
	}
	return fmt.Sprintf("The selection model chose none of %d candidate Articles.", a.CandidateCount)
}
func render(a Artifact) (string, string) {
	if len(a.Items) == 0 {
		return "<p>" + template.HTMLEscapeString(a.EmptyReason) + "</p>", a.EmptyReason + "\n"
	}
	var html, text strings.Builder
	html.WriteString("<ol>")
	for _, i := range a.Items {
		label := template.HTMLEscapeString(i.Title)
		if i.Exploration {
			label += " <em>Exploration pick</em>"
		}
		var htmlClaims, textClaims strings.Builder
		for _, c := range claims(i.Summary) {
			fmt.Fprintf(&htmlClaims, "<p>%s</p>", template.HTMLEscapeString(c))
			textClaims.WriteString(c + "\n")
		}
		fmt.Fprintf(&html, "<li><a href=%q>%s</a>%s<br><a href=%q>👍</a> <a href=%q>👎</a></li>", i.ReadURL, label, htmlClaims.String(), i.UpvoteURL, i.DownvoteURL)
		fmt.Fprintf(&text, "%s%s\n%s%s\n👍 %s  👎 %s\n\n", i.Title, map[bool]string{true: " [Exploration pick]", false: ""}[i.Exploration], textClaims.String(), i.ReadURL, i.UpvoteURL, i.DownvoteURL)
	}
	html.WriteString("</ol>")
	return html.String(), text.String()
}
func date(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
