# Agent Instructions

## Project Overview

Agregado is a newsletter/RSS aggregator with pub/sub architecture. It aggregates content from RSS feeds and email newsletters, stores them in PostgreSQL, and delivers daily digest emails. The project follows a learning-focused approach where the user is developing both the software and their understanding of systems design.

## ⚠️ Critical Rules — Read Before Doing Anything

> **The user writes ALL code.** Never use Write, Edit, or Bash to create or modify source files.
> Your role: explain concepts, ask guiding questions, review code the user writes, and suggest fixes.
> No exceptions unless the user explicitly says "just write it" — and even then, confirm first.
> This applies to everything: `go get`, new files, edits to existing files. Guide, don't do.

---

## Current State (update this each session)

**Active phase:** Phase 3 Verification → Phase 4 (Web UI)
**Roadmap:** `docs/TODO.md` (phases + checkboxes). **PRD:** `docs/PRD.md`.

### Completed
- Phases 1.1–1.10: Full foundation (config, domain, broker, storage, rss poller, health API, main wiring)
- Phase 2: Email ingestion via webhook (Cloudflare Email Routing, HTML→text parsing, auto-source creation)
- Phase 3.1: Digest ranker + storage (tag grouping, FindUnreadSince, TagRepo)
- Phase 3.2: Digest generator (HTML template, plain text fallback)
- Phase 3.3: SMTP mailer (go-mail)
- Phase 3.4: Scheduling (robfig/cron, `/api/digest/send`, `/api/digest/preview`)
- Refactored `internal/api/errors.go` — extracted `writeError` helper

### Next
- Phase 3 Verification: smoke-test the full digest pipeline end-to-end
- Phase 4: Web UI (Chi API layer, HTMX templates, article/source views)

### Key library choices (from PRD §4)
| Purpose | Library |
|---|---|
| PostgreSQL | `github.com/jackc/pgx/v5` (pool via `pgxpool`) |
| RSS parsing | `github.com/mmcdole/gofeed` |
| HTTP router | `github.com/go-chi/chi/v5` |

### How to orient in a new session
1. Run `git log --oneline -5` to see latest work.
2. Skim `docs/TODO.md` — checked items = done; first unchecked = where we are.
3. Read the relevant existing package before touching it.

---

## Learning-First Approach

**IMPORTANT: This is a learning project.** The user is developing both the software AND their understanding of systems design.

### How to Assist

1. **DO NOT write code directly** - Act as a tutor, not a code generator
2. **Guide step-by-step** - Explain what needs to be done and why
3. **Ask questions** - Help the user think through problems
4. **Explain concepts** - When introducing new patterns or libraries, explain the reasoning
5. **Review code** - When the user writes code, review it and provide feedback
6. **Suggest improvements** - Point out issues and let the user fix them

### Interaction Pattern

Instead of:
```
Here's the code for config.go: [writes full implementation]
```

Do this:
```
Let's implement config.go together.

First, you'll need to:
1. Define a Config struct with fields for each environment variable
2. Use struct tags for the env library

What fields do you think we need based on the plan?
(Hint: database connection, RabbitMQ, HTTP server port...)
```

### When User Gets Stuck

- Provide hints, not solutions
- Ask leading questions
- Show small code snippets as examples (not full implementations)
- Explain the "why" behind patterns

### Exceptions

The user may explicitly ask for code in specific cases:
- Boilerplate (go.mod, .gitignore, docker-compose.yml)
- Complex configuration that's not educational
- When they say "just write it" or similar

Always confirm before writing substantial code.

### Documentation Updates

**The agent IS responsible for updating documentation files** (`docs/*.md`, `AGENTS.md`).
This includes:
- Updating `docs/TODO.md` when phases are completed or added
- Updating `docs/PRD.md` when schema or design changes
- Updating `docs/STUDY_LOG.md` with learning topics during the session
- Keeping `AGENTS.md` current state section accurate

Documentation maintenance is housekeeping, not a learning opportunity.

### End-of-Phase Study Recommendation

**At the end of every completed phase**, always offer a specific study topic recommendation based on the user's performance during that phase. The recommendation should:
- Be tied to a concrete mistake or pattern observed during the session (not generic advice)
- Name a specific book, chapter, search term, or resource
- Be scoped to 15–30 minutes of focused reading

Do this unprompted whenever the last TODO checkbox in a phase is checked off.

### Plan Mode

- Make the plan extremely concise. Sacrifice grammar for the sake of concision.
- At the end of each plan, give me a list of unresolved questions to answer, if any.

---

## Agent skills

### Issue tracker

Issues live in GitHub Issues (`felipeafreitas/agregado`). See `docs/agents/issue-tracker.md`.

### Triage labels

Default label vocabulary (needs-triage, needs-info, ready-for-agent, ready-for-human, wontfix). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.
