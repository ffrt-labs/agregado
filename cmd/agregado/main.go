package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/felipeafreitas/agregado/internal/ai"
	"github.com/felipeafreitas/agregado/internal/api"
	"github.com/felipeafreitas/agregado/internal/backup"
	"github.com/felipeafreitas/agregado/internal/broker"
	"github.com/felipeafreitas/agregado/internal/config"
	"github.com/felipeafreitas/agregado/internal/digest"
	"github.com/felipeafreitas/agregado/internal/ingestion/fetch"
	"github.com/felipeafreitas/agregado/internal/ingestion/rss"
	"github.com/felipeafreitas/agregado/internal/logging"
	"github.com/felipeafreitas/agregado/internal/mail"
	"github.com/felipeafreitas/agregado/internal/storage"
	"github.com/joho/godotenv"
)

// fatal logs a startup failure at ERROR (structured, so it's visible in the
// same JSON stream as everything else) and exits non-zero, preserving the
// original log.Fatal fail-fast behavior.
func fatal(msg string, err error) {
	slog.Error(msg, "component", "main", "err", err)
	os.Exit(1)
}

func main() {
	godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		// The default logger isn't configured from cfg yet (cfg is what
		// failed), so install a sane default first, then report structured.
		logging.Setup("info", "json")
		fatal("failed to load config", err)
	}

	logging.Setup(cfg.Level, cfg.Format)
	// Startup emits a short list of non-sensitive fields only — never the whole
	// Config/db/broker structs, which carry the database password and
	// Cloudflare API token. Shipping logs to a central store must not leak
	// credentials.
	slog.Info("agregado starting",
		"component", "main",
		"http_port", cfg.Http.Port,
		"ai_model", cfg.AI.Model,
		"log_level", cfg.Level,
		"log_format", cfg.Format,
		"rss_poll_interval", cfg.Pooler.Interval.String(),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	db, err := storage.NewDB(ctx, cfg.Database)

	if err != nil {
		fatal("failed to load database", err)
	}

	b, err := broker.NewBroker(&cfg.Queue)

	if err != nil {
		fatal("failed to load broker", err)
	}
	if err := b.DeclareTopology(); err != nil {
		fatal("failed to declare broker topology", err)
	}

	publisher, err := broker.NewPublisher(b)
	if err != nil {
		fatal("failed to create publisher", err)
	}

	consumer, err := broker.NewConsumer(b)
	if err != nil {
		fatal("failed to create consumer", err)
	}

	// Own channel (and therefore own Qos/prefetch) from the storage consumer —
	// enrichment runs a fetch + up to two AI calls per article and needs real
	// concurrency; storage stays fast and effectively serial. See
	// broker.Consumer.Consume.
	enrichConsumer, err := broker.NewConsumer(b)
	if err != nil {
		fatal("failed to create enrich consumer", err)
	}

	// Drains articles.dlq: every permanently-failed message is logged at ERROR
	// and acked, so failures are recorded instead of accumulating forever in a
	// queue with no reader. Own channel, single worker — throughput here is
	// irrelevant, visibility is the point.
	dlqConsumer, err := broker.NewConsumer(b)
	if err != nil {
		fatal("failed to create dead-letter consumer", err)
	}

	sourceRepo := storage.NewSourceRepo(db)
	articleRepo := storage.NewArticleRepo(db)
	rawHTMLRepo := storage.NewRawHTMLRepo(db)
	weightsRepo := storage.NewTopicWeightsRepo(db)
	tagRepo := storage.NewTagRepo(db)

	// AI provider dependencies: DB-backed prompts, live tag list, request logging.
	promptRepo := storage.NewPromptRepo(db)
	settingsRepo := storage.NewSettingsRepo(db)
	aiLogRepo := storage.NewAILogRepo(db)
	aiLogger := storage.NewAILogger(settingsRepo, aiLogRepo)

	provider := ai.NewCloudflareProvider(cfg.CloudflareAccountID, cfg.CloudflareAPIToken, cfg.Model, cfg.AI.RequestTimeout, cfg.AI.MaxContentChars, promptRepo, tagRepo, aiLogger)

	ranker := digest.NewRanker(
		articleRepo,
		tagRepo,
		cfg.Digest.MaxArticles,
		cfg.Digest.MinRelevanceScore,
	)
	generator, err := digest.NewDefaultGenerator(provider, cfg.Digest.BaseURL)

	if err != nil {
		fatal("failed to create generator", err)
	}

	mailer := mail.NewMailer(cfg.SMTP)
	scheduler := digest.NewScheduler(ranker, generator, mailer, sourceRepo, cfg.Digest)
	backupScheduler := backup.NewScheduler(sourceRepo, mailer, cfg.Backup)

	parser := rss.NewParser()
	poller := rss.NewPoller(sourceRepo, parser, publisher, cfg.Pooler.Interval)

	fetcher := fetch.New(cfg.Fetch.Timeout, cfg.Fetch.MaxBytes, cfg.Fetch.MinContentChars, cfg.Fetch.UserAgent)

	handler := storage.NewWorker(articleRepo, rawHTMLRepo, publisher)
	enrichHandler := storage.NewEnrichHandler(articleRepo, sourceRepo, articleRepo, fetcher, provider, tagRepo, articleRepo, provider, articleRepo, weightsRepo, cfg.Digest.MinRelevanceScore, cfg.Fetch.DistillMaxChars)
	dlqHandler := broker.NewDeadLetterHandler()

	server := api.NewServer(b, db, cfg.Webhook.Secret, scheduler, backupScheduler, poller, provider, cfg.Digest.MinRelevanceScore)

	go poller.Start(ctx)
	go server.Start(ctx, cfg.Http.Port)
	go scheduler.Start(ctx)
	go backupScheduler.Start(ctx)
	consumer.Consume("articles.store", 1, 5, handler)
	enrichConsumer.Consume("articles.enrich", 5, 5, enrichHandler)
	dlqConsumer.Consume(broker.DeadLetterQueue, 1, 1, dlqHandler)

	<-ctx.Done()

	// Shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
  	defer shutdownCancel()

	server.Shutdown(shutdownCtx)
	publisher.Close()
	consumer.Close()
	enrichConsumer.Close()
	dlqConsumer.Close()
	db.Close()
	b.Close()
}
