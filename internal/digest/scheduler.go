package digest

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/felipeafreitas/agregado/internal/config"
	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/felipeafreitas/agregado/internal/mail"
	"github.com/robfig/cron/v3"
)

// SourceLister supplies the source list used to resolve article source names
// for the digest email. Defined here (not imported from internal/api) so the
// digest package stays free of a dependency on the HTTP layer.
type SourceLister interface {
	List(ctx context.Context, limit, offset int) ([]domain.Source, error)
}

type Scheduler struct {
	ranker      *Ranker
	generator   *Generator
	mailer      *mail.Mailer
	sources     SourceLister
	config      config.Digest
	mu          sync.Mutex
	cached      *ComputedDigest
	cachedDate  string
	// inFlight is non-nil while a compute is running and is closed when that
	// compute finishes. It lets a caller that needs the result now (Today)
	// wait on a compute already started in the background (TodayOrTrigger)
	// instead of starting a redundant, duplicate-AI-call compute. Guarded by mu.
	inFlight    chan struct{}
}

func NewScheduler(ranker *Ranker, generator *Generator, mailer *mail.Mailer, sources SourceLister, config config.Digest) *Scheduler {
	return &Scheduler{
		ranker: ranker,
		generator: generator,
		mailer: mailer,
		sources: sources,
		config: config,
	}
}

// sourceNames builds the source ID → display-name map that Render needs to
// resolve each article's source. Best-effort: on error it returns whatever was
// listed (names simply fall back to empty).
func (s *Scheduler) sourceNames(ctx context.Context) map[string]string {
	sources, _ := s.sources.List(ctx, 1000, 0)
	names := make(map[string]string, len(sources))
	for _, src := range sources {
		names[src.ID] = src.Name
	}
	return names
}

// Today returns today's digest, computing it synchronously (blocking) if the
// cache is cold. Safe for callers where nothing is waiting on an HTTP
// response — digest send and preview need the real content regardless of how
// long the AI compute takes. The web homepage should use TodayOrTrigger
// instead so a cold cache never blocks page load for minutes.
func (s *Scheduler) Today(ctx context.Context) (ComputedDigest, error) {
	return s.awaitCompute(time.Now().Format("2006-01-02"))
}

// TodayOrTrigger returns the cached digest for today if warm (ok=true). If the
// cache is cold, it starts (or joins) a background compute and returns
// immediately with ok=false rather than blocking, so a cold cache never stalls
// the page for the duration of a multi-minute AI compute.
func (s *Scheduler) TodayOrTrigger(ctx context.Context) (computed ComputedDigest, ok bool) {
	today := time.Now().Format("2006-01-02")

	s.mu.Lock()
	if s.cached != nil && s.cachedDate == today {
		cached := *s.cached
		s.mu.Unlock()
		return cached, true
	}
	alreadyRunning := s.inFlight != nil
	s.mu.Unlock()

	if !alreadyRunning {
		go func() {
			if _, err := s.awaitCompute(today); err != nil {
				log.Printf("digest: background compute failed: %v", err)
			}
		}()
	}

	return ComputedDigest{}, false
}

// Refresh drops the cache and computes a fresh digest, waiting for it to
// finish. Used by the manual "regenerate" action, whose caller already shows
// its own loading state while the request is in flight.
func (s *Scheduler) Refresh(ctx context.Context) (ComputedDigest, error) {
	today := time.Now().Format("2006-01-02")

	s.mu.Lock()
	s.cached = nil
	s.mu.Unlock()

	return s.awaitCompute(today)
}

// awaitCompute returns today's digest, computing it if the cache is cold. If a
// compute for today is already running (started by this or another caller),
// it waits for that one to finish and re-checks the cache instead of starting
// a second, redundant compute — GetDigestArticles/Compute make several AI
// calls each, so duplicating a run duplicates real API cost.
func (s *Scheduler) awaitCompute(today string) (ComputedDigest, error) {
	s.mu.Lock()
	if s.cached != nil && s.cachedDate == today {
		cached := *s.cached
		s.mu.Unlock()
		return cached, nil
	}
	if s.inFlight != nil {
		wait := s.inFlight
		s.mu.Unlock()
		<-wait
		return s.awaitCompute(today)
	}
	done := make(chan struct{})
	s.inFlight = done
	s.mu.Unlock()

	computed, err := s.runCompute(today)

	s.mu.Lock()
	if err == nil {
		s.cached = &computed
		s.cachedDate = today
	}
	s.inFlight = nil
	s.mu.Unlock()
	close(done)

	return computed, err
}

// runCompute runs the full digest pipeline without touching the cache. It uses
// a background context (not tied to any HTTP request) with a generous ceiling:
// each AI call inside the pipeline already carries its own per-call timeout
// (see ai.CloudflareProvider), so this only needs to bound the pipeline as a
// whole, not race a single slow call.
func (s *Scheduler) runCompute(today string) (ComputedDigest, error) {
	computeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	articles, candidateCount, err := s.ranker.GetDigestArticles(computeCtx, s.config.LookbackHours)
	if err != nil {
		return ComputedDigest{}, err
	}

	log.Printf("digest: computing for %d groups", len(articles))
	computed := s.generator.Compute(computeCtx, articles, candidateCount)
	log.Printf("digest: computed overview=%t groups=%d", computed.Overview != "", len(computed.Groups))
	return computed, nil
}

func (s *Scheduler) sendDigest(ctx context.Context) error {
	computed, err := s.Today(ctx)
	if err != nil {
		return err
	}

	digestedEmail, err := s.generator.Render(computed, s.sourceNames(ctx))
	if err != nil {
		return err
	}

	return s.mailer.Send(ctx, s.config.RecipientEmail, digestedEmail.Subject, digestedEmail.HTML, digestedEmail.Text)
}

func (s *Scheduler) Send(ctx context.Context) error {
	return s.sendDigest(ctx)
}

func (s *Scheduler) Start(ctx context.Context) {
	c := cron.New()
	c.AddFunc(s.config.Schedule, func() { s.sendDigest(ctx) })
	c.Start()
	<-ctx.Done()
	c.Stop()
}

func (s *Scheduler) Preview(ctx context.Context) (*DigestEmail, error) {
	computed, err := s.Today(ctx)
	if err != nil {
		return nil, err
	}
	return s.generator.Render(computed, s.sourceNames(ctx))
}
