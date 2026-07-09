package api

import (
	"context"
	"net/http"

	"github.com/felipeafreitas/agregado/internal/digest"
)

type DigestScheduler interface {
	Today(ctx context.Context) (digest.ComputedDigest, error)
	TodayOrTrigger(ctx context.Context) (digest.ComputedDigest, bool)
	Refresh(ctx context.Context) (digest.ComputedDigest, error)
}

type DigestArticleCounter interface {
	Count(ctx context.Context) (int, error)
}

// DigestPageData is the web-page wrapper around the shared DigestView: it adds
// the sidebar Nav that only the web shell needs. The DigestView fields are
// promoted, so templates/digest.html still reads .Greeting, .Groups, etc.
type DigestPageData struct {
	digest.DigestView
	Nav        NavData
	Generating bool // true when today's digest is still computing in the background
}

type DigestHandler struct {
	scheduler DigestScheduler
	sources   SourceLister
	articles  DigestArticleCounter
	nav       *NavBuilder
}

func NewDigestHandler(scheduler DigestScheduler, sources SourceLister, articles DigestArticleCounter, nav *NavBuilder) *DigestHandler {
	return &DigestHandler{
		scheduler: scheduler,
		sources:   sources,
		articles:  articles,
		nav:       nav,
	}
}

func (h *DigestHandler) HomePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// TodayOrTrigger (not Today) so a cold cache never blocks this request for
	// the duration of the AI compute — it kicks off a background compute and
	// returns immediately; the page shows a "generating" state until a
	// follow-up load finds the cache warm.
	computed, ready := h.scheduler.TodayOrTrigger(ctx)

	sources, _ := h.sources.List(ctx, 100, 0)
	sourceMap := make(map[string]string, len(sources))
	for _, s := range sources {
		sourceMap[s.ID] = s.Name
	}

	render(w, "digest.html", DigestPageData{
		DigestView: digest.BuildView(computed, sourceMap),
		Nav:        h.nav.Build(ctx),
		Generating: !ready,
	})
}

// Refresh forces a digest recompute (bypassing the daily cache) and warms the
// cache with the fresh result. The client reloads afterward to render it.
// Testing aid — the regenerate button on the home page.
func (h *DigestHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	if _, err := h.scheduler.Refresh(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
}
