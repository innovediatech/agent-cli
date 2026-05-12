# Session Handoff — 2026-05-12T00:00-04:00

**Project:** agent-cli + platform-cli (foundation lib + first consumer)
**Branch:** both on `main`
**Last commit (agent-cli):** `7ff4167` — feat(mirror): add SQLite-backed local cache with FTS5
**Last commit (platform-cli):** `0f141c2` — feat: wire agent-cli mirror into contacts/deals/companies
**Deployed tag:** N/A — both are libraries; nothing deployed
**Session length:** ~2 hours, picked up from 2026-05-11T22:30 handoff

---

## 1. Just finished

- **Both repos initialized.** agent-cli + platform-cli now on `main` as separate local repos with `.gitignore` and an initial-import commit each. Per-repo decision (option a from prior open question §4). No GitHub remotes yet.
- **agent-cli `mirror` package shipped (v0.1b)** — `7ff4167`. Resource-agnostic SQLite cache + FTS5 + cursors + caller-declared typed columns + `DB()` escape hatch. Pure-Go `modernc.org/sqlite` (no CGO). 19 tests pass under `-race`; `go vet` clean; `govulncheck` clean; `make verify` smoke gate green.
- **platform-cli wired to mirror (v0.1.1)** — `0f141c2`. Added `internal/mirrorstore` + per-resource `{contacts,deals,companies} {sync,search,list-local}` + top-level `mirror status / clear`. Smoke tested live against platform API on himothy: 193 contacts synced in 210ms, FTS5 search 17ms, compound query 7ms.
- **Comparison harness re-run with new numbers** at `/projects/libraries/platform-cli/FINDINGS.md` Phase 2b. Headline: **mirror search 54× lighter than MCP** (55 vs 2,991 tokens), **compound DB query 68× lighter** AND not expressible in MCP at all.

## 2. Current state (verified)

⚠️ **DIRTY TREE on innovedia-platform repo** (carried over from prior handoff, untouched this session):
```
 M packages/mcp-server/src/index.ts          (strategy tool registrations removed)
?? packages/mcp-server/bin/                  (new wrapper script directory)
```

**Verified working right now:**
- `cd /projects/libraries/agent-cli && /home/innovedia-admin/.local/go/bin/go test -race ./...` → 7 packages pass (agent, envelope, exitcode, introspect, mirror, output, selectfields)
- `cd /projects/libraries/agent-cli && /home/innovedia-admin/.local/go/bin/go vet ./...` → clean
- `cd /projects/libraries/agent-cli && /home/innovedia-admin/go/bin/govulncheck ./...` → "No vulnerabilities found"
- `make verify` (in agent-cli) → echo-cli smoke tests + greet/select/csv/exit-4 all pass
- `cd /projects/libraries/platform-cli && /home/innovedia-admin/.local/go/bin/go build ./...` → clean
- Live API smoke test (PLATFORM_API_URL=http://172.43.165.122/api):
  - `platform-cli contacts sync --agent` → `{"fetched":193,"stored":193,"skipped":0}` in 210ms
  - `platform-cli contacts search "jose" --agent --select results.id,results.email` → 4 hits, 17ms
  - `sqlite3 /tmp/pcli-mirror.db` compound query (FTS5 + typed-column filter) → 2 hits, 7ms

**Repo state:**
- agent-cli: 2 commits on `main`, working tree clean
- platform-cli: 2 commits on `main`, working tree clean
- innovedia-platform: still at `eed5144` with the same dirty paths from prior session (NOT touched this session)

**Backups:**
- `~/.claude.json.bak-2026-05-11-platform-mcp-fix` — original platform MCP config (still present)

## 3. Unfinished / in-flight

- [ ] **Push both repos to GitHub** — pending Jayson's call on visibility (public vs private) and whether to use `innovediatech/` org. Local repos are ready; nothing else gates this.
- [ ] **agent-cli `httpclient` package** — shared retry/backoff/error-classification. Now genuinely informed by two consumer shapes (echo-cli + platform-cli), so the API design has the data it needs. Pending, not blocked.
- [ ] **Phase 3 — second-CLI comparison** (Sentry/GlitchTip head-to-head, or GitHub `gh` head-to-head) — pending, not blocked.
- [ ] **The two lingering open questions from prior session** still open (see §7).

## 4. Known issues

- **Sync is full-refresh per resource.** No incremental cursor; `cursor` column stores the wall-clock RFC3339 of last sync, used only for "synced N seconds ago" UX. Fine at 193 rows; needs revisit when the platform API exposes pagination.
- **Empty results for deals/companies on the live himothy DB.** Sync correctly returned 0/0 for both — that's the actual data state, not a bug. Worth re-verifying once those tables get populated.
- **Mirror search hit count includes the FTS5 default tokenizer's stemming.** "jose" matches "jose.collado@gmail.com" and "Jose Sr." consistently; expected behavior, but worth knowing if a query feels too loose.
- **`platform-cli contacts list --agent --select results.first_name`** doesn't project anything (the API returns camelCase `firstName`, not `first_name`). Cosmetic; mirror's typed columns *do* fall back camelCase correctly via `lookupFieldValue`, but the `--select` projector is verbatim-keyed. Worth a follow-up in agent-cli's `selectfields` package.

## 5. Hot files

- `/projects/libraries/agent-cli/mirror/mirror.go` — NEW; whole package. Generic `resources` table + per-resource `rt_<name>` typed tables + FTS5 + cursors. ~520 lines.
- `/projects/libraries/agent-cli/mirror/mirror_test.go` — NEW; 19 cases covering open/register/upsert/get/list/search/cursor/persist-across-reopen/concurrent-writes.
- `/projects/libraries/platform-cli/internal/mirrorstore/mirrorstore.go` — NEW; opens mirror at `~/.config/innovedia-platform-cli/mirror.db` (override `PLATFORM_MIRROR=…`), registers contacts/deals/companies with typed columns.
- `/projects/libraries/platform-cli/cmd/platform-cli/mirror_cmd.go` — NEW; sync/search/list-local + top-level `mirror` Cobra subcommands.
- `/projects/libraries/platform-cli/cmd/platform-cli/main.go` — wired `addMirrorSubcommands` into the three resource commands and added `newMirrorCmd` to the root.
- `/projects/libraries/platform-cli/compare.sh` — Phase 2b search-comparison block appended.
- `/projects/libraries/platform-cli/FINDINGS.md` — Phase 2b section: 54×/68× headline, mirror semantics, when-it-earns-its-keep section.
- `/projects/libraries/agent-cli/PLAN.md` — status stamp updated to v0.1b; mirror + introspect crossed off "Out of scope for v0".

## 6. Next concrete step

**Either** (a) push both repos to GitHub, **or** (b) start agent-cli `httpclient`. Recommendation: **(a) GitHub push first** — local-only repos have no off-host backup and the mirror work is the highest-density hour from this week. After that, `httpclient` is the natural next slice now that platform-cli's auth-retry pattern is the second data point informing the shape.

If pushing to GitHub:
```bash
# Decide visibility (likely private — these aren't ready to publish yet),
# then for each repo:
cd /projects/libraries/agent-cli
gh repo create innovediatech/agent-cli --private --source=. --remote=origin --push
cd /projects/libraries/platform-cli
gh repo create innovediatech/platform-cli --private --source=. --remote=origin --push
```

If starting httpclient:
- Open `/projects/libraries/agent-cli/PLAN.md` and re-read Design Principles §3.
- Look at `/projects/libraries/platform-cli/internal/authclient/authclient.go` for the live retry-on-401 pattern.
- Look at `~/pp-sandbox/pp-home/library/petstore/internal/api/` for Printing Press's retry shape.
- Design surface: `httpclient.New(opts)`; opts include retry policy, backoff, classifier, header injection. Should compose with `exitcode.Classify` so failed retries surface typed exit codes.

## 7. Open questions for Jayson

1. **Push to GitHub now?** Visibility = public vs private? Org = `innovediatech/`? — neither repo has an off-host backup right now, which is mild risk. *My recommendation:* private, `innovediatech/`, push tonight.
2. **Commit the dirty platform MCP changes?** (Carried over from prior handoff.) `packages/mcp-server/src/index.ts` + `packages/mcp-server/bin/` still uncommitted on innovedia-platform. Needs commit + push + redeploy if the MCP server's deployment image is from source. *Options:* (a) commit + push, (b) leave dirty, (c) feature branch. Left dirty by default.
3. **Provision `mcp@innovedia.internal` service account?** (Carried over.) Platform MCP still on admin (`jjoseph@innovedia.ai`) creds in `~/.claude.json`. *Options:* (a) reset existing user via DB or `/users` API, (b) leave on admin, (c) create a dedicated service account.
4. **Should `--select` learn the snake_case ↔ camelCase fallback** that `mirror.lookupFieldValue` already has? Cosmetic but consistent — the mirror's typed columns work because of it; the `--select` projector doesn't. Trivial to add to `selectfields/selectfields.go`.

## Cross-references

- **agent-cli FINDINGS / experiment writeup:** `~/pp-sandbox/FINDINGS.md` (Printing Press evaluation, Phase 1)
- **platform-cli comparison harness:** `/projects/libraries/platform-cli/FINDINGS.md` (Phase 2 + 2b)
- **innovedia-platform repo's last session handoff:** `/projects/apps/innovedia-platform/SESSION.md` (2026-05-09, Slice 1a Operations Layer ship — unrelated)
