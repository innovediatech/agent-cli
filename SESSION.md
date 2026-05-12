# Session Handoff — 2026-05-11T22:30-04:00

**Project:** agent-cli (foundation lib) + platform-cli (first consumer) + innovedia-platform/mcp-server (live fix)
**Branch:** none — agent-cli and platform-cli are pre-git
**Last commit:** N/A (no repos yet); innovedia-platform sibling repo is at `eed5144` on `main` (unrelated prior work)
**Deployed tag:** N/A (libraries only); platform MCP config updated in `~/.claude.json` — takes effect on next Claude Code restart
**Session length:** ~3-4 hours across three logical phases

---

## 1. Just finished

- **Printing Press evaluation phase** — installed, isolated in Docker, generated petstore (74 files, 67s) and Stripe (782 files, 765 commands, 112s). Findings at `~/pp-sandbox/FINDINGS.md`. Tool kept around for future no-API-service work.
- **agent-cli v0 + v0.1a shipped** at `/projects/libraries/agent-cli/`. Six packages, 1700+ LOC, all tests pass under `-race`, `govulncheck` clean. Packages: `agent`, `exitcode`, `envelope`, `selectfields`, `output`, `introspect`. Reference CLI at `examples/echo-cli/`.
- **platform-cli v0.1.0 shipped** at `/projects/libraries/platform-cli/`. Real consumer of agent-cli wrapping the same endpoints the platform MCP wraps (deals/contacts/companies/documents). Live-tested against the platform API.
- **Platform MCP rot fixed** — 4 independent issues that left the MCP silently dead despite "✓ Connected" status. ClusterIP-auto-discovery wrapper script at `packages/mcp-server/bin/run-platform-mcp.sh`, dead strategy tools removed from `src/index.ts`, `~/.claude.json` updated. Verified end-to-end via direct stdio JSON-RPC.
- **Comparison harness landed real numbers** at `/projects/libraries/platform-cli/FINDINGS.md`. Headline: curated MCP markdown wins per-call (2,991 tokens vs CLI 4,673 on same data) but CLI's `agent-context` is 3.6× lighter than MCP boot cost (1,155 vs 4,121 tokens).

## 2. Current state (verified)

⚠️ **DIRTY TREE on innovedia-platform repo** (kept intentionally; awaiting commit decision):
```
 M packages/mcp-server/src/index.ts          (strategy tool registrations removed)
?? packages/mcp-server/bin/                  (new wrapper script directory)
```

**Verified working right now:**
- `cd /projects/libraries/agent-cli && /home/innovedia-admin/.local/go/bin/go test -race ./...` → all packages pass
- `cd /projects/libraries/agent-cli && /home/innovedia-admin/.local/go/bin/go vet ./...` → clean
- `cd /projects/libraries/agent-cli && /home/innovedia-admin/go/bin/govulncheck ./...` → "No vulnerabilities found"
- `make verify` (in agent-cli) → smoke tests on echo-cli pass: greet+select, csv envelope unwrap, exit code 4
- `/tmp/echo-cli agent-context | wc -c` → 2,156 bytes (~539 tokens)
- `/tmp/platform-cli agent-context | wc -c` → 4,619 bytes (~1,155 tokens)
- Direct MCP stdio test: `tools/list` returns 6 tools (search_documents, get_document, list_folders, list_deals, get_deal, search_pipeline); `search_pipeline(contacts)` returns 193 real contacts as 12,853-byte markdown table
- Platform API auth: admin creds work via ClusterIP `http://172.43.165.122/api`

**NOT yet verified (requires Claude Code restart):**
- The new platform MCP wrapper actually runs when invoked through Claude Code as opposed to direct stdio. Implementation is identical so very high confidence, but a real session test is the proof.

**Backups:**
- `~/.claude.json.bak-2026-05-11-platform-mcp-fix` — original platform MCP config in case the new wrapper has any surprise

## 3. Unfinished / in-flight

- [ ] **agent-cli v0.1b: `httpclient` package** — shared retry/backoff/error-classification helper. Defer until a second CLI is built so the shape is informed by two consumers, not one. Pending, not blocked.
- [ ] **agent-cli v0.1b: `mirror` package** — SQLite + FTS5 + cursor sync. The architecturally heaviest piece and where the compound-query / offline / FTS5-search wins live. Pending, the next big focused session.
- [ ] **Three triage follow-ups** (see Open questions §7).
- [ ] **Phase 3 — second-CLI comparison** (Sentry/GlitchTip head-to-head, or GitHub `gh` head-to-head). Pending until mirror lands.

## 4. Known issues

- **`strategy` commands in platform-cli still mirror deleted routes.** `platform-cli strategy {context,decisions,sessions} list` 404s upstream. Leaving them in until the methodology dossier widgets are built (per `cleanup/delete-strategy-os`). Cosmetic.
- **Admin credentials in `~/.claude.json` for the platform MCP.** A regression vs the original service-account design. Working-as-intended for today; flagged for proper fix.
- **Platform-cli `default` mode used to emit indented JSON when piped (36k tokens).** FIXED in agent-cli v0.1a — default is now compact when stdout is not a terminal; `--pretty` to force indent.
- **The MCP server's `client.ts` and `tools/strategy-*.ts` still exist** even though the registrations are gone from `index.ts`. Dead code, harmless, kept for symmetry. Worth a cleanup PR.

## 5. Hot files

- `/projects/libraries/agent-cli/output/output.go` — indent-on-pipe fix in `Write()`; new `--pretty` path; render() signature now takes indent bool
- `/projects/libraries/agent-cli/introspect/introspect.go` — NEW; emits Cobra-tree JSON for `agent-context` subcommand
- `/projects/libraries/agent-cli/agent/agent.go` — added `Pretty` field + `--pretty` flag binding
- `/projects/libraries/platform-cli/cmd/platform-cli/main.go` — full Cobra surface; wires `introspect.EmitCommand(root, "0.1.0")`
- `/projects/libraries/platform-cli/internal/authclient/authclient.go` — JWT login + token cache (mode 0600 at `~/.config/innovedia-platform-cli/token.json`) + reactive 401 retry
- `/projects/apps/innovedia-platform/packages/mcp-server/src/index.ts` — strategy tool registrations removed; comment block explains why
- `/projects/apps/innovedia-platform/packages/mcp-server/bin/run-platform-mcp.sh` — NEW wrapper that resolves the platform-api ClusterIP via kubectl and execs tsx

## 6. Next concrete step

**Build agent-cli v0.1b `mirror` package** at `/projects/libraries/agent-cli/mirror/`. This is the compound-query / FTS5 / offline-query piece — the asymmetric win that the Printing Press measurement actually points to (and that v0.1a hasn't measured yet).

Open `PLAN.md` §"Out of scope for v0" for the mirror notes, then design:
- `mirror.New(dbPath)` opens/creates a SQLite DB with FTS5 enabled, sets pragmas
- `mirror.RegisterResource(name, schemaSQL, upsertSQL)` declares a resource family
- `mirror.SyncCursor(name)` reads/writes per-resource cursor checkpoint
- `mirror.UpsertBatch(name, rows)` transactional batched upsert (the platform-cli + petstore-pp-cli code shows the shape)
- `mirror.Search(name, ftsQuery)` FTS5 query helper
- Test against an in-memory sqlite

Reference implementations to copy patterns from (NOT verbatim — Apache 2.0 attribution if so):
- `~/pp-sandbox/pp-home/library/petstore/internal/store/store.go` (1,370 LOC)
- `~/pp-sandbox/pp-home/library/petstore/internal/cli/sync.go` (1,022 LOC)

Once mirror lands, wire it into `platform-cli` (`platform-cli contacts sync`, `contacts search "q"`) and re-run the comparison harness. The expected headline: compound queries the MCP can't do (cross-resource JOINs, FTS5 search across documents+notes+contacts) become token-cheap.

## 7. Open questions for Jayson

1. **Commit the platform MCP changes?** The dirty tree on `innovedia-platform` (`packages/mcp-server/src/index.ts` + `packages/mcp-server/bin/run-platform-mcp.sh`) needs a commit + push + maybe a redeploy if the MCP server's deployment image is from source. *Options:* (a) commit + push, (b) leave dirty for you to review, (c) revert and re-apply via a feature branch. I left it dirty by default per Git Safety Protocol.

2. **Provision `mcp@innovedia.internal` service account?** Currently the platform MCP is running with admin (`jjoseph@innovedia.ai`) credentials in `~/.claude.json`. *Options:* (a) reset the existing user's password via DB or `/users` API, (b) leave on admin and accept the security regression, (c) create a dedicated service account with a narrower role.

3. **Add the `httpclient` package before or after `mirror`?** *Reasoning to add first:* helps the next CLI consumer have a retry/backoff story out of the box. *Reasoning to defer:* mirror is the asymmetric win; httpclient is finishing-touches. My recommendation is **mirror first**, then httpclient extracted from whatever the second consumer needs.

4. **Initialize git repos for agent-cli and platform-cli?** They're pre-git right now. Worth doing before the next session so commits become a thing. *Options:* (a) two separate repos under `innovediatech/`, (b) keep them as folders, (c) put both in a monorepo at `/projects/libraries/agent-tools/`.

## Cross-references

- **agent-cli FINDINGS / experiment writeup:** `~/pp-sandbox/FINDINGS.md` (Printing Press evaluation)
- **platform-cli comparison harness:** `/projects/libraries/platform-cli/FINDINGS.md`
- **innovedia-platform repo's last session handoff:** `/projects/apps/innovedia-platform/SESSION.md` (2026-05-09, unrelated Slice 1a Operations Layer ship — not overwritten)
