# Research — cheerio in n8n's Code node (self-hosted)

**Date:** 2026-09-22
**Issue:** [#114](https://github.com/ffrt-labs/agregado/issues/114) (part of the wayfinder map, [#110](https://github.com/ffrt-labs/agregado/issues/110))

## Context / Question

The Resend-webhook newsletter-ingestion redesign runs entirely inside n8n
(self-hosted, homelab, **not** defined in this repo — its version/config live
in a separate "homelab-edge" repo or on the live instance only). The plan is:
Resend webhook → Code node verifies the Svix signature by hand → cheerio-based
HTML extraction chain, replacing an earlier Cloudflare Workers `HTMLRewriter`
design that has no n8n equivalent.

Question: does n8n's Code node support `cheerio` out of the box, and if not,
exactly what has to be configured (env vars, restart, custom Docker image) to
make it work — and what sandboxing model does the Code node currently run
under, with what caveats for cheerio and its transitive deps (parse5,
htmlparser2, domhandler, dom-serializer)?

## Findings

### 1. Cheerio is not a built-in module

n8n's own docs and community forum consistently treat `cheerio` as an
**external** module that must be explicitly required and allowlisted, not
something pre-bundled into the Code node's global scope. The
[Code node — common issues](https://docs.n8n.io/integrations/builtin/core-nodes/n8n-nodes-base.code/common-issues)
page uses `cheerio` itself as the example of a module accessed via
`require()` that needs the external-module configuration below — i.e. n8n's
own docs treat cheerio as the canonical example of a *not*-built-in package.
I could not render the actual `docs.n8n.io/code/builtin/` modules-list page
through my tooling (it kept resolving to a 404 through the fetch layer I have
access to), so I cannot literally quote "cheerio is absent from this list" —
but no primary source anywhere describes cheerio as built-in, and multiple
n8n Community Forum threads report `Error: Cannot find module 'cheerio'`
on fresh self-hosted instances (e.g. ["Cannot find module 'cheerio' in Code
Node on Self-Hosted Docker
Instance"](https://community.n8n.io/t/cannot-find-module-cheerio-in-code-node-on-self-hosted-docker-instance/167269),
["Can't use cheerio inside code
node"](https://community.n8n.io/t/cant-use-cheerio-inside-code-node/110955)).
Community forum threads are not a primary source in the strict sense (they're
n8n-run but user-authored), so treat this corroboration as secondary, not
as confirmation on par with the docs page.

**Separately:** n8n ships a distinct, dedicated **HTML Extract node**
(`n8n-nodes-base.htmlExtract`) that *internally* uses cheerio to run CSS
selectors against HTML without needing a Code node or external-module
config at all. That's a legitimate no-config alternative to a Code-node
`require('cheerio')` call if the extraction can be expressed as CSS
selectors rather than as a scripted chain (worth considering for whichever
part of the extraction chain doesn't need custom logic).

### 2. What's needed: `NODE_FUNCTION_ALLOW_EXTERNAL` (+ `NODE_FUNCTION_ALLOW_BUILTIN` if built-ins are also needed)

Confirmed on the primary docs page
[Enable modules in Code node](https://docs.n8n.io/deploy/host-n8n/configure-n8n/basic-configuration/configuration-examples/enable-modules-in-code-node):

- `NODE_FUNCTION_ALLOW_BUILTIN` — comma-separated list of Node.js **built-in**
  modules to allow (`*` for all), e.g. `NODE_FUNCTION_ALLOW_BUILTIN=crypto,fs`.
- `NODE_FUNCTION_ALLOW_EXTERNAL` — comma-separated list of **external** (npm)
  modules to allow, sourced from n8n's own `node_modules` directory, e.g.
  `NODE_FUNCTION_ALLOW_EXTERNAL=moment,lodash,cheerio`.
- External-module support is **disabled entirely** unless
  `NODE_FUNCTION_ALLOW_EXTERNAL` is set — there's no default allowlist.
- These variables only *permit* `require()`-ing a module that's already
  physically present in `node_modules` — **they do not install anything**.
  If the module isn't there, allowlisting it changes nothing (see §3).
- The n8n Cloud version does not support importing modules in the Code
  node at all (per the common-issues page); this only applies to self-hosted.
- Restart: the docs pages I could reach don't state this explicitly, but
  since these are process environment variables read at n8n startup (and,
  post-Task-Runners, at task-runner-launcher startup — see §4), a restart of
  the relevant container/process is necessary for a change to take effect.
  This is an inference from how env-var configuration works elsewhere in
  n8n's docs, not a line I could directly quote.

### 3. Cheerio must physically exist in `node_modules` — it is not pre-installed

The official `n8n-io/n8n` Docker image does not ship `cheerio` pre-installed
for user code to `require()` (it may exist as a *transitive* dependency of
n8n internals, e.g. for the HTML Extract node, but modules used internally by
n8n's own nodes are not guaranteed to be `require()`-able by user Code-node
scripts, and multiple self-hosted users hit "Cannot find module 'cheerio'"
even after setting `NODE_FUNCTION_ALLOW_EXTERNAL=cheerio`). The documented
fix, per the common-issues page and n8n's own guidance, is to **extend the
official image with a custom Dockerfile** that `npm install`s the package
into the image, then set the allow-list env var on top. I was not able to
pull a literal, byte-exact Dockerfile snippet from a primary n8n docs page
for the main (non-task-runner) image; the shape is the standard
"`FROM n8nio/n8n:<tag>` + `npm install cheerio` into the appropriate
`node_modules`" pattern described in n8n's own common-issues page and
mirrored in n8n's community-maintained "Automate NPM package installation"
workflow template.

### 4. Sandboxing model: task runners are now the default (n8n 2.0), moving Code-node execution into an isolated process; the JS sandbox mechanism inside that process is still a VM-based sandbox in the vm2 lineage

- Historically, the Code node ran on **`@n8n/vm2`**, n8n's own maintained fork
  of the (now-unmaintained, CVE-riddled) `vm2` library, executing inline in
  n8n's main process by default. n8n's GitHub Security Advisory
  [GHSA-c9c6-rq46-h25v — "Sandbox Escape in JavaScript Code Node via
  Prototype Pollution"](https://github.com/n8n-io/n8n/security/advisories/GHSA-c9c6-rq46-h25v)
  (CVSS 6.0, Moderate) describes the Code node's "VM sandbox" not freezing
  `Function.prototype`, letting an authenticated workflow author pollute it
  and recover a reference to the host `globalThis`. Fixed in **n8n
  1.123.69, 2.33.4, and 2.34.1** — i.e. this VM-sandbox-escape class of bug
  was still being found and patched *after* the 2.0 "hardening" release, which
  confirms the underlying JS sandbox mechanism (a vm2-derived sandbox) is
  still in use post-2.0; what changed in 2.0 is the process boundary around it.
- n8n's official changelog page
  [v2.0 breaking changes](https://docs.n8n.io/changelog/v20-breaking-changes)
  confirms: **"n8n will enable task runners by default... All Code node
  executions will run on task runners"** in secure mode. A consequence
  called out explicitly: `$evaluateExpression()` no longer works inside the
  Code node, because secure mode disables the string-to-code evaluation
  paths expressions depend on. The same release replaces the Pyodide-based
  (in-process, WASM) Python Code node with a task-runner-based native Python
  implementation — dropping convenience variables like `_input` and dot-access
  notation that Pyodide provided.
- Separately, [CVE-2025-68668](https://thecyberexpress.com/n8n-vulnerability-cve-2025-68668/)
  (CVSS 9.9, Critical) affected the **Python** Code node specifically
  (Pyodide sandbox bypass allowing arbitrary host command execution),
  affecting n8n 1.0.0–1.99.x, fixed by the 2.0 task-runner-based native
  Python implementation. Interim workarounds for pre-2.0 instances:
  `N8N_PYTHON_ENABLED=false` (since v1.104.0) or enabling the task-runner
  sandbox via `N8N_RUNNERS_ENABLED` + `N8N_NATIVE_PYTHON_RUNNER` (since
  v1.111.0). This CVE is **not** about vm2/JavaScript — worth flagging
  because it's easy to conflate with the JS sandbox story, but it's the
  reason "task runners by default" became urgent enough to ship as the
  headline of 2.0.
- **Net model for current self-hosted n8n (2.0+):** JS Code-node scripts run
  inside a separate task-runner process (not n8n's main process), and inside
  that process the script executes in a VM sandbox descended from `@n8n/vm2`.
  This is a defense-in-depth improvement (a sandbox escape no longer directly
  compromises the main n8n process/credentials store), not a wholesale
  replacement of vm2 with Node's built-in `vm` module or an isolate-based
  engine like `isolated-vm`. I could not find a primary n8n source stating
  they've dropped vm2-family sandboxing in favor of plain Node `vm` or
  `isolated-vm` — if that migration exists, I didn't find primary
  documentation of it before this research ran out of budget.
- **Task runners relocate the allow-list env vars.** Per the
  enable-modules-in-code-node docs page: *"If n8n instance is setup with Task
  Runners, add the environment variables to the Task Runners instead to the
  main n8n node."* i.e. once Task Runners are active (default in 2.0+),
  `NODE_FUNCTION_ALLOW_EXTERNAL` / `NODE_FUNCTION_ALLOW_BUILTIN` need to be
  set on the **task-runner container/process**, not on the main n8n
  container — a detail that will bite anyone who sets the var on the n8n
  container out of habit and finds cheerio still unavailable. I found
  secondary (non-fully-verified) detail suggesting the runner reads these
  from a launcher config (something like `/etc/n8n-task-runners.json`
  mounted into the runners image) rather than plain container env — I was
  not able to fully verify the exact file path/schema against a primary
  source I could render, so treat this specific mechanism as **unconfirmed,
  needs homelab-side verification** (see Open Questions).

### 5. Cheerio compatibility inside the sandbox

No primary n8n source documents a specific incompatibility list for the
Code-node sandbox (e.g. "packages with native bindings don't work"), beyond
the general expectation that vm2/task-runner sandboxing restricts things like
raw filesystem/process access unless explicitly allowlisted. Cheerio and its
core transitive dependencies (`parse5`, `htmlparser2`, `domhandler`,
`dom-serializer`) are pure JavaScript with no native (N-API/FFI) bindings, so
there's no structural reason they'd be blocked by a VM-based sandbox the way
a package requiring native compilation would be. I found no n8n GitHub issue,
advisory, or forum thread reporting cheerio *specifically* breaking under
either the pre-2.0 inline vm2 sandbox or the 2.0+ task-runner sandbox once
it's actually allowlisted and present in `node_modules` — the failure mode
users report is uniformly "module not found" (missing install /
missing allowlist), not a sandbox-compatibility failure once cheerio is
correctly installed and allowed.

## What I could confirm from primary sources vs. what remains secondary/unconfirmed

**Confirmed from primary sources (docs.n8n.io or github.com/n8n-io):**
- `NODE_FUNCTION_ALLOW_BUILTIN` / `NODE_FUNCTION_ALLOW_EXTERNAL` syntax and
  purpose, and that external modules must already exist in `node_modules`.
- That Task Runners require these vars to be set on the runner, not the main
  n8n process, once Task Runners are in use.
- That n8n 2.0 makes Task Runners the default for all Code-node execution
  (JS and Python), in a "secure mode" that also disables
  `$evaluateExpression()`.
- The existence, CVSS, and fixed versions of GHSA-c9c6-rq46-h25v (JS sandbox
  Function.prototype pollution) and CVE-2025-68668 (Python Pyodide sandbox
  bypass, fixed by the 2.0 native-Python task-runner implementation).
- That n8n Cloud does not support Code-node module imports at all (this only
  matters for self-hosted, which is the homelab case here).

**Secondary / inferred / not fully verified:**
- Whether a container restart is strictly required after setting the env
  vars (highly likely given how env vars work, but not a line I could quote
  from a docs page).
- The exact Dockerfile/`npm install` incantation and the exact
  `/etc/n8n-task-runners.json`-style launcher config path for adding cheerio
  to the task-runner image — the shape is right (custom image extending
  `n8nio/n8n` or `n8nio/runners`, `npm`/`pnpm install cheerio`, then
  allowlisting) but I could not pin the exact current syntax against a page
  my fetch tooling could render cleanly; several `docs.n8n.io` pages
  (`hosting/configuration/task-runners/`, `hosting/securing/hardening-task-runners/`,
  `2-0-breaking-changes/`) 404'd through my tooling despite appearing in
  search results, which is likely a docs-site restructuring/redirect issue
  on n8n's side rather than the pages not existing.
- Whether the current JS sandbox is still `@n8n/vm2`-descended or has since
  moved to Node's built-in `vm` module or `isolated-vm` — the evidence
  (a vm2-lineage "VM sandbox" bug fixed in versions well after 2.0's release)
  points to vm2-family still being the mechanism, but I found no primary
  source stating this in so many words as a design decision.
- The exact n8n 2.0 release date — sources disagree/are ambiguous (one
  secondary source said December 2024, another's URL slug suggests December
  2025); not load-bearing for this research but flagged so nobody cites a
  wrong date downstream.

## Open Questions / What Remains Unconfirmed (homelab-specific TODOs)

None of the above can be turned into a concrete instruction for issue #114
until the actual homelab instance is inspected — this repo does not define
the n8n version or config; it lives in the separate "homelab-edge" repo or
only on the live instance. Before implementing the cheerio extraction chain:

1. **Confirm the running n8n version** — `docker exec <n8n-container>
   n8n --version` (or check the image tag in homelab-edge's compose file).
   This determines whether Task Runners are already mandatory (2.0+) or
   optional (pre-2.0), which changes *where* the allow-list env vars need to
   live.
2. **Check whether Task Runners are enabled** and, if so, whether they run as
   a separate container/process from `n8n` itself — inspect `homelab-edge`'s
   docker-compose for a `n8nio/runners` (or similarly named) service, and
   check `N8N_RUNNERS_ENABLED` / related vars on both the main and runner
   containers.
3. **Check whether `NODE_FUNCTION_ALLOW_EXTERNAL` is already set** anywhere
   (main container and/or runner container/launcher config) and whether
   `cheerio` is already in the list.
4. **Check whether cheerio is already present in `node_modules`** for
   whichever process actually executes Code-node scripts — e.g.
   `docker exec <container> node -e "require.resolve('cheerio')"` against
   the correct container (main vs. runner). If it throws, cheerio isn't
   installed there, however the allow-list env var is set.
5. **Check `homelab-edge`'s Dockerfile(s)** (if any) for whether the n8n
   and/or runner image is already custom-built with extra npm packages, vs.
   pulling the stock `n8nio/n8n` / `n8nio/runners` images unmodified —
   this determines whether adding cheerio is "edit an existing custom
   Dockerfile" or "author a new one for the first time."
6. Decide, once the above is known, whether to install cheerio directly, or
   instead lean on n8n's built-in **HTML Extract node** for the parts of the
   extraction that are expressible as CSS selectors, avoiding the
   custom-image requirement entirely for at least part of the chain.

## Recommendation

**No, cheerio is not available in the Code node out of the box on self-hosted
n8n.** To use it:

1. Build (or extend an existing) custom Docker image on top of the n8n
   image actually in use — `npm install cheerio` (and its transitive deps
   come along automatically) into the image that will actually execute
   Code-node scripts. Under n8n 2.0+ (Task Runners default-on), that's the
   **task-runner image**, not the main `n8n` image — the two are different
   containers/processes, and cheerio needs to land in the one that runs the
   Code node's JS.
2. Set `NODE_FUNCTION_ALLOW_EXTERNAL=cheerio` (comma-append if other external
   modules are also needed) on that same process — the task-runner
   container/launcher config once Task Runners are in play, or the main n8n
   container if still on a pre-2.0 instance without Task Runners.
3. Restart the affected container(s) so the env var and the newly-baked
   image take effect.
4. Pure-JS packages like cheerio, parse5, htmlparser2, domhandler, and
   dom-serializer have no known sandbox-compatibility issues once correctly
   installed and allowlisted — the failure mode everyone hits in practice is
   "module not found," not a runtime sandbox restriction.
5. Before writing any homelab-edge Dockerfile changes, complete the
   verification TODOs above against the actual live instance — this
   research file describes the general n8n mechanism, not this project's
   specific deployment.
