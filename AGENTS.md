# Agent Instructions

## Project Overview

Agregado is a newsletter/RSS aggregator with pub/sub architecture.

---

### Documentation Updates

**The agent IS responsible for updating documentation files** (`docs/*.md`, `AGENTS.md`).
This includes:
- Filing/updating GitHub Issues when work is specced, completed, or added
  (`gh issue list`; see `docs/agents/issue-tracker.md`). **Never add to
  `docs/ROADMAP-ARCHIVE.md`** — it's frozen history
- Writing an ADR in `docs/adr/` when a decision resolves, especially a deliberate
  *non*-goal. Non-goals are decisions, not backlog items — don't file them as issues
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

---

## Agent skills

### Issue tracker

Issues live in GitHub Issues (`ffrt-labs/agregado`). See `docs/agents/issue-tracker.md`.

### Triage labels

Default label vocabulary (needs-triage, needs-info, ready-for-agent, ready-for-human, wontfix). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.
