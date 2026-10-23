package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompletionRequestOmitsGenerationControlsByDefault(t *testing.T) {
	data, err := completionRequest("sys", "user", callOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["max_completion_tokens"]; ok {
		t.Errorf("max_completion_tokens sent for zero options: %s", data)
	}
	if _, ok := body["chat_template_kwargs"]; ok {
		t.Errorf("chat_template_kwargs sent for zero options: %s", data)
	}
	if msgs, _ := body["messages"].([]any); len(msgs) != 2 {
		t.Errorf("messages = %v, want system and user", body["messages"])
	}
}

func TestCompletionRequestForSelectDisablesThinkingAndCapsTokens(t *testing.T) {
	data, err := completionRequest("sys", "user", selectOptions)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		MaxCompletionTokens int            `json:"max_completion_tokens"`
		ChatTemplateKwargs  map[string]any `json:"chat_template_kwargs"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	if body.MaxCompletionTokens != selectOptions.MaxCompletionTokens {
		t.Errorf("max_completion_tokens = %d, want %d", body.MaxCompletionTokens, selectOptions.MaxCompletionTokens)
	}
	if v, ok := body.ChatTemplateKwargs["enable_thinking"]; !ok || v != false {
		t.Errorf("chat_template_kwargs = %v, want enable_thinking: false", body.ChatTemplateKwargs)
	}
}

func TestDecodeCompletionReturnsContentAndUsage(t *testing.T) {
	got, used, err := decodeCompletion(strings.NewReader(`{"success":true,"result":{"choices":[{"message":{"content":"{\"article_ids\":[\"a\"]}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"article_ids":["a"]}` {
		t.Errorf("content = %q", got)
	}
	if used != (usage{Prompt: 10, Completion: 5}) {
		t.Errorf("usage = %+v", used)
	}
}

func TestDecodeCompletionRejectsATruncatedResponse(t *testing.T) {
	_, used, err := decodeCompletion(strings.NewReader(`{"success":true,"result":{"choices":[{"message":{"content":"{\"article_ids\":[\"a\","},"finish_reason":"length"}],"usage":{"prompt_tokens":10,"completion_tokens":1024}}}`))
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("err = %v, want a truncation error", err)
	}
	if used.Completion != 1024 {
		t.Errorf("usage = %+v, want completion tokens kept for the usage log", used)
	}
}

func TestDecodeCompletionReportsCloudflareErrors(t *testing.T) {
	_, _, err := decodeCompletion(strings.NewReader(`{"success":false,"errors":[{"code":5006,"message":"bad input"}]}`))
	if err == nil || !strings.Contains(err.Error(), "bad input") {
		t.Fatalf("err = %v, want the Cloudflare error", err)
	}
}
