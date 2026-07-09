package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/felipeafreitas/agregado/internal/opml"
	"github.com/go-chi/chi/v5"
)

type SourceHandler struct {
	sources         SourceRepository
	sourceRefresher SourceRefresher
	nav             *NavBuilder
}

type SourcesPageData struct {
	Sources          []domain.Source
	ErrorSourceCount int
	Nav              NavData
}

type SourcePatch struct {
	ExtractLinks	*bool	`json:"extract_links"`
	Summarize		*bool	`json:"summarize"`
}

type SourceRepository interface {
	List(ctx context.Context, limit int, offset int) ([]domain.Source, error)
	Create(ctx context.Context, source domain.Source) (*domain.Source, error)
	Delete(ctx context.Context, id string) error
	Update(ctx context.Context, source domain.Source) error
	FindByID(ctx context.Context, id string) (*domain.Source, error)
	FindByURL(ctx context.Context, url string) (*domain.Source, error)
}

type ImportResult struct {
	Created int `json:"created"`
	Skipped int `json:"skipped"`
}

type SourceRefresher interface {
	RefreshSource(ctx context.Context, id string) error
}

func NewSourceHandler(sourceRepo SourceRepository, sourceRefresher SourceRefresher, nav *NavBuilder) *SourceHandler {
	return &SourceHandler{
		sources:         sourceRepo,
		sourceRefresher: sourceRefresher,
		nav:             nav,
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

func (s *SourceHandler) ListPage(w http.ResponseWriter, r *http.Request) {
	limit, offset := ParsePagination(r)
	sources, err := s.sources.List(r.Context(), limit, offset)

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var errCount int
	for _, s := range sources {
		if s.ErrorCount > 0 {
			errCount++
		}
	}

	render(w, "sources.html", SourcesPageData{
		Sources:          sources,
		ErrorSourceCount: errCount,
		Nav:              s.nav.Build(r.Context()),
	})
}

func (s *SourceHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	err := s.sourceRefresher.RefreshSource(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *SourceHandler) Export(w http.ResponseWriter, r *http.Request) {
	sources, err := s.sources.List(r.Context(), 999, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	data, err := opml.Export(sources)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/x-opml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="agregado-sources.opml"`)
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func (s *SourceHandler) Import(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	candidates, err := opml.ParseImportCandidates(data)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid OPML: "+err.Error())
		return
	}

	var result ImportResult
	for _, c := range candidates {
		existing, err := s.sources.FindByURL(r.Context(), c.URL)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if existing != nil {
			result.Skipped++
			continue
		}

		url := c.URL
		_, err = s.sources.Create(r.Context(), domain.Source{
			Name:     c.Name,
			Type:     domain.Rss,
			URL:      &url,
			IsActive: true,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		result.Created++
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}

func (s *SourceHandler) Patch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var patch SourcePatch

 	err := json.NewDecoder(r.Body).Decode(&patch)
  	if err != nil {
 		writeError(w, http.StatusBadRequest, err.Error())
		return
   }

   source, err := s.sources.FindByID(r.Context(), id)
   if err != nil {
 		writeError(w, http.StatusNotFound, err.Error())
		return
   }

   if patch.Summarize != nil {
   		source.Summarize = *patch.Summarize
   }

   if patch.ExtractLinks != nil {
   		source.ExtractLinks = *patch.ExtractLinks
   }

   err = s.sources.Update(r.Context(), *source)
   if err != nil {
 		writeError(w, http.StatusInternalServerError, err.Error())
		return
   }

	w.WriteHeader(http.StatusNoContent)
}
