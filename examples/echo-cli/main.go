// echo-cli is the reference CLI for github.com/innovediatech/agent-cli.
//
// It exercises every pattern the lib offers — the agent flag set, --select
// projection, --deliver sinks, typed exit codes, response envelopes — in a
// trivial command surface so contributors can read it end-to-end.
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
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(exitcode.Classify(err))
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "echo-cli",
		Short:         "Reference CLI for agent-cli patterns",
		SilenceUsage:  true, // never dump --help on error; agents already know
		SilenceErrors: true, // we print the error ourselves with a clean exit
	}
	flags := agent.Bind(root)

	root.AddCommand(newGreetCmd(flags))
	root.AddCommand(newListCmd(flags))
	root.AddCommand(newFailCmd(flags))
	introspect.EmitCommand(root, "0.1.0")
	return root
}

func newGreetCmd(flags *agent.Flags) *cobra.Command {
	return &cobra.Command{
		Use:   "greet <name>",
		Short: "Greet a name and return a record",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			rec := map[string]any{
				"id":       fmt.Sprintf("greet_%d", time.Now().UnixNano()),
				"name":     name,
				"greeting": "hello, " + name,
				"at":       time.Now().UTC().Format(time.RFC3339),
			}
			env := envelope.Live(rec)
			return output.Write(cmd.OutOrStdout(), flags, env,
				func() string { return "1 result (live)" })
		},
	}
}

func newListCmd(flags *agent.Flags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Return a small array of records",
		RunE: func(cmd *cobra.Command, args []string) error {
			recs := []any{
				map[string]any{"id": "a", "name": "alice", "cost": 10},
				map[string]any{"id": "b", "name": "bob", "cost": 20},
				map[string]any{"id": "c", "name": "carol", "cost": 30},
			}
			env := envelope.Live(recs)
			return output.Write(cmd.OutOrStdout(), flags, env,
				func() string { return fmt.Sprintf("%d results (live)", len(recs)) })
		},
	}
}

func newFailCmd(flags *agent.Flags) *cobra.Command {
	var code int
	cmd := &cobra.Command{
		Use:   "fail",
		Short: "Exit with a specific typed exit code (for testing agent branching)",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch code {
			case exitcode.Success:
				return nil
			case exitcode.Usage:
				return exitcode.Errorf(exitcode.Usage, "simulated usage error")
			case exitcode.NotFound:
				return exitcode.Errorf(exitcode.NotFound, "simulated not-found")
			case exitcode.Auth:
				return exitcode.Errorf(exitcode.Auth, "simulated auth failure")
			case exitcode.API:
				return exitcode.Errorf(exitcode.API, "simulated upstream API error")
			case exitcode.RateLimited:
				return exitcode.Errorf(exitcode.RateLimited, "simulated rate limit")
			case exitcode.Config:
				return exitcode.Errorf(exitcode.Config, "simulated config error")
			default:
				return exitcode.Errorf(exitcode.API, "unknown code %d; pass one of 0,2,3,4,5,7,10", code)
			}
		},
	}
	cmd.Flags().IntVar(&code, "code", exitcode.API, "Exit code to simulate (0,2,3,4,5,7,10)")
	return cmd
}
