# @innovediatech/agent-cli

Go library that gives every Innovedia CLI agent-native ergonomics by default. Drop it on top of [Cobra](https://github.com/spf13/cobra) and inherit the patterns the Printing Press experiment validated: `--agent` mega-flag, typed exit codes, response envelopes, `--select` projection, stderr/stdout discipline, file/webhook delivery sinks.

**Status:** v0. See [`PLAN.md`](./PLAN.md) for design rationale and what's coming.

## Why

The [Printing Press](https://printingpress.dev/) experiment (write-up at `~/pp-sandbox/FINDINGS.md`) showed that the durable value of agent-native CLIs lives in the *patterns* — not the generator. This library codifies those patterns so every Innovedia CLI behaves the same way to an agent, regardless of whether it's hand-built or generated.

## Install

```bash
go get github.com/innovediatech/agent-cli
```

## 5-minute walkthrough

```go
package main

import (
    "fmt"
    "os"
    "time"

    "github.com/spf13/cobra"

    "github.com/innovediatech/agent-cli/agent"
    "github.com/innovediatech/agent-cli/envelope"
    "github.com/innovediatech/agent-cli/exitcode"
    "github.com/innovediatech/agent-cli/introspect"
    "github.com/innovediatech/agent-cli/output"
)

func main() {
    root := &cobra.Command{
        Use:           "mycli",
        SilenceUsage:  true,
        SilenceErrors: true,
    }
    flags := agent.Bind(root)

    root.AddCommand(&cobra.Command{
        Use:  "list",
        RunE: func(cmd *cobra.Command, args []string) error {
            rows := []any{
                map[string]any{"id": "1", "name": "alice"},
                map[string]any{"id": "2", "name": "bob"},
            }
            env := envelope.Live(rows)
            return output.Write(cmd.OutOrStdout(), flags, env,
                func() string { return fmt.Sprintf("%d results (live)", len(rows)) })
        },
    })

    introspect.EmitCommand(root, "0.1.0") // adds `mycli agent-context`

    if err := root.Execute(); err != nil {
        fmt.Fprintln(os.Stderr, "Error:", err)
        os.Exit(exitcode.Classify(err))
    }
    _ = time.Now()
}
```

That's a complete agent-native CLI. Out of the box it supports:

- `mycli list` → human-friendly default
- `mycli list --agent` → JSON, compact, no prompts, no color, auto-yes
- `mycli list --select results.name` → keeps only the `name` field of each result
- `mycli list --csv --select results.name,results.id` → CSV with envelope auto-unwrapped
- `mycli list --deliver file:./out.json` → atomic write to file, no stdout
- `mycli list --deliver webhook:https://...` → POSTs the body
- Typed exit codes propagated automatically when you `return exitcode.Errorf(exitcode.Auth, "...")`

## Package surface

| Package | Purpose |
|---|---|
| [`agent`](./agent) | Binds the canonical flag set. `--agent`, `--json`, `--csv`, `--plain`, `--compact`, `--select`, `--no-input`, `--yes`, `--no-color`, `--human-friendly`, `--dry-run`, `--data-source`, `--deliver`. Exposes `Mode()`, `Source()`, `Selector()` plus `Effective*()` accessors that respect `--agent`. |
| [`exitcode`](./exitcode) | Typed exit codes (0/2/3/4/5/7/10) + `Errorf`, `Wrap`, `FromHTTP`, `Classify`. Drop `os.Exit(exitcode.Classify(err))` into `main`. |
| [`envelope`](./envelope) | `Live(data)` and `Local(data, syncedAt, reason)` constructors that produce the `{meta, results}` shape. Provenance always attached. |
| [`selectfields`](./selectfields) | Compiles `--select id,user.email,items.name` and applies it to any JSON-shaped value. Arrays auto-traverse. |
| [`output`](./output) | The single render-and-deliver entry point. Handles mode (JSON/CSV/plain), `--compact`, projection, and `--deliver` (stdout/file/webhook). Envelope-aware: tabular modes auto-unwrap `results`. Auto-compact when piped; `--pretty` to force indent. |
| [`introspect`](./introspect) | Walks the Cobra tree and emits a JSON `agent-context` describing the CLI surface (global flags, commands, subcommands, per-command flags). One line in `main()` registers an `agent-context` subcommand. |
| [`mirror`](./mirror) | Resource-agnostic SQLite local mirror with FTS5, cursor sync, optional typed columns. Powers `--data-source local`. |
| [`httpclient`](./httpclient) | Retry-aware HTTP transport with structured error classification (composes with `exitcode`), pluggable request/response hooks for auth, and optional rate limiting. Powers [platform-cli](https://github.com/innovediatech/platform-cli) (session-auth + 401-retry) and [glitchtip-cli](https://github.com/innovediatech/glitchtip-cli) (bearer auth + Link-header pagination). See [`httpclient/README.md`](./httpclient/README.md). |

## Patterns this gives you

These are the patterns we extracted from Printing Press's generated code and validated against `petstore` and `stripe` CLIs (765 commands). Every CLI built on this lib inherits them for free:

1. **`--agent` mega-flag** — one flag flips on JSON, compact, no-input, no-color, yes. Explicit flags always win.
2. **Response envelope** — `{meta: {source: "live"|"local", synced_at, reason, request_id}, results: ...}`. Agents always know where to look.
3. **Typed exit codes** — branch on `$?` without parsing output.
4. **`--select` projection** — minimize context spend with dotted-path field selection.
5. **stderr/stdout discipline** — JSON on stdout, human summary on stderr (and only when stdout is a terminal).
6. **Composable delivery** — `--deliver file:|webhook:` for piping into other tools.
7. **Provenance tracking** — `envelope.Local(..., syncedAt, reason)` always tells the agent how fresh the data is.

## Roadmap

- **v0.1a — shipped 2026-05-11** — `introspect` package; auto-compact JSON when piped (`--pretty` to override).
- **v0.1b — shipped 2026-05-12** — `mirror` (SQLite + FTS5 + cursors) and `httpclient` (retry/backoff/classification, hook surface for auth and rate limiting). Extensions surfaced by the [glitchtip-cli](https://github.com/innovediatech/glitchtip-cli) build: `envelope.Meta.NextCursor` for paginated agent surfaces, `httpclient.DoJSONHeaders` for response-header inspection (cursors, rate-limit budgets), `mirror.Reset` for atomic full-data wipes including typed side-tables. `output.--deliver webhook:` now goes through `httpclient` for retry + backoff + Retry-After + typed exit codes.
- **v0.2** — profile system (saved flag sets), `feedback` command pattern.
- **v0.3** — generator integration: post-process Printing-Press-generated CLIs through this lib for uniformity.

## Quality gate

```bash
make verify    # vet + test + build + smoke-test echo-cli end-to-end
make vuln      # govulncheck against Go vulnerability database
```

We hold this library to the same bar Printing Press uses for its generated output. See `Makefile`.

## Reference CLI

[`examples/echo-cli/`](./examples/echo-cli) is a tiny CLI that exercises every pattern. Read it end-to-end; it's the shortest path to understanding what this lib gives you.

```bash
go build -o echo-cli ./examples/echo-cli
./echo-cli greet world --agent
./echo-cli list --csv --select results.name,results.cost
./echo-cli fail --code 4; echo "exit=$?"
```
