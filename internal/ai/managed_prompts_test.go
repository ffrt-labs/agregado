package ai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/felipeafreitas/agregado/internal/digestartifact"
)

type fakeTags []string

func (f fakeTags) CategorySlugs(context.Context) ([]string, error) { return f, nil }

// templateCall is one of the enrichment and Digest operations served as a
// prompt template since #203, run with fixed inputs and live tags "tech" and
// "science".
type templateCall struct {
	op     string
	answer string // the stub's reply; "" answers "4"
	run    func(t *testing.T, p *CloudflareProvider)
	// The messages Go built before the operation became a template.
	wantSystem, wantUser string
	wantInput            string // the generation input: the variables
	managed              Prompt // a Langfuse version using every variable
	managedSystem        string
	managedUser          string
}

var selectCandidates = []digestartifact.Candidate{{Article: digestartifact.Article{ID: "a", Title: "Go 1.26", Summary: "news", Score: 5, Topics: []string{"tech"}}}}

const selectPayload = `[{"ID":"a","Title":"Go 1.26","Summary":"news","Score":5,"Topics":["tech"],"Exploration":false}]`

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

var templateCalls = []templateCall{
	{
		op: OpArticleSummary,
		run: func(t *testing.T, p *CloudflareProvider) {
			if _, err := p.SummarizeArticle(context.Background(), "Go 1.26", "[content: partial]\nReleased today."); err != nil {
				t.Fatal(err)
			}
		},
		wantSystem: DefaultPrompts[OpArticleSummary],
		// Clean collapses whitespace, as it did before templates.
		wantUser:      "Title: Go 1.26\n\nContent: [content: partial] Released today.",
		wantInput:     `{"content":"[content: partial] Released today.","title":"Go 1.26"}`,
		managed:       Prompt{Name: "agregado/article_summary", Version: 2, System: "v2 summary", User: "{{title}} | {{content}}"},
		managedSystem: "v2 summary",
		managedUser:   "Go 1.26 | [content: partial] Released today.",
	},
	{
		op: OpCategorize,
		run: func(t *testing.T, p *CloudflareProvider) {
			if _, err := p.Categorize(context.Background(), "Go 1.26", "Released today."); err != nil {
				t.Fatal(err)
			}
		},
		wantSystem: DefaultPrompts[OpCategorize] + " Allowed slugs: tech, science.",
		wantUser:   "Title: Go 1.26\n\nContent: Released today.",
		wantInput:  `{"allowed_slugs":"tech, science","content":"Released today.","title":"Go 1.26"}`,
		// A version that rephrases how the slugs are given.
		managed:       Prompt{Name: "agregado/categorize", Version: 2, System: "Pick one of:\n{{allowed_slugs}}", User: "{{title}} | {{content}}"},
		managedSystem: "Pick one of:\ntech, science",
		managedUser:   "Go 1.26 | Released today.",
	},
	{
		op:     OpDigestSelect,
		answer: `{"article_ids":["a"]}`,
		run: func(t *testing.T, p *CloudflareProvider) {
			choices, err := p.Select(context.Background(), selectCandidates)
			if err != nil || len(choices) != 1 || choices[0].ArticleID != "a" {
				t.Fatalf("Select() = %v, %v", choices, err)
			}
		},
		wantSystem:    DefaultPrompts[OpDigestSelect],
		wantUser:      "Candidates:\n" + selectPayload,
		wantInput:     `{"candidates":` + jsonString(selectPayload) + `}`,
		managed:       Prompt{Name: "agregado/digest_select", Version: 2, System: "v2 select", User: "Pick from {{candidates}}"},
		managedSystem: "v2 select",
		managedUser:   "Pick from " + selectPayload,
	},
}

func TestTemplateOperationsWithoutLangfuseSendTheSameMessagesAsBefore(t *testing.T) {
	for _, tc := range templateCalls {
		t.Run(tc.op, func(t *testing.T) {
			run := newRun(t, nil, fakeTags{"tech", "science"})
			run.stub.answer = tc.answer
			tc.run(t, run.provider)

			system, user := run.stub.messages()
			if system != tc.wantSystem || user != tc.wantUser {
				t.Fatalf("messages = %q, %q, want %q, %q", system, user, tc.wantSystem, tc.wantUser)
			}
			attrs := run.generationOf(t, tc.op)
			if _, ok := attrs["langfuse.observation.prompt.name"]; ok {
				t.Fatalf("an in-code default is linked to a Langfuse prompt: %v", attrs)
			}
			if got := attrs["langfuse.observation.input"]; got != tc.wantInput {
				t.Fatalf("generation input = %s, want the variables %s", got, tc.wantInput)
			}
		})
	}
}

func TestTemplateOperationsUseTheLangfusePromptAndLinkTheGeneration(t *testing.T) {
	for _, tc := range templateCalls {
		t.Run(tc.op, func(t *testing.T) {
			templates := &fakeTemplates{prompt: tc.managed}
			run := newRun(t, nil, fakeTags{"tech", "science"}, WithTemplates(templates))
			run.stub.answer = tc.answer
			tc.run(t, run.provider)

			if len(templates.asked) != 1 || templates.asked[0] != tc.managed.Name {
				t.Fatalf("templates asked for %v, want %s", templates.asked, tc.managed.Name)
			}
			system, user := run.stub.messages()
			if system != tc.managedSystem || user != tc.managedUser {
				t.Fatalf("messages = %q, %q, want %q, %q", system, user, tc.managedSystem, tc.managedUser)
			}
			attrs := run.generationOf(t, tc.op)
			if attrs["langfuse.observation.prompt.name"] != tc.managed.Name || attrs["langfuse.observation.prompt.version"] != "2" {
				t.Fatalf("generation prompt = %q v%q, want %s v2",
					attrs["langfuse.observation.prompt.name"], attrs["langfuse.observation.prompt.version"], tc.managed.Name)
			}
		})
	}
}

func TestTemplateOperationsFallBackToTheDefaultWhenTheLangfusePromptIsInvalid(t *testing.T) {
	for _, tc := range templateCalls {
		t.Run(tc.op, func(t *testing.T) {
			invalid := tc.managed
			invalid.User += " {{author}}"
			run := newRun(t, nil, fakeTags{"tech", "science"}, WithTemplates(&fakeTemplates{prompt: invalid}))
			run.stub.answer = tc.answer
			tc.run(t, run.provider)

			system, user := run.stub.messages()
			if system != tc.wantSystem || user != tc.wantUser {
				t.Fatalf("messages = %q, %q, want the in-code default", system, user)
			}
		})
	}
}

func TestCategorizeCheckedStillChecksTheAnswerAgainstTheLiveSlugs(t *testing.T) {
	run := newRun(t, nil, fakeTags{"tech", "science"}, WithTemplates(&fakeTemplates{prompt: templateCalls[1].managed}))
	run.stub.answer = "Science"
	slug, allowed, err := run.provider.CategorizeChecked(context.Background(), "Go 1.26", "Released today.")
	if err != nil || slug != "Science" || !allowed {
		t.Fatalf("CategorizeChecked() = %q, %v, %v; want Science, allowed", slug, allowed, err)
	}
}
