package digest

import (
	"context"

	"github.com/felipeafreitas/agregado/internal/config"
	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	ranker 		*Ranker
	generator 	*Generator
	mailer 		*Mailer
	config		config.Digest
}

func NewScheduler(ranker *Ranker, generator *Generator, mailer *Mailer, config config.Digest) *Scheduler {
	return &Scheduler{
		ranker: ranker,
		generator: generator,
		mailer: mailer,
		config: config,
	}
}

func (s *Scheduler) sendDigest(ctx context.Context) error {
	articles, err := s.ranker.GetDigestArticles(ctx, s.config.LookbackHours)
	if err != nil {
		return err
	}

	digestedEmail, err := s.generator.Generate(articles)
	if err != nil {
		return err
	}

	err = s.mailer.Send(ctx, s.config.RecipientEmail, *digestedEmail)

	return err
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
	articles, err := s.ranker.GetDigestArticles(ctx, s.config.LookbackHours)
	if err != nil {
		return nil, err
	}

	return s.generator.Generate(articles)
}
