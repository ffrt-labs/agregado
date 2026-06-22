package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/felipeafreitas/agregado/internal/domain"
)

type CloudflareProvider struct {
	accountID	string
	apiToken	string
	model		string
	client		*http.Client
}

type Messages struct {
	Role	string	`json:"role"`
	Content	string	`json:"content"`
}

func NewCloudflareProvider(accountID, apiToken, model string) *CloudflareProvider {
	return &CloudflareProvider{
		accountID: accountID,
		apiToken: apiToken,
		model: model,
		client: http.DefaultClient,
	}
}

func (p *CloudflareProvider) complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
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
	systemPrompt := "You are a news digest assistant. Given a list of article titles, write a 2-3 sentence summary capturing the key themes. Be concise and direct."
	var titles strings.Builder
  	for _, a := range articles {
      fmt.Fprintf(&titles, "- %s\n", a.Title)
   	}
    userPrompt := fmt.Sprintf("Articles:\n%s\nSummary:", titles.String())

	return p.complete(ctx, systemPrompt, userPrompt)
}

func (p *CloudflareProvider) Categorize(ctx context.Context, title, content string) (string, error) {
	systemPrompt := "You are a content classifier. Given an article title and content, return exactly one category slug from this list: tech, business, personal, politics, economy, science, health, entertainment. Return only the slug — no explanation, no punctuation."
	userPrompt := fmt.Sprintf("Title: %s\n\nContent: %s", title, content[:min(len(content), 500)])

	return p.complete(ctx, systemPrompt, userPrompt)
}

func (p *CloudflareProvider) Score(ctx context.Context, title, content string, topicWeights map[string]float64) (int, error) {
	systemPrompt := "You are a content score giver. Given an article title and content, return only a number 1-5. 1=spam/trivial, 3=worth reading, 5=essential global significance. Return only the integer."

	var weights strings.Builder
	for topic, w := range topicWeights {
		fmt.Fprintf(&weights, "- %s: %.1f\n", topic, w)
	}

	userPrompt := fmt.Sprintf("Title: %s\n\nContent: %s\n\nTopic interest weights (1.0 = neutral, higher = more interested): %s", title, content[:min(len(content), 500)], weights)

	result, err := p.complete(ctx, systemPrompt, userPrompt)
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
