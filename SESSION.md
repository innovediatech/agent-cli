# Session Handoff — 2026-05-12T05:30-04:00

**Project:** agent-cli + platform-cli (foundation lib + first consumer)
**Branch:** both on `main`
**Last commit (agent-cli):** `d4ab32d` — docs(phase-3): plan glitchtip-cli as second-domain validation
**Last commit (platform-cli):** `5334ff4` — refactor(authclient): build on agent-cli/httpclient
**Deployed tag:** N/A — both are libraries; nothing deployed
**Remotes:** both pushed to `github.com:innovediatech/{agent-cli,platform-cli}` (private)
**Session length:** ~2 hours, picked up cleanly from 2026-05-12T00:15 handoff

---

## 1. Just finished

- **`httpclient` package shipped on agent-cli** — `665ae79`. Stateless HTTP transport: retry policy with exponential backoff + Retry-After honoring, pluggable `RequestHook`/`Classifier` for auth and observability, `APIError` that composes with `exitcode.Classify` for typed exit codes without per-CLI mapping. `Limiter` interface for optional rate-limiting (petstore's adaptive limiter satisfies it). `Do` follows net/http semantics (raw response + transport errors only); `DoJSON` is the opinionated wrapper that treats non-2xx as APIError and sanitizes BOM/XSSI guard prefixes. ~440 LOC, 35 tests passing under `-race`.
- **`authclient` refactored onto httpclient** — `5334ff4` on platform-cli. 60+/53− lines; public surface unchanged (`New`/`Config`/`Do`/`Token`). 401-retry semantics preserved via `MaxAttempts=2`; 5xx retry added as a free improvement that the bespoke loop lacked. The auth lifecycle is now a `RequestHook` (token injection via `Token()`) + `Classifier` (401 → invalidate + retry). Live-verified against `errors.innovedia.ai`-style API: `contacts list --agent --select results.first_name,results.email` projects both fields correctly through the new stack.
- **Phase 3 plan committed** — `d4ab32d` on agent-cli. `docs/phase-3-glitchtip-plan.md` scopes 5 build slices for `glitchtip-cli` as second-domain validation. Probed GlitchTip's Sentry-compat API live; framed the comparison honestly (no head-to-head against `sentry-cli` — it's upload-side, different problem) as "agent + curl/jq today vs. agent + glitchtip-cli". Token already in `pass innovedia/glitchtip/api-token`.
- **Bash parallel-cwd footgun captured to memory** — `feedback_bash_parallel_cwd.md` + MEMORY.md index entry. Rule: parallel Bash tool calls share session cwd; use `git -C <path>` or self-contained `cd <abs> && ...` per call. Bit me 3× this session before the rule landed. First post-rule commits used `git -C` cleanly.

## 2. Current state (verified)

**All three repos clean:**
- agent-cli: `d4ab32d` on `main`, working tree clean, pushed to origin
- platform-cli: `5334ff4` on `main`, working tree clean, pushed to origin
- innovedia-platform: `2159a1f` on `main` (carried over from prior session, untouched this session)

**Verified working right now:**
- `cd /projects/libraries/agent-cli && /home/innovedia-admin/.local/go/bin/go test -race ./... -count=1` → 8 packages pass (agent, envelope, exitcode, **httpclient**, introspect, mirror, output, selectfields). httpclient ~1.15s; mirror ~2s; rest sub-second.
- `cd /projects/libraries/agent-cli && /home/innovedia-admin/.local/go/bin/go vet ./...` → clean
- `cd /projects/libraries/platform-cli && /home/innovedia-admin/.local/go/bin/go build ./...` → clean (no test files in platform-cli; vet clean)
- Live API smoke (PLATFORM_API_URL via ClusterIP, PLATFORM_EMAIL=mcp@innovedia.internal): `platform-cli contacts list --agent --select results.first_name,results.email` → projects both fields via the new authclient→httpclient stack.
- GlitchTip API smoke: `curl -H "Authorization: Bearer $(pass show innovedia/glitchtip/api-token)" https://errors.innovedia.ai/api/0/organizations/` → returns innovedia org.

**Backups:**
- `~/.claude.json.bak-2026-05-11-platform-mcp-fix` (older session)
- `~/.claude.json.bak-2026-05-12-mcp-svc-account` (prior session's creds swap)

## 3. Unfinished / in-flight

- [ ] **Phase 3 Slice 0 — glitchtip-cli repo skeleton.** Plan committed at `docs/phase-3-glitchtip-plan.md`. Next concrete move spelled out in §6 of that doc. Pending, not blocked.
- [ ] **Phase 3 Slices 1-5** — auth + reads, issues + projection, mirror integration, FINDINGS writeup, optional writes. All scoped in the plan. Pending.
- [ ] **Webhook delivery retry wire-up** (`output --deliver webhook:` backed by httpclient) — small follow-up from the updated PLAN out-of-scope list. Low priority; deferred.
- [ ] **Rotate `jjoseph@innovedia.ai` admin password?** Still optional, still low urgency. Service-account-only access for MCP shrank the exposure surface already.

## 4. Known issues

- **Bash parallel-cwd footgun (now mitigated by rule)** — captured in `feedback_bash_parallel_cwd.md`. First two parallel-cd attempts this session ran in the wrong cwd silently; final commits used `git -C` and worked cleanly. If the rule slips again, escalate to a permission-prompt-style guard.
- **Sync still full-refresh per resource** (carried over) — fine at current scale, revisit when platform API exposes pagination.
- **`camelToSnake` for acronym runs is still naive** (carried over) — `URLPath` → `u_r_l_path`. Documented limitation; hasn't bitten anything real.
- **GlitchTip project-name duplication** — 8 `aion-ci-api-*` projects in `errors.innovedia.ai` from an old deployment-tag bug. Not glitchtip-cli's problem to fix, but flagged in the plan as a thing to surface when issues are projected.

## 5. Hot files

- `/projects/libraries/agent-cli/httpclient/httpclient.go` — NEW. Core `Client.Do` with retry loop, header injection, hook surface. ~210 LOC.
- `/projects/libraries/agent-cli/httpclient/retry.go` — NEW. `RetryPolicy`, `Decision`, `DefaultClassifier`, `parseRetryAfter`, backoff with optional jitter.
- `/projects/libraries/agent-cli/httpclient/error.go` — NEW. `APIError` implementing `exitcode.CodedError`; `sanitizeJSONResponse` for BOM/XSSI.
- `/projects/libraries/agent-cli/httpclient/json.go` — NEW. `Client.DoJSON` ergonomic wrapper; `Limiter` interface.
- `/projects/libraries/agent-cli/httpclient/httpclient_test.go` + `retry_internal_test.go` — NEW. 35 tests; black-box via `httptest.Server` + internal unit tests for `parseRetryAfter` and `DefaultClassifier`.
- `/projects/libraries/agent-cli/docs/phase-3-glitchtip-plan.md` — NEW. 5-slice runway for glitchtip-cli build.
- `/projects/libraries/platform-cli/internal/authclient/authclient.go` — REFACTORED. Built on httpclient; same public surface; +5xx retry as bonus.
- `/projects/libraries/agent-cli/{PLAN.md,README.md}` — UPDATED. v0.1b roadmap entry marked shipped; package surface table extended with `httpclient` + `mirror`.
- `~/.claude/projects/-/memory/feedback_bash_parallel_cwd.md` + `MEMORY.md` — NEW. Persistent rule capture.

## 6. Next concrete step

**Phase 3 Slice 0 — `glitchtip-cli` repo skeleton.** Read `docs/phase-3-glitchtip-plan.md` end-to-end first (it's the runbook). Then:

```bash
# 1. Scaffold
mkdir -p /projects/libraries/glitchtip-cli/{cmd/glitchtip-cli,internal/{apiclient,client}}
cd /projects/libraries/glitchtip-cli && git init

# 2. go.mod (mirror platform-cli's setup — see /projects/libraries/platform-cli/go.mod)
#    Module: github.com/innovediatech/glitchtip-cli
#    Replace: github.com/innovediatech/agent-cli => /projects/libraries/agent-cli

# 3. main.go with cobra root + agent.Bind + introspect.EmitCommand
# 4. PLAN.md (scoped from the agent-cli phase-3 plan), README.md stub
# 5. GitHub repo via `gh repo create innovediatech/glitchtip-cli --private`
# 6. First commit + push
```

**Exit criterion for Slice 0:** `glitchtip-cli agent-context` runs and emits valid JSON.

Token + smoke target:
```bash
TOKEN=$(pass show innovedia/glitchtip/api-token)
curl -H "Authorization: Bearer $TOKEN" https://errors.innovedia.ai/api/0/organizations/
```

## 7. Open questions for Jayson

1. **Public-ify `agent-cli` later?** Still on the table. No Innovedia-specific code in agent-cli (platform-cli is the consumer). Reasonable after `httpclient` README example lands and glitchtip-cli is a second public proof. Not now.
2. **GlitchTip default org via env var vs. config file?** Plan §"Open questions" called env var (`GLITCHTIP_ORG`) for v1, config file for v2. Worth a 30-second sanity check before Slice 0.
3. **Phase 3 Slice 5 (writes) — ship or skip?** Plan flags it as optional. Writes are nice-to-have (resolve/ignore from CLI) but reads are the agent-native value. Decide after Slice 4's FINDINGS writeup reveals whether anything else is missing.

## Cross-references

- **agent-cli FINDINGS / experiment writeup:** `~/pp-sandbox/FINDINGS.md` (Printing Press evaluation, Phase 1)
- **platform-cli comparison harness:** `/projects/libraries/platform-cli/FINDINGS.md` (Phase 2 + 2b)
- **Phase 3 plan (authoritative for next session):** `/projects/libraries/agent-cli/docs/phase-3-glitchtip-plan.md`
- **innovedia-platform repo's last handoff:** `/projects/apps/innovedia-platform/SESSION.md` (2026-05-09, Slice 1a Operations Layer — unrelated)
