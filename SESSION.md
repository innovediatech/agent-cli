# Session Handoff — 2026-05-12T13:20-04:00

**Project:** agent-cli + glitchtip-cli (Phase 3 followups) + cross-fleet sonner cleanup
**Branch:** all on `main`
**Last commit (agent-cli):** `a05eff2` — feat(output): wire --deliver webhook: through httpclient
**Last commit (glitchtip-cli):** `2acf13f` — chore(session): handoff after Phase 3 slices 2/3/4 complete (unchanged this session)
**Last commit (platform-cli):** `6e0be4f` — fix(mirror): use mirror.Reset for clear --all to wipe typed side-tables
**Last commit (fullstack-starter):** `fb1e9fd` — fix(web): bump sonner 1.7.1 -> 2.0.7 to fix hydration #418
**Deployed tag:** N/A — agent-cli/glitchtip-cli/platform-cli are libraries; fullstack-starter is a template. 8 client apps got dep-bump commits (not deployed this session).
**Session length:** ~2.5 hours, picked up cleanly from 2026-05-12T11:35 handoff (in glitchtip-cli/SESSION.md)

---

## 1. Just finished

- **agent-cli public on GitHub** — `f8d4ffc` (httpclient/README.md package walkthrough + top-level README cites both consumer proofs) → `gh repo edit --visibility public`. Description set. Live at github.com/innovediatech/agent-cli.
- **agent-cli webhook delivery now retries via httpclient** — `a05eff2`. `postWebhook` swapped from `http.DefaultClient` to a per-call `httpclient.Client`: 3 attempts, exp backoff, Retry-After honoring, 30s/attempt + 90s overall budget, terminal failure returns `*httpclient.APIError` which composes with `exitcode.Classify`. Three new tests verify retry-on-5xx, exhausted-retries, no-retry-on-4xx. PLAN.md/README updated — v0.2 bullet now struck through as v0.1b extension.
- **platform-cli `clear --all` now uses `mirror.Reset`** — `6e0be4f`. Atomic wipe covers `rt_companies`/`rt_contacts`/`rt_deals` typed side-tables (typed-table-leak bug fixed). Public surface unchanged. Smoke-tested against rebuilt binary.
- **GlitchTip cleanup** — 9 `aion-ci-*` projects deleted via Sentry-compat DELETE; bulk-resolved 5 React #418 issues via `PUT /api/0/organizations/innovedia/issues/?id=...` (per-issue PUT returns 405; bulk endpoint is the working shape).
- **React #418 root cause + remediation** — Located: `<Toaster richColors />` from fullstack-starter, sonner 1.7.4 on React 19, hits flushSync hydration bug fixed in sonner 2.0.1. Template fixed (`fb1e9fd`), then sonner bumped to ^2.0.7 across **8 client apps** in dep-bump commits: proreno, divinitycobridal, stratagenos, theregalbarbershop, springboardkids, dmsiq, flexhrpro, coremanaged.
- **coremanaged history rewrite** — `dc5a8ae security: remove secrets.yaml from git tracking` had a 209MB MP4 swept in alongside the security work, blocking all pushes. `git filter-branch --index-filter` dropped the MP4 from the 3 unpushed commits (new SHAs: `76ec523`/`a5856b8`/`9c4e07c`). MP4 preserved at `~/coremanaged-rescued/`. Followups: `.gitignore` hardened (`c0ccbb9` — `*.mp4`/`*.mov`/`*.zip`/`*.dmg` patterns) + 5 orphan docs/context binaries dropped from HEAD (`ad51af6`).

## 2. Current state (verified)

**All 12 repos clean and pushed, 0 ahead of origin:**

```
agent-cli              a05eff2  feat(output): wire --deliver webhook: through httpclient
glitchtip-cli          2acf13f  (unchanged this session)
platform-cli           6e0be4f  fix(mirror): use mirror.Reset for clear --all
fullstack-starter      fb1e9fd  fix(web): bump sonner 1.7.1 -> 2.0.7
coremanaged            ad51af6  chore(docs): drop orphan context binaries
proreno                5b9e84e  fix(web): bump sonner 1.7.1 -> 2.0.7
divinitycobridal       b7aa141  fix(web): bump sonner 1.7.x -> 2.0.7
stratagenos            34718bb  fix(web): bump sonner 1.7.x -> 2.0.7
theregalbarbershop     b421c67  fix(web): bump sonner 1.7.x -> 2.0.7
springboardkids        32b6644  fix(web): bump sonner 1.7.x -> 2.0.7
dmsiq                  11ef7ca  fix(web): bump sonner 1.7.x -> 2.0.7
flexhrpro              6ff6f4f  fix(web): bump sonner 1.7.x -> 2.0.7
```

**Tests passing right now:**
- `cd /projects/libraries/agent-cli && go test ./... -count=1` → 8 packages pass; output package 4.02s (3 new webhook retry tests add ~4s).
- `cd /projects/libraries/glitchtip-cli && go test ./... -count=1` → `internal/client` passes (no regressions from this session — unchanged).

**Pre-existing WIP that I deliberately did not touch in any repo:**
- coremanaged: `M CLAUDE.md`, `M apps/api/src/modules/ai/ai.service.ts`, untracked `SESSION.md`
- flexhrpro: `M CLAUDE.md`, `M apps/api/src/modules/ocr/ocr.service.ts`, untracked `SESSION.md`
- divinitycobridal / stratagenos / theregalbarbershop / fullstack-starter: untracked `SESSION.md` (or in fullstack-starter, `M docs/research.md`)
- springboardkids: `M CLAUDE.md`, `M docs/source-of-truth.md`, multiple untracked context PDFs/photos
- dmsiq: `M docs/decisions.md`, `M docs/source-of-truth.md`, untracked `backups/` and audit doc
- cart-n-craft: active WIP on `apps/web/package.json` + `pnpm-lock.yaml` (qrcode feature in flight)
- omex: on feature branch `slice/6-1-synthesis-pipeline`, 5 commits ahead of main

**Live API smokes:**
- GlitchTip cleanup verified: `curl ... /organizations/innovedia/projects/ | grep aion` → no remaining aion-* projects.
- All 5 React #418 issues: `status=resolved`.

## 3. Unfinished / in-flight

- [ ] **omex sonner bump** — skipped because it's on feature branch `slice/6-1-synthesis-pipeline`. Apply on `main` once Slice 6 lands.
- [ ] **cart-n-craft sonner bump** — skipped because qrcode WIP is touching the exact files (`apps/web/package.json` + `pnpm-lock.yaml`). Apply after that work lands.
- [ ] **Innovedia OS / Phase X.0+** — completely untouched this session; see `~/.claude/projects/-/memory/innovedia_os_reframe_2026_04_19.md` and `x0_build_state_2026_04_27.md` for actual roadmap state.
- [ ] **Public-ify glitchtip-cli too?** — agent-cli is now public; glitchtip-cli is the cleanest second-domain validation proof. Worth flipping at the same time, but not asked.

## 4. Known issues

- **GlitchTip per-issue PUT returns 405.** Workaround: use the bulk endpoint `PUT /api/0/organizations/{org}/issues/?id=X&id=Y...`. If a future glitchtip-cli `issues resolve` command lands (Slice 5 was skipped), it should call the bulk endpoint with one ID, not the per-issue one.
- **coremanaged history rewrite changed SHAs** — anyone with a stale local clone of coremanaged will need `git fetch --all && git reset --hard origin/main` (or a fresh clone) since `dc5a8ae`/`e2c1f6e`/`7a6e2aa` no longer exist on origin (replaced by `76ec523`/`a5856b8`/`9c4e07c`, then `c0ccbb9` and `ad51af6` on top). Jayson is the only consumer so far → low risk.
- **Sonner bump is unverified in production** — Existing 8 apps now have sonner 2.0.7 pinned in lockfiles, but **none have been redeployed**. The fix only lands in prod at the next `make deploy-web` per project. React #418 won't recur in prod until then.
- **Bash parallel-cwd footgun** (carried over from prior handoffs) — held this session; `git -C <abs>` pattern was used throughout.

## 5. Hot files

- `/projects/libraries/agent-cli/output/output.go` — `postWebhook` rewritten to use `httpclient.Client`.
- `/projects/libraries/agent-cli/output/output_test.go` — 3 new tests for webhook retry semantics (retry-on-5xx, exhausted-retries-returns-APIError, no-retry-on-4xx).
- `/projects/libraries/agent-cli/httpclient/README.md` — NEW. Package walkthrough for public agent-cli release.
- `/projects/libraries/agent-cli/{README.md,PLAN.md}` — updated to cite both consumer proofs (platform-cli + glitchtip-cli) and strike the v0.2 webhook bullet.
- `/projects/libraries/platform-cli/cmd/platform-cli/mirror_cmd.go` — `clear --all` swapped to `mirror.Reset`.
- `/projects/templates/fullstack-starter/apps/web/package.json` + `pnpm-lock.yaml` — sonner ^1.7.1 → ^2.0.7.
- `/projects/apps/coremanaged/.gitignore` — added `*.mp4`/`*.mov`/`*.zip`/`*.dmg` blocks.

## 6. Next concrete step

**Likely a fresh thread on a different concern entirely** (Innovedia OS, MariLexis, client work). No active in-progress task in any of the 12 repos touched today.

If picking up exactly this thread:

```bash
# 1. Decide whether to bump omex + cart-n-craft once their WIP lands:
git -C /projects/apps/omex log --oneline main..HEAD | head -1
git -C /projects/apps/cart-n-craft status

# 2. Or trigger sonner-bump redeploys for the 8 apps that already have the
#    fix in their lockfiles but unbuilt images. From each app dir:
make deploy-web TAG=v<next>
```

## 7. Open questions for Jayson

1. **Redeploy the 8 sonner-bumped apps now, or let them ride to next natural deploy cycle?** Each is committed-but-unbuilt. Next-natural-cycle is fine since #418 is rare (one event per project to date), but waiting means the next stray event still ships as the old build.
2. **Public-ify glitchtip-cli alongside agent-cli?** No Innovedia-specific code in the package, and it's the second-domain validation proof for agent-cli. Trivial repo edit + visibility flip if you want both public.
3. **Slice 5 of glitchtip-cli (write commands) — decision stands as skip?** Today's GlitchTip cleanup needed `projects delete` + `issues resolve` (bulk shape) and I curled them directly. ~20-min build if the cleanup workflow becomes recurring.

## Cross-references

- **Prior handoff this morning (now superseded):** `/projects/libraries/glitchtip-cli/SESSION.md` (2026-05-12T11:35) — kept for the Phase 3 outcome detail it captured.
- **Phase 3 outcome:** `/projects/libraries/glitchtip-cli/FINDINGS.md` — second-domain validation writeup, still authoritative.
- **agent-cli plan:** `/projects/libraries/agent-cli/PLAN.md` — v0.1b webhook bullet now struck through; v0.2 still has profile system + feedback pattern.
- **Recovered MP4 from coremanaged history rewrite:** `~/coremanaged-rescued/Innovedia - Jose Collado-20260219_165801-Meeting Recording.mp4` (209MB; was a meeting recording that got swept into a security commit by accident).
