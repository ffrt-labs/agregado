package backup

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/felipeafreitas/agregado/internal/config"
	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/felipeafreitas/agregado/internal/mail"
	"github.com/felipeafreitas/agregado/internal/opml"
	"github.com/robfig/cron/v3"
)

// SourceLister mirrors the identically-named local interface in
// internal/digest — keeps this package decoupled from internal/storage.
type SourceLister interface {
	List(ctx context.Context, limit, offset int) ([]domain.Source, error)
}

type Scheduler struct {
	sources SourceLister
	mailer  *mail.Mailer
	config  config.Backup
}

func NewScheduler(sources SourceLister, mailer *mail.Mailer, cfg config.Backup) *Scheduler {
	return &Scheduler{
		sources: sources,
		mailer:  mailer,
		config:  cfg,
	}
}

func (s *Scheduler) sendBackup(ctx context.Context) error {
	if s.config.RecipientEmail == "" {
		log.Println("backup: BACKUP_RECIPIENT_EMAIL not set, skipping")
		return nil
	}

	sources, err := s.sources.List(ctx, 999, 0)
	if err != nil {
		return err
	}

	data, err := opml.Export(sources)
	if err != nil {
		return err
	}

	subject := "Agregado sources backup — " + time.Now().Format("2006-01-02")
	body := fmt.Sprintf("Automated backup of %d sources attached as OPML.", len(sources))

	return s.mailer.SendAttachment(ctx, s.config.RecipientEmail, subject, body, "agregado-sources.opml", data)
}

// Send triggers a backup immediately, bypassing the cron schedule. Used by
// the manual POST /api/backup/send endpoint.
func (s *Scheduler) Send(ctx context.Context) error {
	return s.sendBackup(ctx)
}

func (s *Scheduler) Start(ctx context.Context) {
	c := cron.New()
	c.AddFunc(s.config.Schedule, func() {
		if err := s.sendBackup(ctx); err != nil {
			log.Printf("backup: send failed: %v", err)
		}
	})
	c.Start()
	<-ctx.Done()
	c.Stop()
}
