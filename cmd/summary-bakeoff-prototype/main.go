// PROTOTYPE — summary bake-off for agregado#86. Throwaway: lives only on the
// prototype/summary-bakeoff branch, never merged.
//
// Question: which Article Summary strategy gives the Digest's selection call
// the best input per Neuron? The Summary is machine input (CONTEXT.md), so it
// is judged downstream: the same frozen candidate set goes through the real
// selection prompt once per arm, and you rank the resulting Digests blind.
//
// Arms:
//
//	lead       title + first 150 words of the content, no model
//	current    today's OpSummarize prompt on AI_MODEL (400-char excerpt)
//	structured purpose-built claims/entities/kind prompt on CHEAP_MODEL
//	           (default gemma-4-26b: the cheap tier, whatever production runs)
//	long       no Summary model; selection reads the first 800 words
//
// Held fixed: candidate preparation (digestartifact's own), the
// OpDigestSelect prompt, and SELECT_MODEL. Content that cannot be fetched
// falls back to Miniflux's feed content for every arm, marked partial.
//
// Run (anywhere the Agregado Postgres is reachable):
//
//	DATABASE_HOST=… DATABASE_USER=… DATABASE_PASSWORD=… DATABASE_DB=… \
//	CLOUDFLARE_ACCOUNT_ID=… CLOUDFLARE_API_TOKEN=… AI_MODEL=<production model> \
//	MINIFLUX_URL=… MINIFLUX_API_KEY=… \
//	go run ./cmd/summary-bakeoff-prototype
//
// Optional: DAYS=2026-09-29,2026-09-30,2026-10-01 (default: the 3 most recent
// days with candidates, excluding today), SELECT_MODEL (default gpt-oss-120b; kimi is paid-plan only),
// CHEAP_MODEL (default gemma-4-26b-a4b-it),
// OUT (default ./summary-bakeoff-out).
//
// Output: OUT/<day>.html (four blind Digests, A–D), OUT/RANKING.md (fill in),
// OUT/_key/key.json + OUT/_key/metrics.csv (open only after ranking).
package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/caarlos0/env/v10"
	"github.com/felipeafreitas/agregado/internal/ai"
	"github.com/felipeafreitas/agregado/internal/config"
	"github.com/felipeafreitas/agregado/internal/digestartifact"
	"github.com/felipeafreitas/agregado/internal/ingestion/fetch"
	"github.com/felipeafreitas/agregado/internal/textutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

const structuredPrompt = `You prepare an article for a model that will decide whether it belongs in a reader's daily digest. Nobody reads your output except that model. Return only JSON:
{"kind":"news|analysis|tutorial|opinion|announcement|newsletter|other","claims":["the article's main claim or finding, one sentence each, at most 3"],"entities":["people, companies, products, places it is about, at most 6"]}
Be factual: state only what the content says. If the content is a stub or truncated, say so in claims.`

// Neurons per million tokens, from developers.cloudflare.com/workers-ai/platform/pricing.
var neuronRates = map[string][2]float64{
	"@cf/moonshotai/kimi-k2.6":             {86364, 363636},
	"@cf/moonshotai/kimi-k2.7-code":        {86364, 363636},
	"@cf/google/gemma-4-26b-a4b-it":        {9091, 27273},
	"@cf/openai/gpt-oss-120b":              {31818, 68182},
	"@cf/deepseek-ai/deepseek-v4-pro-0813": {120000, 360000},
}

var arms = []string{"lead", "current", "structured", "long"}

type usage struct {
	In, Out int
	Ms      int64
	Model   string
}

func (u usage) neurons() float64 {
	if u.In+u.Out == 0 {
		return 0
	}
	r, ok := neuronRates[u.Model]
	if !ok {
		return -1
	}
	return (float64(u.In)*r[0] + float64(u.Out)*r[1]) / 1e6
}

type candidate struct {
	digestartifact.Candidate
	MinifluxID int64
	Content    string
	Partial    bool
}

type armResult struct {
	Arm        string
	Summaries  map[string]string
	Choices    []digestartifact.Choice
	SummaryUse usage
	SelectUse  usage
	SelectErr  string
}

type client struct {
	account, token string
	http           *http.Client
}

func (c client) complete(ctx context.Context, model, system, user string) (string, usage, error) {
	body, _ := json.Marshal(map[string]any{"messages": []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}}})
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run/%s", c.account, model)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := c.http.Do(req)
	u := usage{Model: model}
	if err != nil {
		return "", u, err
	}
	defer resp.Body.Close()
	u.Ms = time.Since(start).Milliseconds()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Success bool `json:"success"`
		Result  struct {
			Response json.RawMessage `json:"response"`
			Choices  []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Usage struct {
				Prompt     int `json:"prompt_tokens"`
				Completion int `json:"completion_tokens"`
			} `json:"usage"`
		} `json:"result"`
		Errors any `json:"errors"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || !out.Success {
		return "", u, fmt.Errorf("cloudflare %s: %s", model, textutil.Truncate(string(raw), 300))
	}
	u.In, u.Out = out.Result.Usage.Prompt, out.Result.Usage.Completion
	if len(out.Result.Choices) > 0 {
		return out.Result.Choices[0].Message.Content, u, nil
	}
	var s string
	if json.Unmarshal(out.Result.Response, &s) == nil {
		return s, u, nil
	}
	return string(out.Result.Response), u, nil
}

func main() {
	ctx := context.Background()
	var dbc config.Database
	if err := env.Parse(&dbc); err != nil {
		log.Fatal(err)
	}
	current, selectModel := must("AI_MODEL"), envOr("SELECT_MODEL", "@cf/openai/gpt-oss-120b")
	cheap := envOr("CHEAP_MODEL", "@cf/google/gemma-4-26b-a4b-it")
	cf := client{must("CLOUDFLARE_ACCOUNT_ID"), must("CLOUDFLARE_API_TOKEN"), &http.Client{Timeout: 5 * time.Minute}}
	floor, _ := strconv.Atoi(envOr("DIGEST_MIN_SCORE", "3"))
	maxChars, _ := strconv.Atoi(envOr("AI_MAX_CONTENT_CHARS", "8000"))
	outDir := envOr("OUT", "summary-bakeoff-out")
	os.MkdirAll(filepath.Join(outDir, "_key"), 0o755)

	pool, err := pgxpool.New(ctx, fmt.Sprintf("postgres://%s:%s@%s:%s/%s", dbc.User, dbc.Password, dbc.Host, dbc.Port, dbc.Name))
	if err != nil {
		log.Fatal(err)
	}
	prompt := func(op string) string {
		var p string
		if pool.QueryRow(ctx, "SELECT system_prompt FROM ai_prompts WHERE operation = $1", op).Scan(&p) != nil || p == "" {
			return ai.DefaultPrompts[op]
		}
		return p
	}
	summarizePrompt, selectPrompt := prompt(ai.OpSummarize), prompt(ai.OpDigestSelect)
	fetcher := fetch.New(15*time.Second, 5<<20, 500, "Agregado/1.0 (+https://github.com/felipeafreitas/agregado)")

	days := pickDays(ctx, pool)
	key := map[string]map[string]string{}
	metrics := [][]string{{"day", "arm", "candidates", "partial", "selected", "summary_model", "summary_in_tok", "summary_out_tok", "summary_ms", "summary_neurons", "select_model", "select_in_tok", "select_out_tok", "select_ms", "select_neurons", "neurons_total_day", "pct_free_daily_10k", "select_error"}}
	var ranking strings.Builder
	ranking.WriteString("# Summary bake-off — your ranking (agregado#86)\n\nFor each day, rank A–D (1 = best) and mark any Digest that is **clearly worse**: it misses an Article you'd have wanted, or a \"why\" is wrong/misleading. Slightly worse wording does not count. Only then open `_key/`.\n\n")

	for _, day := range days {
		log.Printf("== %s", day.Format("2006-01-02"))
		cands := loadCandidates(ctx, pool, day, floor)
		if len(cands) == 0 {
			log.Printf("no candidates, skipping")
			continue
		}
		partial := 0
		for i := range cands {
			c := &cands[i]
			if r, err := fetcher.Fetch(ctx, c.CanonicalURL); err == nil && strings.TrimSpace(r.Markdown) != "" {
				c.Content = r.Markdown
			} else {
				c.Content, c.Partial = minifluxContent(ctx, c.MinifluxID), true
				partial++
				log.Printf("partial: %s (%v)", c.CanonicalURL, err)
			}
		}

		var results []armResult
		for _, arm := range arms {
			res := armResult{Arm: arm, Summaries: map[string]string{}, SummaryUse: usage{}}
			for _, c := range cands {
				var s string
				switch arm {
				case "lead":
					s = c.Title + ". " + words(c.Content, 150)
				case "long":
					s = c.Title + ". " + words(c.Content, 800)
				case "current":
					var u usage
					s, u, err = cf.complete(ctx, current, summarizePrompt, fmt.Sprintf("Articles:\n- %s\n  Excerpt: %s\n\nSummary:", c.Title, textutil.Clean(c.Content, 400)))
					res.SummaryUse = add(res.SummaryUse, u)
				case "structured":
					var u usage
					s, u, err = cf.complete(ctx, cheap, structuredPrompt, fmt.Sprintf("Title: %s\n\nContent: %s", c.Title, textutil.Clean(c.Content, maxChars)))
					res.SummaryUse = add(res.SummaryUse, u)
				}
				if err != nil {
					log.Printf("%s summary failed for %s: %v", arm, c.ID, err)
					s = c.Title
				}
				if c.Partial {
					s = "[content: partial] " + s
				}
				res.Summaries[c.ID] = strings.TrimSpace(s)
			}
			res.Choices, res.SelectUse, err = selectFor(ctx, cf, selectModel, selectPrompt, cands, res.Summaries)
			if err != nil {
				res.SelectErr = err.Error()
				log.Printf("%s select failed: %v", arm, err)
			}
			log.Printf("%s: %d chosen, summary %d/%d tok, select %d/%d tok", arm, len(res.Choices), res.SummaryUse.In, res.SummaryUse.Out, res.SelectUse.In, res.SelectUse.Out)
			results = append(results, res)
		}

		order := rand.Perm(len(results))
		letters := map[string]string{}
		var blind []armResult
		for i, idx := range order {
			letters[string(rune('A'+i))] = results[idx].Arm
			blind = append(blind, results[idx])
		}
		ds := day.Format("2006-01-02")
		key[ds] = letters
		writeDay(filepath.Join(outDir, ds+".html"), ds, cands, blind)
		fmt.Fprintf(&ranking, "## %s  (%d candidates, %d partial)\n\n| Digest | Rank | Clearly worse? Why |\n|---|---|---|\n| A | | |\n| B | | |\n| C | | |\n| D | | |\n\n", ds, len(cands), partial)
		for _, r := range results {
			sn, se := r.SummaryUse.neurons(), r.SelectUse.neurons()
			total := sn + se
			metrics = append(metrics, []string{ds, r.Arm, itoa(len(cands)), itoa(partial), itoa(len(r.Choices)), r.SummaryUse.Model,
				itoa(r.SummaryUse.In), itoa(r.SummaryUse.Out), strconv.FormatInt(r.SummaryUse.Ms, 10), f(sn),
				selectModel, itoa(r.SelectUse.In), itoa(r.SelectUse.Out), strconv.FormatInt(r.SelectUse.Ms, 10), f(se), f(total), f(total / 100), r.SelectErr})
		}
	}

	k, _ := json.MarshalIndent(key, "", "  ")
	os.WriteFile(filepath.Join(outDir, "_key", "key.json"), k, 0o644)
	mf, _ := os.Create(filepath.Join(outDir, "_key", "metrics.csv"))
	csv.NewWriter(mf).WriteAll(metrics)
	mf.Close()
	os.WriteFile(filepath.Join(outDir, "RANKING.md"), []byte(ranking.String()), 0o644)
	log.Printf("done → %s (rank first, then open _key/)", outDir)
}

func selectFor(ctx context.Context, cf client, model, system string, cands []candidate, summaries map[string]string) ([]digestartifact.Choice, usage, error) {
	// Same payload shape as ai.CloudflareProvider.Select.
	type payload struct {
		ID, Title, Summary string
		Score              int
		Topics             []string
		Exploration        bool
	}
	ps := make([]payload, len(cands))
	for i, c := range cands {
		ps[i] = payload{c.ID, c.Title, summaries[c.ID], c.Score, c.Topics, c.Exploration}
	}
	data, _ := json.Marshal(ps)
	out, u, err := cf.complete(ctx, model, system, "Candidates:\n"+string(data))
	if err != nil {
		return nil, u, err
	}
	var resp struct {
		Choices []digestartifact.Choice `json:"choices"`
	}
	start, end := strings.Index(out, "{"), strings.LastIndex(out, "}")
	if start < 0 || end < start {
		return nil, u, fmt.Errorf("no JSON in selection: %s", textutil.Truncate(out, 200))
	}
	if err := json.Unmarshal([]byte(out[start:end+1]), &resp); err != nil {
		return nil, u, fmt.Errorf("decode selection: %w", err)
	}
	return resp.Choices, u, nil
}

func loadCandidates(ctx context.Context, pool *pgxpool.Pool, day time.Time, floor int) []candidate {
	// Same query as storage.DigestArtifactRepo.Candidates, plus the Miniflux entry id.
	rows, err := pool.Query(ctx, `SELECT id, canonical_url, title, COALESCE(summary, ''), COALESCE(source_id, ''), score, tags, COALESCE(miniflux_entry_id, 0) FROM article_index WHERE processing_status = 'complete' AND published_at >= $1 AND published_at < $1 + INTERVAL '1 day' ORDER BY score DESC, published_at DESC`, day)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	var articles []digestartifact.Article
	ids := map[string]int64{}
	for rows.Next() {
		var a digestartifact.Article
		var tags []byte
		var mid int64
		if err := rows.Scan(&a.ID, &a.CanonicalURL, &a.Title, &a.Summary, &a.Source, &a.Score, &tags, &mid); err != nil {
			log.Fatal(err)
		}
		json.Unmarshal(tags, &a.Topics)
		ids[a.ID] = mid
		articles = append(articles, a)
	}
	var out []candidate
	for _, c := range digestartifact.PrototypePrepare(articles, floor, 10) {
		out = append(out, candidate{Candidate: c, MinifluxID: ids[c.ID]})
	}
	return out
}

func pickDays(ctx context.Context, pool *pgxpool.Pool) []time.Time {
	var days []time.Time
	if s := os.Getenv("DAYS"); s != "" {
		for _, d := range strings.Split(s, ",") {
			t, err := time.Parse("2006-01-02", strings.TrimSpace(d))
			if err != nil {
				log.Fatal(err)
			}
			days = append(days, t)
		}
		return days
	}
	rows, err := pool.Query(ctx, `SELECT DISTINCT date_trunc('day', published_at AT TIME ZONE 'UTC') AS d FROM article_index WHERE processing_status = 'complete' AND published_at < date_trunc('day', now()) ORDER BY d DESC LIMIT 3`)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var t time.Time
		rows.Scan(&t)
		days = append(days, time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC))
	}
	return days
}

func minifluxContent(ctx context.Context, id int64) string {
	base, key := os.Getenv("MINIFLUX_URL"), os.Getenv("MINIFLUX_API_KEY")
	if base == "" || key == "" || id == 0 {
		return ""
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(base, "/")+"/v1/entries/"+strconv.FormatInt(id, 10), nil)
	req.Header.Set("X-Auth-Token", key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var e struct {
		Content string `json:"content"`
	}
	json.NewDecoder(resp.Body).Decode(&e)
	return textutil.Strip(e.Content)
}

var page = template.Must(template.New("p").Parse(`<!doctype html><meta charset=utf-8><title>Bake-off {{.Day}}</title>
<style>body{font:15px/1.45 system-ui;margin:1.5rem}h1{font-size:1.2rem}.grid{display:grid;grid-template-columns:repeat(4,1fr);gap:1rem}.col{border:1px solid #ccc;border-radius:8px;padding:.8rem}li{margin:.5rem 0}.why{color:#333}.x{background:#ffe9a8;font-size:.75rem;padding:0 .3rem;border-radius:3px}.miss{color:#888;font-size:.85rem}.err{color:#b00}</style>
<h1>Summary bake-off — {{.Day}} · {{len .Cands}} candidates (PROTOTYPE, agregado#86)</h1>
<p>Four Digests from the same candidates, A–D shuffled. Rank them in RANKING.md before opening _key/.</p>
<div class=grid>{{range .Cols}}<div class=col><h2>{{.Letter}}</h2>{{if .Err}}<p class=err>selection failed: {{.Err}}</p>{{end}}<ol>{{range .Items}}<li><a href="{{.URL}}">{{.Title}}</a>{{if .Exploration}} <span class=x>exploration</span>{{end}}<div class=why>{{.Why}}</div></li>{{end}}</ol>
<p class=miss><b>Not selected:</b>{{range .Missed}}<br>· {{.}}{{end}}</p></div>{{end}}</div>`))

func writeDay(path, day string, cands []candidate, blind []armResult) {
	type item struct {
		URL, Title, Why string
		Exploration     bool
	}
	type col struct {
		Letter, Err string
		Items       []item
		Missed      []string
	}
	byID := map[string]candidate{}
	for _, c := range cands {
		byID[c.ID] = c
	}
	var cols []col
	for i, r := range blind {
		cl := col{Letter: string(rune('A' + i)), Err: r.SelectErr}
		chosen := map[string]bool{}
		for _, ch := range r.Choices {
			c, ok := byID[ch.ArticleID]
			if !ok {
				continue
			}
			chosen[c.ID] = true
			cl.Items = append(cl.Items, item{c.CanonicalURL, c.Title, ch.Why, c.Exploration})
		}
		for _, c := range cands {
			if !chosen[c.ID] {
				cl.Missed = append(cl.Missed, c.Title)
			}
		}
		cols = append(cols, cl)
	}
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	page.Execute(f, map[string]any{"Day": day, "Cands": cands, "Cols": cols})
}

func words(s string, n int) string {
	w := strings.Fields(textutil.Strip(s))
	if len(w) > n {
		w = w[:n]
	}
	return strings.Join(w, " ")
}
func add(a, b usage) usage {
	return usage{In: a.In + b.In, Out: a.Out + b.Out, Ms: a.Ms + b.Ms, Model: b.Model}
}
func itoa(i int) string  { return strconv.Itoa(i) }
func f(x float64) string { return strconv.FormatFloat(x, 'f', 1, 64) }
func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func must(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("%s is required", k)
	}
	return v
}
