package langfuse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/felipeafreitas/agregado/internal/ai"
)

// DefaultPromptCacheTTL matches the Langfuse SDKs' prompt cache default.
const DefaultPromptCacheTTL = 60 * time.Second

// ProductionLabel is the label Agregado serves; moving it in Langfuse
// promotes or rolls back a prompt version.
const ProductionLabel = "production"

// promptFetchTimeout bounds one prompt request. Fetches always run in the
// background, so this only bounds how long a hung Langfuse holds a goroutine.
const promptFetchTimeout = 5 * time.Second

var errUnconfigured = errors.New("langfuse: not configured")

// Prompts serves Langfuse-managed chat prompts at the production label to
// ai.CloudflareProvider (it implements ai.TemplateStore). A model call never
// waits on Langfuse: every fetch runs in the background, one per prompt at a
// time. Each prompt is cached for the TTL; after it a call gets the cached
// copy while a refresh runs, and a failed refresh keeps the last good copy.
// With nothing fetched yet a call errors at once (the caller uses its
// in-code default) and starts the fetch; a failed fetch is not retried until
// the TTL has passed again. Warm fills the cache at startup so the defaults
// are served only briefly.
type Prompts struct {
	cfg    Config
	ttl    time.Duration
	client *http.Client
	now    func() time.Time

	mu    sync.Mutex
	cache map[string]*cachedPrompt
}

type cachedPrompt struct {
	prompt     ai.Prompt
	ok         bool      // prompt holds a good copy
	checked    time.Time // last fetch attempt, successful or not
	refreshing bool
}

// PromptsOption configures an optional Prompts seam.
type PromptsOption func(*Prompts)

// WithClock replaces time.Now for the cache TTL (tests).
func WithClock(now func() time.Time) PromptsOption {
	return func(p *Prompts) { p.now = now }
}

// NewPrompts returns a client for cfg's project. ttl <= 0 uses
// DefaultPromptCacheTTL. With Langfuse unconfigured every Template call
// errors, so callers use their in-code defaults.
func NewPrompts(cfg Config, ttl time.Duration, opts ...PromptsOption) *Prompts {
	if ttl <= 0 {
		ttl = DefaultPromptCacheTTL
	}
	p := &Prompts{
		cfg:    cfg,
		ttl:    ttl,
		client: &http.Client{Timeout: promptFetchTimeout},
		now:    time.Now,
		cache:  map[string]*cachedPrompt{},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Template returns the cached production version of the named chat prompt,
// starting a background fetch when the cache is empty or past the TTL.
func (p *Prompts) Template(_ context.Context, name string) (ai.Prompt, error) {
	if !p.cfg.Enabled() {
		return ai.Prompt{}, errUnconfigured
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	entry := p.entry(name)
	if p.now().Sub(entry.checked) >= p.ttl {
		p.refreshLocked(name, entry)
	}
	if !entry.ok {
		return ai.Prompt{}, fmt.Errorf("langfuse: prompt %s not fetched", name)
	}
	return entry.prompt, nil
}

// Warm starts a background fetch of each named prompt that has no cached
// copy, so the first calls after startup are served from Langfuse.
func (p *Prompts) Warm(names ...string) {
	if !p.cfg.Enabled() {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, name := range names {
		if entry := p.entry(name); !entry.ok {
			p.refreshLocked(name, entry)
		}
	}
}

// entry returns name's cache entry, creating it. p.mu must be held.
func (p *Prompts) entry(name string) *cachedPrompt {
	entry := p.cache[name]
	if entry == nil {
		entry = &cachedPrompt{}
		p.cache[name] = entry
	}
	return entry
}

// refreshLocked starts a background fetch of name unless one is running.
// p.mu must be held.
func (p *Prompts) refreshLocked(name string, entry *cachedPrompt) {
	if entry.refreshing {
		return
	}
	entry.refreshing = true
	go p.refresh(name)
}

func (p *Prompts) refresh(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), promptFetchTimeout)
	defer cancel()
	prompt, err := p.fetch(ctx, name)
	p.store(name, prompt, err)
}

// store records a fetch attempt: a success replaces the cached copy, a
// failure keeps it (and is logged).
func (p *Prompts) store(name string, prompt ai.Prompt, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry := p.cache[name]
	entry.checked = p.now()
	entry.refreshing = false
	if err != nil {
		slog.Warn("langfuse: prompt fetch failed", "component", "langfuse", "prompt", name,
			"cached_version", entry.prompt.Version, "err", err)
		return
	}
	entry.prompt, entry.ok = prompt, true
}

// chatMessage is a Langfuse chat prompt entry. Placeholders carry a type
// "placeholder" and a name instead of a role and content.
type chatMessage struct {
	Type    string `json:"type,omitempty"`
	Role    string `json:"role"`
	Content string `json:"content"`
}

type promptResponse struct {
	Type    string          `json:"type"`
	Name    string          `json:"name"`
	Version int             `json:"version"`
	Prompt  json.RawMessage `json:"prompt"`
}

// fetch GETs {base}/api/public/v2/prompts/{name}?label=production. The name
// is path-escaped: Agregado's names contain a folder slash.
func (p *Prompts) fetch(ctx context.Context, name string) (ai.Prompt, error) {
	endpoint := p.endpoint("/api/public/v2/prompts/"+url.PathEscape(name)) + "?label=" + ProductionLabel
	var resp promptResponse
	if err := p.do(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return ai.Prompt{}, err
	}
	if resp.Type != "chat" {
		return ai.Prompt{}, fmt.Errorf("prompt %s v%d is a %q prompt, want chat", name, resp.Version, resp.Type)
	}
	var messages []chatMessage
	if err := json.Unmarshal(resp.Prompt, &messages); err != nil {
		return ai.Prompt{}, fmt.Errorf("prompt %s v%d: %w", name, resp.Version, err)
	}
	if len(messages) != 2 || messages[0].Type == "placeholder" || messages[1].Type == "placeholder" ||
		messages[0].Role != "system" || messages[1].Role != "user" {
		return ai.Prompt{}, fmt.Errorf("prompt %s v%d must be one system then one user message", name, resp.Version)
	}
	return ai.Prompt{Name: resp.Name, Version: resp.Version, System: messages[0].Content, User: messages[1].Content}, nil
}

// Seed creates prompt as version 1 of its name at the production label,
// unless Langfuse already has any version of that name: Langfuse's create
// always adds a version, so this checks first and never overwrites.
func (p *Prompts) Seed(ctx context.Context, prompt ai.Prompt) (created bool, err error) {
	if !p.cfg.Enabled() {
		return false, errUnconfigured
	}
	var list struct {
		Data []json.RawMessage `json:"data"`
	}
	query := url.Values{"name": {prompt.Name}}.Encode()
	if err := p.do(ctx, http.MethodGet, p.endpoint("/api/public/v2/prompts")+"?"+query, nil, &list); err != nil {
		return false, fmt.Errorf("look up prompt %s: %w", prompt.Name, err)
	}
	if len(list.Data) > 0 {
		return false, nil
	}
	body := map[string]any{
		"name": prompt.Name,
		"type": "chat",
		"prompt": []chatMessage{
			{Role: "system", Content: prompt.System},
			{Role: "user", Content: prompt.User},
		},
		"labels": []string{ProductionLabel},
	}
	if err := p.do(ctx, http.MethodPost, p.endpoint("/api/public/v2/prompts"), body, nil); err != nil {
		return false, fmt.Errorf("create prompt %s: %w", prompt.Name, err)
	}
	return true, nil
}

func (p *Prompts) endpoint(path string) string {
	return strings.TrimRight(p.cfg.BaseURL, "/") + path
}

// do sends one authenticated public API request, JSON both ways.
func (p *Prompts) do(ctx context.Context, method, endpoint string, body, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", p.cfg.basicAuth())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s %s: status %d: %s", method, req.URL.Path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}
