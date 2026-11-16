package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/felipeafreitas/agregado/internal/articleindex"
	"github.com/felipeafreitas/agregado/internal/digestartifact"
	"github.com/felipeafreitas/agregado/internal/preferenceexport"
	"github.com/felipeafreitas/agregado/internal/preferences"
	"github.com/felipeafreitas/agregado/internal/storage"
	"github.com/felipeafreitas/agregado/internal/telemetry"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Server struct {
	db         *storage.DB
	httpServer *http.Server
}

func NewServer(db *storage.DB, minRelevanceScore int, articleIndexHandler *articleindex.Handler, digestArtifactHandler *digestartifact.Handler, preferenceExportHandler *preferenceexport.Handler, articleIndexOpenHandler *preferenceexport.OpenHandler, preferencesHandler *preferences.Handler, scores telemetry.ScoreSink) *Server {
	r := chi.NewRouter()
	r.Use(
		middleware.RequestID,
		middleware.Logger,
		middleware.Recoverer,
	)

	httpServer := http.Server{
		Handler: r,
	}

	articleRepo := storage.NewArticleRepo(db)
	explicitFeedbackRepo := storage.NewExplicitFeedbackRepo(db)
	articlesHandler := NewArticleHandler(articleRepo)
	explicitFeedbackHandler := NewExplicitFeedbackHandler(explicitFeedbackRepo, explicitFeedbackRepo, scores)

	s := &Server{
		db:         db,
		httpServer: &httpServer,
	}

	r.Get("/health", s.healthHandler)
	r.Get("/health/db", s.dbHealthHandler)
	if articleIndexHandler != nil {
		r.Post("/api/private/articles/enrich", articleIndexHandler.Handle)
	}
	if digestArtifactHandler != nil {
		r.Post("/api/private/digests", digestArtifactHandler.Handle)
		r.Post("/api/private/digests/{date}", digestArtifactHandler.Handle)
	}
	if preferenceExportHandler != nil {
		r.Get("/api/private/preferences/signals", preferenceExportHandler.Handle)
	}
	if preferencesHandler != nil {
		r.Get("/api/private/preferences", preferencesHandler.Get)
		r.Put("/api/private/preferences", preferencesHandler.Put)
		r.Post("/api/private/preferences/regenerate", preferencesHandler.Regenerate)
	}

	if articleIndexOpenHandler != nil {
		r.Get("/r/{id}", articleIndexOpenHandler.HandleOr(articlesHandler.Open))
	} else {
		r.Get("/r/{id}", articlesHandler.Open)
	}
	r.Get("/f/{id}/{vote}", explicitFeedbackHandler.Handle)

	return s
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) dbHealthHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	err := s.db.Ping(ctx)

	if err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
