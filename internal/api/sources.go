package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/go-chi/chi/v5"
)

type SourceHandler struct {
	sources 	SourceRepository
}

type SourceRepository interface {
	List(ctx context.Context, limit int, offset int) ([]domain.Source, error)
	Create(ctx context.Context, source domain.Source) (*domain.Source, error)
	Delete(ctx context.Context, id string) error
	Update(ctx context.Context, source domain.Source) error
}

func NewSourceHandler(sourceRepo SourceRepository) *SourceHandler {
	return &SourceHandler{
		sources: sourceRepo,
	}
}

func (s *SourceHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, offset := ParsePagination(r)
	sources, err := s.sources.List(r.Context(), limit, offset)

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(sources)
}

func (s *SourceHandler) Create(w http.ResponseWriter, r *http.Request) {
	var sourcePayload domain.Source
 	err := json.NewDecoder(r.Body).Decode(&sourcePayload)

	 if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	 }

	source, err := s.sources.Create(r.Context(), sourcePayload)
	 if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	 }

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(source)
}

func (s *SourceHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var sourcePayload domain.Source

 	err := json.NewDecoder(r.Body).Decode(&sourcePayload)
  	if err != nil {
 		writeError(w, http.StatusBadRequest, err.Error())
		return
   }

   sourcePayload.ID = id
   err = s.sources.Update(r.Context(), sourcePayload)
   if err != nil {
 		writeError(w, http.StatusInternalServerError, err.Error())
		return
   }

  	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}

func (s *SourceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	err := s.sources.Delete(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
