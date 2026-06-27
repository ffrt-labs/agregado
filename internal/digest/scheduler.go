package digest

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/felipeafreitas/agregado/internal/config"
	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	ranker      *Ranker
	generator   *Generator
	mailer      *Mailer
	config      config.Digest
	mu          sync.Mutex
	cached      *ComputedDigest
	cachedDate  string
}

func NewScheduler(ranker *Ranker, generator *Generator, mailer *Mailer, config config.Digest) *Scheduler {
	return &Scheduler{
		ranker: ranker,
		generator: generator,
		mailer: mailer,
		config: config,
	}
}

func (s *Scheduler) Today(ctx context.Context) (ComputedDigest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	// TOGGLE: in-memory daily cache disabled for testing — uncomment to re-enable.
	// if s.cached != nil && s.cachedDate == today {
	// 	return *s.cached, nil
	// }

	// Use a background context so AI calls aren't cancelled if the triggering
	// HTTP request ends before the compute finishes.
	computeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	articles, err := s.ranker.GetDigestArticles(computeCtx, s.config.LookbackHours)
	if err != nil {
		return ComputedDigest{}, err
	}

	log.Printf("digest: computing for %d groups", len(articles))
	computed := s.generator.Compute(computeCtx, articles)
	log.Printf("digest: computed overview=%q groups=%d", computed.Overview != "", len(computed.Groups))
	s.cached = &computed
	s.cachedDate = today
	return computed, nil
}

func (s *Scheduler) sendDigest(ctx context.Context) error {
	computed, err := s.Today(ctx)
	if err != nil {
		return err
	}

	digestedEmail, err := s.generator.Render(computed)
	if err != nil {
		return err
	}

	return s.mailer.Send(ctx, s.config.RecipientEmail, *digestedEmail)
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
	return s.generator.Render(computed)
}
