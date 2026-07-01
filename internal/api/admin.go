package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/felipeafreitas/agregado/internal/ai"
	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/felipeafreitas/agregado/internal/storage"
	"github.com/go-chi/chi/v5"
)

const adminLogsPerPage = 50

type AdminHandler struct {
	logs     *storage.AILogRepo
	settings *storage.SettingsRepo
	prompts  *storage.PromptRepo
	tags     *storage.TagRepo
	nav      *NavBuilder
}

func NewAdminHandler(
	logs *storage.AILogRepo,
	settings *storage.SettingsRepo,
	prompts *storage.PromptRepo,
	tags *storage.TagRepo,
	nav *NavBuilder,
) *AdminHandler {
	return &AdminHandler{logs: logs, settings: settings, prompts: prompts, tags: tags, nav: nav}
}

type AdminLogsPageData struct {
	Logs           []domain.AILog
	Operation      string
	LoggingEnabled bool
	Page           int
	PrevPage       int
	NextPage       int
	HasPrev        bool
	HasNext        bool
	Nav            NavData
}

// LogsPage renders the AI request/response log, newest first, optionally filtered
// by operation via ?operation=, paged via ?page=.
func (h *AdminHandler) LogsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	operation := r.URL.Query().Get("operation")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * adminLogsPerPage

	logs, err := h.logs.List(ctx, adminLogsPerPage, offset, operation)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	enabled, err := h.settings.AILoggingEnabled(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	render(w, "admin_logs.html", AdminLogsPageData{
		Logs:           logs,
		Operation:      operation,
		LoggingEnabled: enabled,
		Page:           page,
		PrevPage:       page - 1,
		NextPage:       page + 1,
		HasPrev:        page > 1,
		HasNext:        len(logs) == adminLogsPerPage,
		Nav:            h.nav.Build(ctx),
	})
}

// ToggleLogging flips the ai_logging_enabled flag. The client reloads to reflect it.
func (h *AdminHandler) ToggleLogging(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	enabled, err := h.settings.AILoggingEnabled(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.settings.SetAILoggingEnabled(ctx, !enabled); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ClearLogs deletes all AI log rows.
func (h *AdminHandler) ClearLogs(w http.ResponseWriter, r *http.Request) {
	if err := h.logs.Clear(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

type AdminPromptsPageData struct {
	Prompts []domain.AIPrompt
	Nav     NavData
}

type PromptUpdate struct {
	SystemPrompt string `json:"system_prompt"`
}

// PromptsPage lists the editable system prompts (one per operation).
func (h *AdminHandler) PromptsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	prompts, err := h.prompts.List(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	render(w, "admin_prompts.html", AdminPromptsPageData{
		Prompts: prompts,
		Nav:     h.nav.Build(ctx),
	})
}

// UpdatePrompt saves a new system prompt for an operation.
func (h *AdminHandler) UpdatePrompt(w http.ResponseWriter, r *http.Request) {
	operation := chi.URLParam(r, "operation")
	var body PromptUpdate
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.prompts.Update(r.Context(), operation, body.SystemPrompt); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ResetPrompt restores an operation's prompt to its in-code default.
func (h *AdminHandler) ResetPrompt(w http.ResponseWriter, r *http.Request) {
	operation := chi.URLParam(r, "operation")
	def, ok := ai.DefaultPrompts[operation]
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown operation")
		return
	}
	if err := h.prompts.Update(r.Context(), operation, def); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

type AdminTagsPageData struct {
	Tags []domain.Tag
	Nav  NavData
}

type TagPayload struct {
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Color string `json:"color"`
}

// normalize lowercases + trims the slug so it matches the normalized classifier
// output (see ranker's slug lookup), and defaults an empty color.
func (p TagPayload) normalize() (name, slug, color string) {
	name = strings.TrimSpace(p.Name)
	slug = strings.ToLower(strings.TrimSpace(p.Slug))
	color = strings.TrimSpace(p.Color)
	if color == "" {
		color = "#64748b"
	}
	return
}

func (h *AdminHandler) TagsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tags, err := h.tags.FindAll(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	render(w, "admin_tags.html", AdminTagsPageData{Tags: tags, Nav: h.nav.Build(ctx)})
}

func (h *AdminHandler) CreateTag(w http.ResponseWriter, r *http.Request) {
	var body TagPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name, slug, color := body.normalize()
	if name == "" || slug == "" {
		writeError(w, http.StatusBadRequest, "name and slug are required")
		return
	}
	if err := h.tags.Create(r.Context(), name, slug, color); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *AdminHandler) UpdateTag(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body TagPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name, slug, color := body.normalize()
	if name == "" || slug == "" {
		writeError(w, http.StatusBadRequest, "name and slug are required")
		return
	}
	if err := h.tags.Update(r.Context(), id, name, slug, color); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *AdminHandler) DeleteTag(w http.ResponseWriter, r *http.Request) {
	if err := h.tags.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
