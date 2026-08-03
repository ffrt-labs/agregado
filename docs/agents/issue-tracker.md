# Issue tracker: GitHub

Issues and PRDs for this repo live as GitHub issues. Use the `gh` CLI for all operations.

## Conventions

- **Create an issue**: `gh issue create --title "..." --body "..."`. Use a heredoc for multi-line bodies.
- **Read an issue**: `gh issue view <number> --comments`, filtering comments by `jq` and also fetching labels.
- **List issues**: `gh issue list --state open --json number,title,body,labels,comments --jq '[.[] | {number, title, body, labels: [.labels[].name], comments: [.comments[].body]}]'` with appropriate `--label` and `--state` filters.
- **Comment on an issue**: `gh issue comment <number> --body "..."`
- **Apply / remove labels**: `gh issue edit <number> --add-label "..."` / `--remove-label "..."`
- **Close**: `gh issue close <number> --comment "..."`

Infer the repo from `git remote -v` — `gh` does this automatically when run inside a clone.

## When a skill says "publish to the issue tracker"

Create a GitHub issue.

## When a skill says "fetch the relevant ticket"

Run `gh issue view <number> --comments`.

## Authentication

The sandbox proxy injects real GitHub credentials for `api.github.com`, but `gh`
refuses to start without *some* token in the environment and its error message
("please run gh auth login") reads like an auth failure. Set any placeholder —
the proxy substitutes the real credential:

```bash
export GH_TOKEN=proxy-injected   # value is irrelevant
```

Verify with a **read** (`gh issue list`), never `gh auth status`, which reports
"not logged in" even when credentials are being injected — and never with a
write, which will create a real issue.

## Wayfinding operations

Maps and tickets for the `wayfinder` skill are GitHub issues. Everything below
uses GitHub's **native** relationships, so the frontier renders in GitHub's own
UI without opening the map.

**Labels** (all exist already):

| Label | Meaning |
|---|---|
| `wayfinder:map` | The shared map for a wayfinding effort |
| `wayfinder:research` | AFK ticket — resolved by a research subagent |
| `wayfinder:prototype` | HITL ticket — resolved by building something rough |
| `wayfinder:grilling` | HITL ticket — resolved by conversation |
| `wayfinder:task` | Manual work that unblocks a decision |

**Create a map**: `gh issue create --label "wayfinder:map" --body-file <file>`.
Title as `Map — <effort>`.

**Create tickets**, then wire them in a **second pass** — issues need ids before
they can reference each other. Note both APIs take the issue's **`id`** (the
global database id from `gh api .../issues/<n> --jq .id`), *not* its number, and
`gh api` must pass it with **`-F`** so it is sent as an integer; `-f` sends a
string and fails with HTTP 422.

**Parent/child** — native sub-issues:

```bash
gh api repos/ffrt-labs/agregado/issues/<map>/sub_issues -X POST -F sub_issue_id=<child-id>
gh api repos/ffrt-labs/agregado/issues/<map>/sub_issues --jq '.[] | "\(.number) \(.state) \(.title)"'
```

**Blocking** — native issue dependencies:

```bash
gh api repos/ffrt-labs/agregado/issues/<blocked>/dependencies/blocked_by -X POST -F issue_id=<blocker-id>
gh api repos/ffrt-labs/agregado/issues/<n>/dependencies/blocked_by --jq '.[] | "\(.number) \(.title)"'
```

**Claiming** is assignment: `gh issue edit <n> --add-assignee @me`. Claim
*before* any work, so concurrent sessions skip the ticket.

**The frontier** — open, unblocked, unclaimed children of a map:

```bash
gh api repos/ffrt-labs/agregado/issues/<map>/sub_issues \
  --jq '.[] | select(.state=="open") | select(.assignees|length==0) | .number' |
while read -r n; do
  if [ "$(gh api repos/ffrt-labs/agregado/issues/$n/dependencies/blocked_by --jq 'map(select(.state=="open"))|length')" = 0 ]; then
    gh issue view "$n" --json number,title --jq '"\(.number)\t\(.title)"'
  fi
done
```

**Resolving a ticket**: post the answer as a comment, close the issue, then
append a one-line pointer to the map's *Decisions so far*.
