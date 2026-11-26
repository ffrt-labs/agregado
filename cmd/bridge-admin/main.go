// Command bridge-admin is the Newsletters app: a tailnet-only web app that
// manages the Bridge's Newsletter Sources (spec #225, ADR-0012). It is served
// at newsletters.<domain> by the homelab-edge Caddy, which admits only
// tailnet addresses; the app itself has no login.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/felipeafreitas/agregado/internal/bridgeadmin"
	"github.com/felipeafreitas/agregado/internal/cloudflare/d1"
	"github.com/felipeafreitas/agregado/internal/cloudflare/r2"
	"github.com/felipeafreitas/agregado/internal/config"
	"github.com/felipeafreitas/agregado/internal/logging"
	"github.com/felipeafreitas/agregado/internal/miniflux"
	"github.com/joho/godotenv"
)

func fatal(msg string, err error) {
	slog.Error(msg, "component", "main", "err", err)
	os.Exit(1)
}

func main() {
	godotenv.Load()
	cfg, err := config.LoadBridgeAdmin()
	if err != nil {
		logging.Setup("info", "json")
		fatal("failed to load config", err)
	}
	logging.Setup(cfg.Level, cfg.Format)
	slog.Info("bridge-admin starting", startupFields(cfg)...)

	store := bridgeadmin.NewD1Store(d1.New(d1.Options{
		AccountID:  cfg.Cloudflare.AccountID,
		DatabaseID: cfg.Bridge.D1DatabaseID,
		APIToken:   cfg.Cloudflare.APIToken,
	}))
	reader := miniflux.New(cfg.Miniflux.URL, cfg.Miniflux.APIKey, 0)
	app := bridgeadmin.New(store, reader, bridgeadmin.Config{
		ReceivingDomain: cfg.Bridge.ReceivingDomain,
		BridgeOrigin:    cfg.Bridge.Origin,
	})
	app.WithObjects(r2.New(r2.Options{
		AccountID:       cfg.Cloudflare.AccountID,
		AccessKeyID:     cfg.R2.AccessKeyID,
		SecretAccessKey: cfg.R2.SecretAccessKey,
		Bucket:          cfg.R2.Bucket,
	}))

	server := &http.Server{
		Addr:              ":" + cfg.Http.Port,
		Handler:           app.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal("http server failed", err)
		}
	}()
	slog.Info("bridge-admin listening", "component", "main", "addr", server.Addr)

	<-ctx.Done()
	shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown failed", "component", "main", "err", err)
	}
}
