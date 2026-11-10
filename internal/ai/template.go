package ai

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
)

// Prompt is a versioned chat prompt: a system and a user message template
// with {{name}} variables. Prompts managed in Langfuse carry its name and
// version; an in-code default has Version 0.
type Prompt struct {
	Name    string // Langfuse prompt name, e.g. agregado/preference_score
	Version int    // Langfuse version; 0 for an in-code default
	System  string
	User    string
}

// PromptName is the Langfuse name of an operation's prompt.
func PromptName(operation string) string { return "agregado/" + operation }

// TemplateStore supplies the managed (Langfuse) prompt for an operation. An
// error means there is no usable managed prompt, and the caller falls back
// to the in-code default. Implemented by internal/langfuse.
type TemplateStore interface {
	Template(ctx context.Context, name string) (Prompt, error)
}

// defaultUserTemplates are the in-code user messages of the operations
// served as templates. Their system message is DefaultPrompts, so with
// Langfuse unconfigured the model sees the in-code prompt.
var defaultUserTemplates = map[string]string{
	OpArticleSummary:  "Title: {{title}}\n\nContent: {{content}}",
	OpCategorize:      "Title: {{title}}\n\nContent: {{content}}",
	OpDigestSelect:    "Candidates:\n{{candidates}}",
	OpPreferenceScore: "PREFERENCES.md:\n{{preferences}}\n\nTitle: {{title}}\n\nContent: {{content}}",
}

// defaultSystemSuffixes are appended to the default system prompt of an
// operation whose system message takes variables. The default categorize
// prompt holds no slug list, so its template appends them as {{allowed_slugs}}.
var defaultSystemSuffixes = map[string]string{
	OpCategorize: " Allowed slugs: {{allowed_slugs}}.",
}

// TemplateOperations lists the operations served as prompt templates, in a
// stable order (the seeding command creates one Langfuse prompt for each).
func TemplateOperations() []string {
	ops := make([]string, 0, len(defaultUserTemplates))
	for op := range defaultUserTemplates {
		ops = append(ops, op)
	}
	sort.Strings(ops)
	return ops
}

// DefaultTemplate is an operation's in-code default prompt. ok is false for
// an operation that is not served as a template.
func DefaultTemplate(operation string) (Prompt, bool) {
	user, ok := defaultUserTemplates[operation]
	if !ok {
		return Prompt{}, false
	}
	return Prompt{Name: PromptName(operation), System: DefaultPrompts[operation] + defaultSystemSuffixes[operation], User: user}, true
}

// variableRe matches {{name}}, allowing whitespace inside the braces, as the
// Langfuse SDKs do.
var variableRe = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

// Compile substitutes vars into both messages in a single pass, so text
// inside a value (article content may contain "{{") is never re-scanned.
// The prompt is invalid, and Compile errors, when a message references a
// variable vars lacks or when vars holds one neither message uses: either
// means the template and the code disagree about the inputs.
func (p Prompt) Compile(vars map[string]string) (system, user string, err error) {
	used := map[string]bool{}
	var undefined []string
	compile := func(tmpl string) string {
		return variableRe.ReplaceAllStringFunc(tmpl, func(m string) string {
			name := variableRe.FindStringSubmatch(m)[1]
			v, ok := vars[name]
			if !ok {
				undefined = append(undefined, name)
				return m
			}
			used[name] = true
			return v
		})
	}
	system, user = compile(p.System), compile(p.User)
	var unused []string
	for name := range vars {
		if !used[name] {
			unused = append(unused, name)
		}
	}
	sort.Strings(unused)
	if len(undefined) > 0 || len(unused) > 0 {
		return "", "", fmt.Errorf("prompt %s v%d: undefined variables [%s], unused variables [%s]",
			p.Name, p.Version, strings.Join(undefined, ", "), strings.Join(unused, ", "))
	}
	return system, user, nil
}

// WithTemplates serves the template operations from store (Langfuse), with
// the in-code default as the fallback. Without it (or with nil) every call
// uses the default.
func WithTemplates(store TemplateStore) CloudflareOption {
	return func(p *CloudflareProvider) {
		if store != nil {
			p.templates = store
		}
	}
}

// compiledPrompt is what a call's messages were compiled from, so its
// generation can link the prompt version and record the variables.
type compiledPrompt struct {
	prompt Prompt
	vars   map[string]string
}

// completeTemplate compiles an operation's prompt with vars and makes the
// call. The managed prompt is used when the store has one that compiles;
// otherwise the in-code default is, and an invalid managed prompt is logged
// as a warning. Langfuse is never on the failure path.
func (p *CloudflareProvider) completeTemplate(ctx context.Context, operation string, vars map[string]string, opts callOptions) (string, error) {
	if p.templates != nil {
		if prompt, err := p.templates.Template(ctx, PromptName(operation)); err == nil {
			system, user, err := prompt.Compile(vars)
			if err == nil {
				return p.call(ctx, operation, system, user, opts, &compiledPrompt{prompt, vars})
			}
			slog.Warn("ai: managed prompt is invalid, using the in-code default",
				"component", "ai", "operation", operation, "prompt", prompt.Name, "version", prompt.Version, "err", err)
		}
	}
	prompt, ok := DefaultTemplate(operation)
	if !ok {
		return "", fmt.Errorf("ai: %s has no prompt template", operation)
	}
	system, user, err := prompt.Compile(vars)
	if err != nil {
		return "", err // a bug: the in-code default and its caller disagree
	}
	return p.call(ctx, operation, system, user, opts, &compiledPrompt{prompt, vars})
}
