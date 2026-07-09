package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/felipeafreitas/agregado/internal/textutil"
)

// maxPromptContentChars caps how much article body we feed the model. Keeping
// the window small keeps the prompt focused (and cheaper) without starving the
// model of signal.
const maxPromptContentChars = 500

// defaultCategorySlugs is the fallback allowed-list appended to the categorize
// prompt when no live tags are available (nil TagLister or empty result), so the
// prompt is always well-formed.
var defaultCategorySlugs = []string{"tech", "business", "personal", "politics", "economy", "science", "health", "entertainment"}

// defaultRequestTimeout bounds a single Cloudflare call when the caller
// doesn't configure one. Digest compute makes several of these calls
// sequentially (categorize per article, summarize per group, then the
// overview) sharing one overall budget — without a per-call cap, one slow
// request can exhaust that budget and starve every call queued behind it.
const defaultRequestTimeout = 30 * time.Second

type CloudflareProvider struct {
	accountID	string
	apiToken	string
	model		string
	client		*http.Client
	requestTimeout	time.Duration

	prompts	PromptStore // editable system prompts; nil → in-code defaults
	tags	TagLister   // live category slugs for categorize; nil → defaultCategorySlugs
	logs	AILogSink   // request/response logging; nil → no logging
}

type Messages struct {
	Role	string	`json:"role"`
	Content	string	`json:"content"`
}

func NewCloudflareProvider(accountID, apiToken, model string, requestTimeout time.Duration, prompts PromptStore, tags TagLister, logs AILogSink) *CloudflareProvider {
	if requestTimeout <= 0 {
		requestTimeout = defaultRequestTimeout
	}
	return &CloudflareProvider{
		accountID: accountID,
		apiToken: apiToken,
		model: model,
		client: &http.Client{Timeout: requestTimeout},
		requestTimeout: requestTimeout,
		prompts: prompts,
		tags: tags,
		logs: logs,
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
	callCtx, cancel := context.WithTimeout(ctx, p.requestTimeout)
	defer cancel()

	start := time.Now()
	response, err := p.doComplete(callCtx, systemPrompt, userPrompt)
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

func (p *CloudflareProvider) doComplete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run/%s", p.accountID, p.model)
	body := struct {
      Messages []Messages `json:"messages"`
  	}{
   		Messages: []Messages{
     		{ Role: "system", Content: systemPrompt },
     		{ Role: "user", Content: userPrompt },
     	},
   	}
    data, err := json.Marshal(body)
   	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(data))
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+p.apiToken)
  	req.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(req)

	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	var result struct {
      Result struct {
          Choices []struct {
              Message struct {
                  Content string `json:"content"`
              } `json:"message"`
          } `json:"choices"`
      } `json:"result"`
      Success bool `json:"success"`
      Errors []struct {
          Code    int    `json:"code"`
          Message string `json:"message"`
      } `json:"errors"`
  	}

	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("Error decoding result")
	}

	if !result.Success {
		return "", fmt.Errorf("cloudflare AI error: %v", result.Errors)
	}

	if len(result.Result.Choices) == 0 {
         return "", fmt.Errorf("cloudflare AI returned no choices")
    }

	return result.Result.Choices[0].Message.Content, nil
}

func (p *CloudflareProvider) Summarize(ctx context.Context, articles []domain.Article) (string, error) {
	systemPrompt := p.systemPrompt(ctx, OpSummarize)
	var titles strings.Builder
  	for _, a := range articles {
      fmt.Fprintf(&titles, "- %s\n", a.Title)
   	}
    userPrompt := fmt.Sprintf("Articles:\n%s\nSummary:", titles.String())

	return p.complete(ctx, OpSummarize, systemPrompt, userPrompt)
}

func (p *CloudflareProvider) Digest(ctx context.Context, topicSummaries []string) (string, error) {
	systemPrompt := p.systemPrompt(ctx, OpDigest)
	userPrompt := "Topic summaries:\n" + strings.Join(topicSummaries, "\n") + "\n\nIntroduction:"

	return p.complete(ctx, OpDigest, systemPrompt, userPrompt)
}

func (p *CloudflareProvider) Categorize(ctx context.Context, title, content string) (string, error) {
	// Append the live tag slugs so the classifier can only pick from current tags.
	systemPrompt := p.systemPrompt(ctx, OpCategorize) +
		" Allowed slugs: " + strings.Join(p.categorySlugs(ctx), ", ") + "."
	userPrompt := fmt.Sprintf("Title: %s\n\nContent: %s", title, textutil.Clean(content, maxPromptContentChars))

	return p.complete(ctx, OpCategorize, systemPrompt, userPrompt)
}

func (p *CloudflareProvider) Reason(ctx context.Context, title, content string) (string, error) {
	systemPrompt := p.systemPrompt(ctx, OpReason)
	userPrompt := fmt.Sprintf("Title: %s\n\nContent: %s", title, textutil.Clean(content, maxPromptContentChars))

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

	userPrompt := fmt.Sprintf("Title: %s\n\nContent: %s\n\nTopic interest weights (1.0 = neutral, higher = more interested): %s", title, textutil.Clean(content, maxPromptContentChars), weights.String())

	result, err := p.complete(ctx, OpScore, systemPrompt, userPrompt)
	if err != nil {
		return 0, err
	}

	score, err := strconv.Atoi(strings.TrimSpace(result))
	if err != nil {
		return 0, err
	}

	if score < 1 || score > 5 {
      return 0, fmt.Errorf("score out of range: %d", score)
  	}

	return score, nil
}
