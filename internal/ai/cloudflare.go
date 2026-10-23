package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/felipeafreitas/agregado/internal/digestartifact"
	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/felipeafreitas/agregado/internal/textutil"
)

// defaultMaxContentChars bounds how much article body we feed the model when
// the caller doesn't configure a budget.
const defaultMaxContentChars = 8000

// defaultCategorySlugs is the fallback allowed-list appended to the categorize
// prompt when no live tags are available (nil TagLister or empty result), so the
// prompt is always well-formed.
var defaultCategorySlugs = []string{"tech", "business", "personal", "politics", "economy", "science", "health", "entertainment"}

// jsonFenceRe matches a Markdown code fence (with or without a "json"
// language tag), capturing its content.
var jsonFenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)\\s*```")

// extractJSON recovers the JSON body from a model response that wraps its
// answer in a Markdown code fence — observed live from the configured
// reasoning model (agregado#145's digest-select call failed to decode with
// "invalid character '`' looking for beginning of value" because its answer
// arrived as ```json\n{...}\n``` rather than raw JSON). Responses that are
// already plain JSON pass through unchanged (just trimmed), so this is safe
// to apply unconditionally rather than only on a fenced response.
func extractJSON(s string) string {
	if m := jsonFenceRe.FindStringSubmatch(s); len(m) == 2 {
		return m[1]
	}
	return strings.TrimSpace(s)
}

// defaultRequestTimeout bounds a single Cloudflare call when the caller
// doesn't configure one. Digest compute makes several of these calls
// sequentially (categorize per article, summarize per group, then the
// overview) sharing one overall budget — without a per-call cap, one slow
// request can exhaust that budget and starve every call queued behind it.
// Kept in sync with config.AI.RequestTimeout's default (internal/config):
// agregado#145's live verification timed out the frontier-select call at the
// old 30s under normal latency for the configured reasoning model, which
// emits a verbose reasoning trace before its actual answer.
const defaultRequestTimeout = 90 * time.Second

type CloudflareProvider struct {
	accountID       string
	apiToken        string
	model           string
	client          *http.Client
	requestTimeout  time.Duration
	maxContentChars int

	prompts PromptStore // editable system prompts; nil → in-code defaults
	tags    TagLister   // live category slugs for categorize; nil → defaultCategorySlugs
	logs    AILogSink   // request/response logging; nil → no logging
}

type Messages struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func NewCloudflareProvider(accountID, apiToken, model string, requestTimeout time.Duration, maxContentChars int, prompts PromptStore, tags TagLister, logs AILogSink) *CloudflareProvider {
	if requestTimeout <= 0 {
		requestTimeout = defaultRequestTimeout
	}
	if maxContentChars <= 0 {
		maxContentChars = defaultMaxContentChars
	}
	return &CloudflareProvider{
		accountID:       accountID,
		apiToken:        apiToken,
		model:           model,
		client:          &http.Client{Timeout: requestTimeout},
		requestTimeout:  requestTimeout,
		maxContentChars: maxContentChars,
		prompts:         prompts,
		tags:            tags,
		logs:            logs,
	}
}

// systemPrompt returns the editable prompt for an operation, falling back to the
// in-code default when the store is absent, errors, or returns empty.
func (p *CloudflareProvider) systemPrompt(ctx context.Context, operation string) string {
	if p.prompts != nil {
		if s, err := p.prompts.SystemPrompt(ctx, operation); err == nil && s != "" {
			return s
		}
	}
	return DefaultPrompts[operation]
}

// categorySlugs returns the live tag slugs, or the built-in fallback list.
func (p *CloudflareProvider) categorySlugs(ctx context.Context) []string {
	if p.tags != nil {
		if slugs, err := p.tags.CategorySlugs(ctx); err == nil && len(slugs) > 0 {
			return slugs
		}
	}
	return defaultCategorySlugs
}

// complete runs an AI call and records the request/response to the log sink,
// whether it succeeds or fails. The operation label ties the log row to the
// caller (score/categorize/summarize/digest).
//
// Each call gets its own bounded context (p.requestTimeout) rather than
// inheriting whatever's left on the caller's deadline: digest compute makes
// several of these calls back to back under one overall budget, and a single
// slow call must not eat the time reserved for the calls after it.
func (p *CloudflareProvider) complete(ctx context.Context, operation, systemPrompt, userPrompt string) (string, error) {
	return p.completeWith(ctx, operation, systemPrompt, userPrompt, callOptions{})
}

// callOptions are per-call generation controls. The zero value sends neither
// field, leaving Workers AI's model defaults in place.
type callOptions struct {
	// MaxCompletionTokens caps generated tokens; 0 leaves it unset.
	MaxCompletionTokens int
	// DisableThinking asks the chat template to skip the reasoning trace.
	DisableThinking bool
}

// selectOptions keep the digest-select call well inside p.requestTimeout.
// With reasoning on, the 2026-10-22 call spent 71s emitting 3,876 completion
// tokens for an answer of a few hundred, and 2026-10-23's ran past 90s. The
// answer is at most 10 IDs, so 1024 tokens is ample once thinking is off; if
// the model ever reasons anyway, the call stops at the cap and fails fast as
// truncated instead of running into the timeout.
var selectOptions = callOptions{MaxCompletionTokens: 1024, DisableThinking: true}

func (p *CloudflareProvider) completeWith(ctx context.Context, operation, systemPrompt, userPrompt string, opts callOptions) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, p.requestTimeout)
	defer cancel()

	start := time.Now()
	response, used, err := p.doComplete(callCtx, systemPrompt, userPrompt, opts)
	if used.Prompt+used.Completion > 0 {
		attrs := []any{"component", "ai", "operation", operation, "model", p.model,
			"prompt_tokens", used.Prompt, "completion_tokens", used.Completion}
		if n, ok := used.neurons(p.model); ok {
			attrs = append(attrs, "neurons", math.Round(n*100)/100)
		}
		slog.Info("ai: usage", attrs...)
	}
	p.record(ctx, operation, systemPrompt, userPrompt, response, err, time.Since(start))
	return response, err
}

// record persists a log entry using a context detached from the call's own
// deadline/cancellation. Using the call's context here would mean a call that
// failed with context deadline exceeded also fails to log itself — the
// failure vanishes from the AI log table instead of showing up as a failed
// row, which is exactly the visibility we need when a call times out.
func (p *CloudflareProvider) record(ctx context.Context, operation, systemPrompt, userPrompt, response string, callErr error, elapsed time.Duration) {
	if p.logs == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	entry := LogEntry{
		Operation:    operation,
		Model:        p.model,
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		Response:     response,
		Success:      callErr == nil,
		DurationMs:   int(elapsed.Milliseconds()),
	}
	if callErr != nil {
		entry.Err = callErr.Error()
	}
	p.logs.Record(ctx, entry)
}

func (p *CloudflareProvider) doComplete(ctx context.Context, systemPrompt, userPrompt string, opts callOptions) (string, usage, error) {
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run/%s", p.accountID, p.model)
	data, err := completionRequest(systemPrompt, userPrompt, opts)
	if err != nil {
		return "", usage{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(data))
	if err != nil {
		return "", usage{}, err
	}

	req.Header.Set("Authorization", "Bearer "+p.apiToken)
	req.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(req)

	if err != nil {
		return "", usage{}, err
	}
	defer response.Body.Close()
	return decodeCompletion(response.Body)
}

// completionRequest builds the Workers AI request body. enable_thinking goes
// through chat_template_kwargs, the model's pass-through to its chat template.
func completionRequest(systemPrompt, userPrompt string, opts callOptions) ([]byte, error) {
	body := struct {
		Messages            []Messages     `json:"messages"`
		MaxCompletionTokens int            `json:"max_completion_tokens,omitempty"`
		ChatTemplateKwargs  map[string]any `json:"chat_template_kwargs,omitempty"`
	}{
		Messages: []Messages{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		MaxCompletionTokens: opts.MaxCompletionTokens,
	}
	if opts.DisableThinking {
		body.ChatTemplateKwargs = map[string]any{"enable_thinking": false}
	}
	return json.Marshal(body)
}

// decodeCompletion reads a Workers AI response. A completion cut off by the
// token cap is an error rather than content: its text is a partial answer
// (or reasoning) that would only fail later as malformed JSON.
func decodeCompletion(r io.Reader) (string, usage, error) {
	var result struct {
		Result struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage struct {
				Prompt     int `json:"prompt_tokens"`
				Completion int `json:"completion_tokens"`
			} `json:"usage"`
		} `json:"result"`
		Success bool `json:"success"`
		Errors  []struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(r).Decode(&result); err != nil {
		return "", usage{}, fmt.Errorf("Error decoding result")
	}

	if !result.Success {
		return "", usage{}, fmt.Errorf("cloudflare AI error: %v", result.Errors)
	}

	used := usage{Prompt: result.Result.Usage.Prompt, Completion: result.Result.Usage.Completion}
	if len(result.Result.Choices) == 0 {
		return "", used, fmt.Errorf("cloudflare AI returned no choices")
	}
	choice := result.Result.Choices[0]
	if choice.FinishReason == "length" {
		return "", used, fmt.Errorf("cloudflare AI response truncated at the token cap (%d completion tokens)", used.Completion)
	}

	return choice.Message.Content, used, nil
}

// defaultSummarizeExcerptChars bounds each article's content excerpt in the
// Summarize prompt. Titles alone (the old behavior) give the model nothing
// to summarize but its own prior guesses at relevance; a real excerpt plus
// the article's already-computed RelevanceReason gives it substance without
// re-paying for a full body per article.
const defaultSummarizeExcerptChars = 400

func (p *CloudflareProvider) Summarize(ctx context.Context, articles []domain.Article) (string, error) {
	systemPrompt := p.systemPrompt(ctx, OpSummarize)

	excerptBudget := defaultSummarizeExcerptChars
	if n := len(articles); n > 0 {
		if perArticle := p.maxContentChars / n; perArticle < excerptBudget {
			excerptBudget = perArticle
		}
	}

	var body strings.Builder
	for _, a := range articles {
		fmt.Fprintf(&body, "- %s\n", a.Title)
		if a.RelevanceReason != nil && *a.RelevanceReason != "" {
			fmt.Fprintf(&body, "  Why it matters: %s\n", *a.RelevanceReason)
		}
		if excerpt := textutil.Clean(a.BestText(), excerptBudget); excerpt != "" {
			fmt.Fprintf(&body, "  Excerpt: %s\n", excerpt)
		}
	}
	userPrompt := fmt.Sprintf("Articles:\n%s\nSummary:", body.String())

	return p.complete(ctx, OpSummarize, systemPrompt, userPrompt)
}

// SummarizeArticle produces the machine-input Summary for one Article (ADR 0009):
// the raw model response to the structured-claims prompt. Validation and the
// lead-extract fallback live in internal/articleindex, which owns the contract.
func (p *CloudflareProvider) SummarizeArticle(ctx context.Context, title, content string) (string, error) {
	userPrompt := fmt.Sprintf("Title: %s\n\nContent: %s", title, textutil.Clean(content, p.maxContentChars))
	return p.complete(ctx, OpArticleSummary, p.systemPrompt(ctx, OpArticleSummary), userPrompt)
}

func (p *CloudflareProvider) Digest(ctx context.Context, topicSummaries []string) (string, error) {
	systemPrompt := p.systemPrompt(ctx, OpDigest)
	userPrompt := "Topic summaries:\n" + strings.Join(topicSummaries, "\n") + "\n\nIntroduction:"

	return p.complete(ctx, OpDigest, systemPrompt, userPrompt)
}

// Select performs the Digest's single frontier-model call. Selection and its
// explanations travel together so a retry can persist one coherent artifact.
func (p *CloudflareProvider) Select(ctx context.Context, candidates []digestartifact.Candidate) ([]digestartifact.Choice, error) {
	type candidate struct {
		ID, Title, Summary string
		Score              int
		Topics             []string
		Exploration        bool
	}
	payload := make([]candidate, len(candidates))
	for i, c := range candidates {
		payload[i] = candidate{c.ID, c.Title, flattenSummary(c.Summary), c.Score, c.Topics, c.Exploration}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	result, err := p.completeWith(ctx, OpDigestSelect, p.systemPrompt(ctx, OpDigestSelect), "Candidates:\n"+string(data), selectOptions)
	if err != nil {
		return nil, err
	}
	ids, err := decodeSelection(result)
	if err != nil {
		return nil, err
	}
	choices := make([]digestartifact.Choice, len(ids))
	for i, id := range ids {
		choices[i] = digestartifact.Choice{ArticleID: id}
	}
	return choices, nil
}

// decodeSelection reads the ordered IDs OpDigestSelect's prompt promises:
// {"article_ids":["..."]}.
func decodeSelection(result string) ([]string, error) {
	var response struct {
		ArticleIDs []string `json:"article_ids"`
	}
	if err := json.Unmarshal([]byte(extractJSON(result)), &response); err != nil {
		return nil, fmt.Errorf("decode digest selection: %w", err)
	}
	return response.ArticleIDs, nil
}

// flattenSummary turns a stored Summary into plain text for the selection
// payload, so the model does not read JSON inside a JSON string. It accepts
// the three stored forms: structured JSON, the plain-text lead extract
// (returned unchanged), and either with a leading [content: partial] line.
func flattenSummary(summary string) string {
	const partialMarker = "[content: partial]"
	body, prefix := summary, ""
	if rest, ok := strings.CutPrefix(summary, partialMarker+"\n"); ok {
		body, prefix = rest, partialMarker+" "
	}
	var s struct {
		Kind     string   `json:"kind"`
		Claims   []string `json:"claims"`
		Entities []string `json:"entities"`
	}
	if err := json.Unmarshal([]byte(body), &s); err != nil || len(s.Claims) == 0 {
		return summary
	}
	out := s.Kind + ": " + strings.Join(s.Claims, "; ")
	if len(s.Entities) > 0 {
		out += " (entities: " + strings.Join(s.Entities, ", ") + ")"
	}
	return prefix + out
}

func (p *CloudflareProvider) Categorize(ctx context.Context, title, content string) (string, error) {
	// Append the live tag slugs so the classifier can only pick from current tags.
	systemPrompt := p.systemPrompt(ctx, OpCategorize) +
		" Allowed slugs: " + strings.Join(p.categorySlugs(ctx), ", ") + "."
	userPrompt := fmt.Sprintf("Title: %s\n\nContent: %s", title, textutil.Clean(content, p.maxContentChars))

	return p.complete(ctx, OpCategorize, systemPrompt, userPrompt)
}

func (p *CloudflareProvider) Reason(ctx context.Context, title, content string) (string, error) {
	systemPrompt := p.systemPrompt(ctx, OpReason)
	userPrompt := fmt.Sprintf("Title: %s\n\nContent: %s", title, textutil.Clean(content, p.maxContentChars))

	result, err := p.complete(ctx, OpReason, systemPrompt, userPrompt)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(result), nil
}

func (p *CloudflareProvider) Score(ctx context.Context, title, content string, topicWeights map[string]float64) (int, error) {
	systemPrompt := p.systemPrompt(ctx, OpScore)

	var weights strings.Builder
	for topic, w := range topicWeights {
		fmt.Fprintf(&weights, "- %s: %.1f\n", topic, w)
	}

	userPrompt := fmt.Sprintf("Title: %s\n\nContent: %s\n\nTopic interest weights (1.0 = neutral, higher = more interested): %s", title, textutil.Clean(content, p.maxContentChars), weights.String())

	result, err := p.complete(ctx, OpScore, systemPrompt, userPrompt)
	if err != nil {
		return 0, err
	}

	return parseScore(result)
}

// ScoreWithPreferences scores a Reader Article against the accepted
// PREFERENCES.md verbatim. It is separate from the legacy topic-weight scorer
// because preference scoring belongs to Agregado, not shared enrichment code.
func (p *CloudflareProvider) ScoreWithPreferences(ctx context.Context, title, content, preferences string) (int, error) {
	userPrompt := fmt.Sprintf("PREFERENCES.md:\n%s\n\nTitle: %s\n\nContent: %s", preferences, title, textutil.Clean(content, p.maxContentChars))
	result, err := p.complete(ctx, OpPreferenceScore, p.systemPrompt(ctx, OpPreferenceScore), userPrompt)
	if err != nil {
		return 0, err
	}
	return parseScore(result)
}

func parseScore(result string) (int, error) {
	score, err := strconv.Atoi(strings.TrimSpace(result))
	if err != nil {
		return 0, err
	}
	if score < 1 || score > 5 {
		return 0, fmt.Errorf("score out of range: %d", score)
	}
	return score, nil
}
