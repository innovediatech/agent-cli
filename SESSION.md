# Session Handoff — 2026-05-12T00:15-04:00

**Project:** agent-cli + platform-cli (foundation lib + first consumer)
**Branch:** both on `main`
**Last commit (agent-cli):** `c410cb2` — feat(selectfields): snake↔camel key fallback in projections
**Last commit (platform-cli):** `686298a` — chore(session): handoff stub pointing at canonical agent-cli/SESSION.md
**Deployed tag:** N/A — both are libraries; nothing deployed
**Remotes:** both pushed to `github.com:innovediatech/{agent-cli,platform-cli}` (private)
**Session length:** ~30 min, picked up cleanly from 2026-05-12T00:00 handoff

---

## 1. Just finished

- **Both repos live on GitHub** — `innovediatech/agent-cli` + `innovediatech/platform-cli`, both private. Rewrote local commit author emails from `info@innovedia.io` → `innovediatech@users.noreply.github.com` via `git rebase --root --exec 'commit --amend --reset-author'` to clear GitHub's email-privacy block. No remote history existed yet, so no force-push risk.
- **Platform MCP server changes committed + pushed** — `2159a1f` on innovedia-platform `main`. Removed `registerStrategy{Context,Decision,Session}Tools` registrations (referenced deleted `/strategy/*` routes from `cleanup/delete-strategy-os`); added `packages/mcp-server/bin/run-platform-mcp.sh` k8s-ClusterIP-resolving wrapper. The `tools/strategy-*` source files were intentionally left in place in case methodology gets rebuilt as Dossier widgets.
- **MCP service account password reset + wired into Claude Code** — `mcp@innovedia.internal` already existed (id=`mcp-service-account`, role=ADMIN, OWNER membership, last login 2026-02-20) but the password had drifted. Reset to `McpService2026!` via bcrypt-direct-DB update (10 rounds, matches `bcrypt.hash(_, 10)` in `auth.service.ts`). Verified login against the API. Updated `~/.claude.json` `mcpServers.platform.env` to use the service account creds instead of admin (`jjoseph@innovedia.ai`). Stored password in `pass innovedia/platform/mcp-service-account`.
- **`selectfields` snake↔camel fallback** — `c410cb2` on agent-cli. `lookupKey` helper tries the verbatim key first, then `snakeToCamel` and `camelToSnake` renderings. Output key honors what the caller asked for. Mirrors `mirror.lookupFieldValue`'s case-tolerance so both projection layers behave the same. 6 new tests pass under `-race`; live-verified against platform API (`--select results.first_name` projects from camelCase `firstName` correctly).

## 2. Current state (verified)

**All three repos clean:**
- agent-cli: `c410cb2` on `main`, working tree clean, pushed to origin
- platform-cli: `686298a` on `main`, working tree clean, pushed to origin
- innovedia-platform: `2159a1f` on `main`, working tree clean, pushed to origin (the carried-over dirt from prior sessions is now committed)

**Verified working right now:**
- `cd /projects/libraries/agent-cli && /home/innovedia-admin/.local/go/bin/go test -race ./... -count=1` → 7 packages pass (agent, envelope, exitcode, introspect, mirror, output, selectfields). Mirror takes 21s; the rest sub-second.
- `cd /projects/libraries/agent-cli && /home/innovedia-admin/.local/go/bin/go vet ./...` → clean
- `cd /projects/libraries/platform-cli && /home/innovedia-admin/.local/go/bin/go build ./...` → clean
- Live API smoke (PLATFORM_API_URL via ClusterIP, PLATFORM_EMAIL=mcp@innovedia.internal):
  - `platform-cli contacts list --agent --select results.first_name,results.email` → projects both fields (`first_name` projected from camelCase `firstName` source via new fallback)
  - Login as `mcp@innovedia.internal` / `McpService2026!` against `http://$CLUSTERIP/api/auth/login` → returns accessToken with role=ADMIN

**Backups:**
- `~/.claude.json.bak-2026-05-11-platform-mcp-fix` — earlier session's backup (still present)
- `~/.claude.json.bak-2026-05-12-mcp-svc-account` — fresh backup before this session's creds swap

## 3. Unfinished / in-flight

- [ ] **Restart Claude Code** to pick up the new `~/.claude.json` MCP env. Next chat will get this automatically.
- [ ] **agent-cli `httpclient` package** — shared retry/backoff/error-classification. Two consumer shapes available (echo-cli + platform-cli's `internal/authclient`); design has the data it needs. Pending, not blocked.
- [ ] **Phase 3 — second-CLI comparison** (Sentry/GlitchTip head-to-head, or GitHub `gh` head-to-head) — pending, not blocked.
- [ ] **Rotate `jjoseph@innovedia.ai` admin password?** Now that MCP no longer needs admin creds in plaintext config, the admin password could be rotated independently. Optional; the credential's exposure surface shrank tonight regardless. *Not* on the immediate path.

## 4. Known issues

- **Both bash-parallel `cd` calls bit me twice this session** — when two parallel Bash tool calls both interact with cwd, the second can inherit the first's cwd or race. Fix: every parallel Bash call should use absolute paths or its own `cd`. Recorded as `feedback_bash_parallel_cwd.md`-worthy if it recurs.
- **Sync is still full-refresh per resource** (carried over) — fine at 193 rows, revisit when platform API exposes pagination.
- **`camelToSnake` for acronym runs is naive** — `URLPath` → `u_r_l_path` not `url_path`. Doc'd as a known limitation; JSON API keys conventionally avoid acronym runs so this hasn't hurt. Worth revisiting only if a real key trips it.

## 5. Hot files

- `/projects/libraries/agent-cli/selectfields/selectfields.go` — added `lookupKey` + `snakeToCamel` + `camelToSnake`; `applyNode` now uses `lookupKey` for map descent. Package docstring updated to describe the case-tolerance.
- `/projects/libraries/agent-cli/selectfields/selectfields_test.go` — 6 new tests: snake spec / camel data, camel spec / snake data, exact-match precedence, nested+array fallback, and direct tests for the two case helpers.
- `/projects/apps/innovedia-platform/packages/mcp-server/src/index.ts` — strategy tool registrations removed (committed in `2159a1f`).
- `/projects/apps/innovedia-platform/packages/mcp-server/bin/run-platform-mcp.sh` — NEW; k8s-ClusterIP-resolving wrapper (committed in `2159a1f`).
- `/home/innovedia-admin/.claude.json` — `mcpServers.platform.env.PLATFORM_EMAIL/PASSWORD` swapped to `mcp@innovedia.internal` / `McpService2026!`. Backup at `~/.claude.json.bak-2026-05-12-mcp-svc-account`.

## 6. Next concrete step

**Restart Claude Code** so the new MCP creds take effect, then start the **agent-cli `httpclient` package**. It's the natural next slice — now informed by two real consumer shapes — and is the last gap before agent-cli covers the day-one CLI primitives.

If starting httpclient:
- Re-read `/projects/libraries/agent-cli/PLAN.md` Design Principles §3.
- `/projects/libraries/platform-cli/internal/authclient/authclient.go` for the live retry-on-401 pattern.
- `~/pp-sandbox/pp-home/library/petstore/internal/api/` for Printing Press's retry shape.
- Surface to design: `httpclient.New(opts)` with retry policy / backoff / classifier / header injection; should compose with `exitcode.Classify` so failed retries surface typed exit codes.

## 7. Open questions for Jayson

1. **Public-ify `agent-cli` later?** It has no Innovedia-specific code (`platform-cli` is the consumer). Reasonable to make public once `httpclient` lands and a README exists. Not now.
2. **Rotate `jjoseph@innovedia.ai` admin password?** Optional, low urgency. Service-account-only access for MCP is the meaningful win; rotating admin is a separate hygiene task.
3. **Three carried-over questions from earlier sessions are resolved** — repos pushed (Q1), platform MCP committed (Q2), service account provisioned (Q3), `--select` fallback shipped (Q4).

## Cross-references

- **agent-cli FINDINGS / experiment writeup:** `~/pp-sandbox/FINDINGS.md` (Printing Press evaluation, Phase 1)
- **platform-cli comparison harness:** `/projects/libraries/platform-cli/FINDINGS.md` (Phase 2 + 2b)
- **innovedia-platform repo's last session handoff:** `/projects/apps/innovedia-platform/SESSION.md` (2026-05-09, Slice 1a Operations Layer ship — unrelated)
