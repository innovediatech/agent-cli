# Phase 3 — `glitchtip-cli` as second-domain validation

**Status:** Plan written 2026-05-12. Not started.
**Scope:** Build a thin, real CLI on top of agent-cli against the GlitchTip API. Validates that the agent-native patterns translate to a different domain (error tracking) than the platform CRM/pipeline shape that platform-cli already covers.

---

## Why GlitchTip

Two factors make GlitchTip the right Phase 3 target over GitHub `gh`:

1. **We run it.** `errors.innovedia.ai` is the org's error tracker; we have a real workflow (issue triage during incidents) that today is curl-or-web-UI. A working `glitchtip-cli` becomes useful tooling, not just an evaluation artifact.
2. **The right shape for evaluation.** Error tracking exercises every agent-cli package naturally — list/get/events/update map cleanly to projection, pagination, mirror, and (modest) writes. `gh` has a vastly larger surface that would dilute the test.

**No head-to-head against an existing CLI.** `sentry-cli` exists but is rust-based and upload-focused (sourcemaps, releases) — it solves a different problem. The actual comparison is **"agent + curl/jq today"** vs. **"agent + glitchtip-cli"**. That framing is honest: we're shipping the missing tool, not displacing one.

---

## Target API surface

GlitchTip's API is Sentry-API-compatible. Probed 2026-05-12 against `errors.innovedia.ai`. Auth is `Authorization: Bearer <token>` (token in `pass innovedia/glitchtip/api-token`).

**Read endpoints (v1 scope):**

| Endpoint | Purpose | Notes |
|---|---|---|
| `GET /api/0/organizations/` | List orgs | Trivial; mostly returns `innovedia` for us |
| `GET /api/0/organizations/{slug}/projects/` | List projects | Returns ~28 in our org (some duplicate-named) |
| `GET /api/0/organizations/{slug}/issues/` | Cross-project issue list | Supports `?query=`, `?limit=`, `?statsPeriod=` |
| `GET /api/0/projects/{org}/{project}/issues/` | Issues scoped to one project | Same shape as cross-org list |
| `GET /api/0/issues/{id}/` | Issue detail | Fuller metadata, status history |
| `GET /api/0/issues/{id}/events/` | Events for an issue | The actual error instances |
| `GET /api/0/events/{event_id}/` | Single event w/ stacktrace | Most useful per-token: full stack + breadcrumbs |

**Write endpoints (v2 scope, opt-in):**

| Endpoint | Purpose | Notes |
|---|---|---|
| `PUT /api/0/issues/{id}/` | Update status/assignee | `{status: "resolved"|"unresolved"|"ignored"}` |

**Pagination:** Sentry-style `Link: <url>; rel="next"; cursor="..."` headers. Need to parse and surface as cursors.

**Sample issue shape (truncated):**
```json
{
  "id": "46",
  "type": "error",
  "level": "error",
  "status": "unresolved",
  "project": {"id": "34", "slug": "omex-web", "name": "OMEX Web"},
  "title": "Error: Minified React error #418; ...",
  "metadata": {"type": "Error", "value": "...", "filename": "...", "function": "rD"},
  "firstSeen": "2026-04-30T01:00:34.580Z",
  "lastSeen": "2026-05-09T00:08:26.240Z",
  "count": "2"
}
```

Note the `project.slug` nested shape — good test for `selectfields`' dotted-path traversal (`--select results.title,results.project.slug,results.lastSeen`).

---

## Which agent-cli packages this validates

Every v0/v0.1 package gets a real-world test:

| Package | What it does for glitchtip-cli | What we learn |
|---|---|---|
| `agent` | `--agent` mega-flag, all sub-flags | Whether the canonical set is enough — or whether error-triage needs anything new (e.g. `--since 24h`?) |
| `exitcode` | 401→Auth, 404→NotFound, 429→RateLimited | Confirm the typed codes carry through Bearer-auth APIs |
| `envelope` | Wrap issue lists with provenance | First test of envelope on a non-Innovedia-internal source |
| `selectfields` | `--select results.id,results.title,results.lastSeen` projects out of ~20 fields/issue | Validates the cost story: agent gets ~50 bytes/issue instead of ~800 |
| `output` | JSON/CSV/plain for triage; `--deliver file:` for batched dumps | Real CSV use case (incident report) |
| `introspect` | `glitchtip-cli agent-context` | First publish of agent-context for a non-trivial command tree |
| `mirror` | SQLite cache of issues; `--data-source local` for offline triage | First real test of mirror against an API with Link-header pagination |
| `httpclient` | Bearer auth, 429-with-Retry-After (GlitchTip can rate-limit), 5xx retry | First time httpclient's request-hook surface drives a static-token API instead of refresh-style JWT |

The two most interesting ones:
- **`mirror` + Link-header pagination.** Sentry's cursor format isn't the same as platform-cli's — we'll learn whether `mirror.Cursor` was over-fit to platform.
- **`httpclient`'s static-Bearer pattern.** Different from `authclient`'s refresh-token shape; if it composes cleanly, the hook surface is sound.

---

## Slice plan

Five slices, each end-to-end (impl + tests + smoke).

### Slice 0 — Repo + skeleton (~20 min)
- `git init` at `/projects/libraries/glitchtip-cli/`
- `go.mod` with `replace github.com/innovediatech/agent-cli => /projects/libraries/agent-cli` (mirror platform-cli's setup)
- `PLAN.md` (mirror this doc, scoped to glitchtip-cli)
- `README.md` stub
- `cmd/glitchtip-cli/main.go` with root cobra command + `agent.Bind` + `introspect.EmitCommand`
- GitHub repo at `innovediatech/glitchtip-cli` (private)
- First commit + push
- **Exit criterion:** `glitchtip-cli agent-context` runs and emits valid JSON

### Slice 1 — `auth` + `httpclient` + first read (~30 min)
- `internal/apiclient/apiclient.go` — thin wrapper over `httpclient.Client` with a `RequestHook` that injects Bearer from `GLITCHTIP_TOKEN` env (no token cache — static API tokens don't need it)
- `internal/client/client.go` — typed read surface for `ListOrgs`, `ListProjects`
- `glitchtip-cli orgs list` + `glitchtip-cli projects list --org <slug>`
- Live-verified against `errors.innovedia.ai`
- **Exit criterion:** `glitchtip-cli projects list --org innovedia --agent --select results.slug,results.name | head -3` returns clean JSON

### Slice 2 — issues read + projection (~30 min)
- `ListIssues(ctx, params)` — cross-org and per-project variants
- `GetIssue(ctx, id)` — single issue detail
- `ListEvents(ctx, issueID, params)` — events for an issue
- `GetEvent(ctx, eventID)` — single event with stacktrace
- Pagination: read `Link` header, expose `--cursor` flag for next-page; emit cursor in `meta.next_cursor`
- **Exit criterion:** `glitchtip-cli issues list --status unresolved --select results.title,results.project.slug,results.lastSeen` returns projected JSON; `--cursor <c>` continues from the right page

### Slice 3 — mirror integration (~45 min)
- `glitchtip-cli sync` — bulk-pull issues to local SQLite via `mirror` package, FTS5 on `title + metadata.value`
- `glitchtip-cli issues list --data-source local` — read from mirror; `meta.source: "local"` and `meta.synced_at: <ts>`
- `glitchtip-cli issues search "<query>" --data-source local` — FTS5 search
- **Exit criterion:** sync 100+ issues, search hits without hitting the network, `--data-source auto` falls back to local when network is offline

### Slice 4 — FINDINGS writeup (~30 min)
- `FINDINGS.md` at root of glitchtip-cli following the platform-cli/FINDINGS.md template
- Sections: token-spend comparison (curl+jq baseline vs `--select`), what worked, what was missing, what we'd change in agent-cli
- Update agent-cli's main README to link Phase 3 results

### Slice 5 (optional) — writes (~20 min)
- `glitchtip-cli issues resolve <id>` / `... ignore <id>` / `... unresolve <id>` — PUT /issues/{id}/
- Only if v1 reads feel solid

---

## Success criteria

The Phase 3 build is successful when:

1. **All 8 agent-cli packages are exercised** by glitchtip-cli, with no package needing extension to support its use case. Any gap is a finding worth recording.
2. **Token spend story is concrete.** FINDINGS.md shows a real measurement: "agent triaging the last 24h of issues used ~X tokens with `--select`, vs ~Y tokens with raw curl/jq."
3. **`mirror` works with Link-header pagination.** This is the most likely source of friction — mirror was built against the platform-cli paged shape, which is different.
4. **Live-verified end-to-end.** Final smoke pulls real OMEX/Innovedia issues into SQLite, searches, and projects.

Non-goals:
- Beating `sentry-cli` (different problem).
- Covering every Sentry-compat endpoint (read surface + maybe resolve is enough).
- Webhook ingestion (separate concern; webhooks already write to GlitchTip directly).

---

## Where it lives

- **Repo:** `/projects/libraries/glitchtip-cli/` (sibling of `agent-cli` + `platform-cli`)
- **GitHub:** `innovediatech/glitchtip-cli` (private, same org pattern)
- **Replace directive:** `replace github.com/innovediatech/agent-cli => /projects/libraries/agent-cli` for local-dev cycle (matches platform-cli)
- **Secrets:** `pass innovedia/glitchtip/api-token` — already exists, value confirmed working 2026-05-12

---

## Open questions to resolve before Slice 0

None blocking. Worth thinking through during Slice 0:

1. **Should the CLI default org be configurable?** Most invocations target `innovedia`; a config file or `GLITCHTIP_ORG` env var saves typing. Decision: env var first, config file as v2.
2. **What's the right "agent-friendly" subset of the issue shape?** Default `--compact` projection should be the agent triage set: `id, title, level, status, project.slug, lastSeen, count`. Decide during Slice 2.
3. **How do we handle the project-name duplication?** The org has 8 `aion-ci-api-*` projects from a deployment-tag bug. Not glitchtip-cli's problem to fix, but worth surfacing.

---

## Carry-over for next session

When picking this up:

1. Read this doc end-to-end.
2. `git status` in `/projects/libraries/` to confirm `glitchtip-cli/` doesn't exist yet.
3. Start at Slice 0 — repo skeleton.
4. The token is in `pass innovedia/glitchtip/api-token`; smoke against `https://errors.innovedia.ai`.
5. Use `git -C <path>` or self-contained `cd <path> && ...` for any parallel-Bash work touching multiple repos. (See `feedback_bash_parallel_cwd.md`.)
