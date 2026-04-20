package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/felipeafreitas/agregado/internal/broker"
	"github.com/felipeafreitas/agregado/internal/ingestion/email"
	"github.com/felipeafreitas/agregado/internal/storage"
	"github.com/go-chi/chi/v5"
)

type Server struct {
	broker *broker.Broker
	db *storage.DB
	httpServer *http.Server
}

func NewServer(b *broker.Broker, db *storage.DB, webhookSecret string) *Server {
	r := chi.NewRouter()

	httpServer := http.Server{
		Handler: r,
	}

	emailParser := email.NewParser()
	sourceRepo := storage.NewSourceRepo(db)
	publisher, err := broker.NewPublisher(b)

	if err != nil {
          panic(err)
    }

    emailHandler := email.NewHandler(webhookSecret, emailParser, sourceRepo, publisher)

	s := &Server{
		broker: b,
		db: db,
		httpServer: &httpServer,
	}

	r.Get("/health", s.healthHandler)
	r.Get("/health/rabbit", s.rabbitHealthHandler)
	r.Get("/health/db", s.dbHealthHandler)
	r.Post("/webhook/email", emailHandler.HandleWebhook)

	return s
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status":"ok"})
}

func (s *Server) rabbitHealthHandler(w http.ResponseWriter, r *http.Request) {
	ch, err := s.broker.NewChannel()

	if err == nil {
		ch.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status":"ok"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	json.NewEncoder(w).Encode(map[string]string{"status":"error", "detail":err.Error()})
}

func (s *Server) dbHealthHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	err := s.db.Ping(ctx)

	if err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status":"ok"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	json.NewEncoder(w).Encode(map[string]string{"status":"error", "detail": err.Error()})
}

func (s *Server) Start(ctx context.Context, port string) error {
	s.httpServer.Addr = fmt.Sprintf(":%s", port)

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// add logging in the future
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.Shutdown(shutdownCtx)
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
