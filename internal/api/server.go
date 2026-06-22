package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/felipeafreitas/agregado/internal/broker"
	"github.com/felipeafreitas/agregado/internal/digest"
	"github.com/felipeafreitas/agregado/internal/ingestion/email"
	"github.com/felipeafreitas/agregado/internal/ingestion/rss"
	"github.com/felipeafreitas/agregado/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Server struct {
	broker *broker.Broker
	db *storage.DB
	httpServer *http.Server
	scheduler *digest.Scheduler
}

func NewServer(b *broker.Broker, db *storage.DB, webhookSecret string, scheduler *digest.Scheduler, pooler *rss.Poller) *Server {
	r := chi.NewRouter()
	r.Use(
		middleware.RequestID,
		middleware.Logger,
		middleware.Recoverer,
	)

	httpServer := http.Server{
		Handler: r,
	}

	emailParser := email.NewParser()
	sourceRepo := storage.NewSourceRepo(db)
	articleRepo := storage.NewArticleRepo(db)
	feedbackRepo := storage.NewFeedbackRepo(db)
	weightsRepo := storage.NewTopicWeightsRepo(db)
	publisher, err := broker.NewPublisher(b)

	if err != nil {
          panic(err)
    }

    emailHandler := email.NewHandler(webhookSecret, emailParser, sourceRepo, publisher)
    sourcesHandler := NewSourceHandler(sourceRepo, pooler)
    articlesHandler := NewArticleHandler(articleRepo, sourceRepo)
    feedbackHandler := NewFeedbackHandler(
    	webhookSecret,
     	feedbackRepo,
     	weightsRepo,
      	articleRepo,
    )

	s := &Server{
		broker: b,
		db: db,
		httpServer: &httpServer,
		scheduler: scheduler,
	}

	r.Get("/health", s.healthHandler)
	r.Get("/health/rabbit", s.rabbitHealthHandler)
	r.Get("/health/db", s.dbHealthHandler)
	r.Post("/webhook/email", emailHandler.HandleWebhook)

	r.Post("/api/digest/send", s.Send)
	r.Get("/api/digest/preview", s.Preview)

	r.Route("/api/sources", func(r chi.Router) {
		r.Get("/", sourcesHandler.List)
		r.Post("/", sourcesHandler.Create)
		r.Put("/{id}", sourcesHandler.Update)
		r.Delete("/{id}", sourcesHandler.Delete)
	})

	r.Route("/api/articles", func(r chi.Router) {
		r.Get("/", articlesHandler.List)
		r.Post("/{id}/read", articlesHandler.MarkRead)
		r.Delete("/{id}/read", articlesHandler.MarkUnread)
		r.Get("/search", articlesHandler.Search)
	})

	r.Get("/articles", articlesHandler.ListPage)
	r.Get("/articles/search", articlesHandler.SearchPage)
	r.Get("/sources", sourcesHandler.ListPage)
	r.Post("/api/sources/{id}/refresh", sourcesHandler.Refresh)

	r.Get("/api/feedback", feedbackHandler.Handle)

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

	writeError(w, http.StatusServiceUnavailable, err.Error())
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

	writeError(w, http.StatusServiceUnavailable, err.Error())
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

func (s *Server) Send(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	err := s.scheduler.Send(ctx)

	if err != nil{
		log.Println("digest send error:", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) Preview(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	preview, err := s.scheduler.Preview(ctx)

	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())

		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, preview.HTML)
}
