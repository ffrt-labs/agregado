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
	"github.com/felipeafreitas/agregado/internal/articleindex"
	"github.com/felipeafreitas/agregado/internal/config"
	"github.com/felipeafreitas/agregado/internal/digestartifact"
	"github.com/felipeafreitas/agregado/internal/ingestion/fetch"
	"github.com/felipeafreitas/agregado/internal/langfuse"
	"github.com/felipeafreitas/agregado/internal/logging"
	"github.com/felipeafreitas/agregado/internal/preferenceexport"
	"github.com/felipeafreitas/agregado/internal/preferences"
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
	// Non-sensitive fields only — see startupFields.
	slog.Info("agregado starting", startupFields(cfg)...)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	db, err := storage.NewDB(ctx, cfg.Database)

	if err != nil {
		fatal("failed to load database", err)
	}
	slog.Info("database connected", databaseFields(cfg)...)

	articleIndexRepo := storage.NewArticleIndexRepo(db)
	tagRepo := storage.NewTagRepo(db)

	// Langfuse tracing is optional: unconfigured, the tracer provider is a
	// no-op and nothing is exported (spec #198).
	langfuseCfg := langfuse.ConfigFrom(cfg.Langfuse)
	tracing, err := langfuse.NewTracing(ctx, langfuseCfg)
	if err != nil {
		fatal("failed to set up Langfuse tracing", err)
	}
	slog.Info("langfuse tracing", "component", "main", "enabled", tracing.Enabled())
	tracerProvider := tracing.TracerProvider()
	// Langfuse scores (user_vote, code checks, ...) go through a bounded async
	// queue; a no-op when Langfuse is unconfigured.
	scores := langfuse.NewScores(langfuseCfg)

	// Langfuse-managed prompts (#201). Unconfigured, every template call uses
	// its in-code default. The cache fills in the background, so the defaults
	// serve only the first calls.
	langfusePrompts := langfuse.NewPrompts(langfuseCfg, cfg.Langfuse.PromptCacheTTL)
	for _, op := range ai.TemplateOperations() {
		langfusePrompts.Warm(ai.PromptName(op))
	}

	provider := ai.NewCloudflareProvider(cfg.CloudflareAccountID, cfg.CloudflareAPIToken, cfg.Model, cfg.AI.RequestTimeout, cfg.AI.MaxContentChars, tagRepo,
		ai.WithTracerProvider(tracerProvider), ai.WithTemplates(langfusePrompts))
	preferenceService := preferences.NewService(storage.NewPreferenceProfileRepo(db), articleIndexRepo, provider)
	preferencesHandler := preferences.NewHandler(cfg.Enrichment.Secret, preferenceService)
	articleIndexHandler := articleindex.NewHandler(cfg.Enrichment.Secret, articleindex.NewService(
		articleIndexRepo,
		fetch.New(cfg.Fetch.Timeout, cfg.Fetch.MaxBytes, cfg.Fetch.MinContentChars, cfg.Fetch.UserAgent),
		articleindex.NewCloudflareModel(provider),
		preferenceService,
		articleindex.WithTracerProvider(tracerProvider),
		articleindex.WithScoreSink(scores),
	))
	digestArtifactHandler := digestartifact.NewHandler(cfg.Enrichment.Secret, digestartifact.NewService(
		storage.NewDigestArtifactRepo(db), provider, cfg.Digest.BaseURL, cfg.Digest.MinRelevanceScore, 10,
		digestartifact.WithTracerProvider(tracerProvider),
		digestartifact.WithScoreSink(scores),
	))
	preferenceExportHandler := preferenceexport.NewHandler(cfg.Enrichment.Secret, articleIndexRepo)
	articleIndexOpenHandler := preferenceexport.NewOpenHandler(articleIndexRepo, scores)

	server := api.NewServer(db, articleIndexHandler, digestArtifactHandler, preferenceExportHandler, articleIndexOpenHandler, preferencesHandler, scores)

	go server.Start(ctx, cfg.Http.Port)

	<-ctx.Done()

	// Shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	server.Shutdown(shutdownCtx)
	// After the server, so spans of requests that just finished are flushed too.
	if err := tracing.Shutdown(shutdownCtx); err != nil {
		slog.Warn("langfuse: flushing spans at shutdown failed", "component", "langfuse", "err", err)
	}
	// After the spans, so a score never arrives for a trace not yet sent.
	if err := scores.Shutdown(shutdownCtx); err != nil {
		slog.Warn("langfuse: sending queued scores at shutdown failed", "component", "langfuse", "err", err)
	}
	db.Close()
}
