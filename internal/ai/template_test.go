package ai

import (
	"context"
	"testing"
)

func TestPromptCompile(t *testing.T) {
	tests := []struct {
		name       string
		system     string
		user       string
		vars       map[string]string
		wantSystem string
		wantUser   string
		wantErr    bool
	}{
		{
			name:       "substitutes every variable",
			system:     "Score for {{reader}}.",
			user:       "Title: {{title}}\n\nContent: {{content}}",
			vars:       map[string]string{"reader": "Felipe", "title": "Go 1.26", "content": "Released."},
			wantSystem: "Score for Felipe.",
			wantUser:   "Title: Go 1.26\n\nContent: Released.",
		},
		{
			name:     "trims whitespace inside the braces",
			user:     "{{ title }}|{{content  }}",
			vars:     map[string]string{"title": "a", "content": "b"},
			wantUser: "a|b",
		},
		{
			name:     "a variable used twice is substituted twice",
			user:     "{{title}} / {{title}}",
			vars:     map[string]string{"title": "x"},
			wantUser: "x / x",
		},
		{
			name:     "substituted values are not re-scanned",
			user:     "Content: {{content}}",
			vars:     map[string]string{"content": "see {{title}} and {{content}}"},
			wantUser: "Content: see {{title}} and {{content}}",
		},
		{
			name:       "text without braces is copied verbatim",
			system:     "Return only the integer.",
			user:       "{{title}} { not a var } {{ }}",
			vars:       map[string]string{"title": "t"},
			wantSystem: "Return only the integer.",
			wantUser:   "t { not a var } {{ }}",
		},
		{
			name:    "an undefined variable makes the prompt invalid",
			user:    "{{title}} {{author}}",
			vars:    map[string]string{"title": "t"},
			wantErr: true,
		},
		{
			name:    "an unused variable makes the prompt invalid",
			user:    "{{title}}",
			vars:    map[string]string{"title": "t", "content": "c"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Prompt{Name: "agregado/test", Version: 1, System: tt.system, User: tt.user}
			system, user, err := p.Compile(tt.vars)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Compile() = %q, %q, want an error", system, user)
				}
				return
			}
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			if system != tt.wantSystem || user != tt.wantUser {
				t.Fatalf("Compile() = %q, %q, want %q, %q", system, user, tt.wantSystem, tt.wantUser)
			}
		})
	}
}

// Each operation's in-code default compiles, with representative variables,
// to exactly the messages Go built before the prompt became a template.
func TestDefaultTemplatesCompileToThePreTemplateMessages(t *testing.T) {
	tests := []struct {
		op                   string
		vars                 map[string]string
		wantSystem, wantUser string
	}{
		{
			op:         OpArticleSummary,
			vars:       map[string]string{"title": "Go 1.26", "content": "[content: partial]\nReleased."},
			wantSystem: DefaultPrompts[OpArticleSummary],
			wantUser:   "Title: Go 1.26\n\nContent: [content: partial]\nReleased.",
		},
		{
			op:         OpCategorize,
			vars:       map[string]string{"allowed_slugs": "tech, science", "title": "Go 1.26", "content": "Released."},
			wantSystem: DefaultPrompts[OpCategorize] + " Allowed slugs: tech, science.",
			wantUser:   "Title: Go 1.26\n\nContent: Released.",
		},
		{
			op:         OpDigestSelect,
			vars:       map[string]string{"candidates": `[{"ID":"a"}]`},
			wantSystem: DefaultPrompts[OpDigestSelect],
			wantUser:   "Candidates:\n" + `[{"ID":"a"}]`,
		},
		{
			op:         OpPreferenceScore,
			vars:       map[string]string{"preferences": "likes Go", "title": "Go 1.26", "content": "Released."},
			wantSystem: DefaultPrompts[OpPreferenceScore],
			wantUser:   "PREFERENCES.md:\nlikes Go\n\nTitle: Go 1.26\n\nContent: Released.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.op, func(t *testing.T) {
			prompt, ok := DefaultTemplate(context.Background(), nil, tt.op)
			if !ok {
				t.Fatalf("%s has no default template", tt.op)
			}
			if prompt.Name != "agregado/"+tt.op || prompt.Version != 0 {
				t.Fatalf("prompt = %s v%d, want agregado/%s v0", prompt.Name, prompt.Version, tt.op)
			}
			system, user, err := prompt.Compile(tt.vars)
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			if system != tt.wantSystem || user != tt.wantUser {
				t.Fatalf("Compile() = %q, %q, want %q, %q", system, user, tt.wantSystem, tt.wantUser)
			}
		})
	}
}

func TestCategorizeDefaultAppendsTheSlugsToTheAIPromptsRow(t *testing.T) {
	prompt, _ := DefaultTemplate(context.Background(), fakePromptStore{OpCategorize: "Edited in admin."}, OpCategorize)
	if want := "Edited in admin. Allowed slugs: {{allowed_slugs}}."; prompt.System != want {
		t.Fatalf("system = %q, want %q", prompt.System, want)
	}
}
