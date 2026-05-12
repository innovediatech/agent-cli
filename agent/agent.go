// Package agent wires the canonical agent-native flag set onto a Cobra
// command and exposes computed accessors that respect the --agent mega-flag.
//
// Typical use:
//
//	rootCmd := &cobra.Command{Use: "mycli"}
//	flags := agent.Bind(rootCmd)            // adds all standard flags
//	subCmd := &cobra.Command{
//		Use: "list",
//		RunE: func(cmd *cobra.Command, args []string) error {
//			data := fetch()
//			return output.Write(cmd.OutOrStdout(), flags, data)
//		},
//	}
//	rootCmd.AddCommand(subCmd)
//
// --agent acts as a single switch that flips all agent-friendly defaults on:
//
//	--json --compact --no-input --no-color --yes
//
// Individual flags can still be set explicitly; explicit values always win
// over --agent's implied defaults.
package agent

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/innovediatech/agent-cli/selectfields"
)

// OutputMode is the chosen output rendering.
type OutputMode string

const (
	ModeAuto  OutputMode = "auto"  // human-friendly when tty, json otherwise
	ModeJSON  OutputMode = "json"  // always JSON
	ModeCSV   OutputMode = "csv"   // CSV for arrays
	ModePlain OutputMode = "plain" // tab-separated plain text
)

// DataSource is the read-time preference between live and local.
type DataSource string

const (
	DataSourceAuto  DataSource = "auto"  // live, fall back to local on failure
	DataSourceLive  DataSource = "live"  // live only
	DataSourceLocal DataSource = "local" // local mirror only
)

// Flags is the bag of canonical agent-native flags. Construct via Bind.
//
// Field names mirror the flag names for readability; the actual flag binding
// happens once in Bind so the Cobra `--help` ordering and defaults are stable
// across CLIs.
type Flags struct {
	// Output shaping
	Agent       bool
	JSON        bool
	CSV         bool
	Plain       bool
	Compact     bool
	Pretty      bool
	NoColor     bool
	HumanFriendly bool
	Select      string

	// Interaction
	NoInput bool
	Yes     bool

	// Behavior
	DryRun     string // raw flag value; rarely consulted directly
	DryRunFlag bool
	DataSource string
	Deliver    string
}

// Bind attaches the canonical flag set as persistent flags on cmd and returns
// a *Flags wired to those flag values. Call from main once on the root
// command; subcommands inherit the persistent flags.
func Bind(cmd *cobra.Command) *Flags {
	return BindOn(cmd.PersistentFlags())
}

// BindOn attaches the canonical flag set to an explicit FlagSet. Useful when
// you want the flags on a single subcommand or on a non-cobra pflag set.
func BindOn(fs *pflag.FlagSet) *Flags {
	f := &Flags{}
	fs.BoolVar(&f.Agent, "agent", false, "Set all agent-friendly defaults (--json --compact --no-input --no-color --yes)")
	fs.BoolVar(&f.JSON, "json", false, "Output as JSON")
	fs.BoolVar(&f.CSV, "csv", false, "Output as CSV (for arrays)")
	fs.BoolVar(&f.Plain, "plain", false, "Output as plain tab-separated text")
	fs.BoolVar(&f.Compact, "compact", false, "Return only key fields for minimal token usage")
	fs.BoolVar(&f.Pretty, "pretty", false, "Force indented JSON even when piped (overrides auto-compact)")
	fs.BoolVar(&f.NoColor, "no-color", false, "Disable colored output")
	fs.BoolVar(&f.HumanFriendly, "human-friendly", false, "Force colored, rich formatting even when piped")
	fs.StringVar(&f.Select, "select", "", "Comma-separated dotted-path field selection (e.g. id,name,user.email)")
	fs.BoolVar(&f.NoInput, "no-input", false, "Disable all interactive prompts")
	fs.BoolVar(&f.Yes, "yes", false, "Assume yes for all confirmations")
	fs.BoolVar(&f.DryRunFlag, "dry-run", false, "Show request without sending")
	fs.StringVar(&f.DataSource, "data-source", string(DataSourceAuto), "Read source: auto, live, or local")
	fs.StringVar(&f.Deliver, "deliver", "stdout", "Output sink: stdout, file:<path>, or webhook:<url>")
	return f
}

// IsAgent reports whether --agent was set OR any flag implied by it is set.
// Useful for branching on "should I behave non-interactively?" without
// checking each individual flag.
func (f *Flags) IsAgent() bool { return f.Agent }

// EffectiveJSON returns true when JSON output should be emitted.
// --json wins over --agent's implied json default; explicit --plain or --csv
// override --agent.
func (f *Flags) EffectiveJSON() bool {
	if f.CSV || f.Plain {
		return false
	}
	return f.JSON || f.Agent
}

// EffectiveCompact returns true when compact mode applies.
func (f *Flags) EffectiveCompact() bool { return f.Compact || f.Agent }

// EffectiveNoColor returns true when color is suppressed.
// --human-friendly explicitly opts out of agent's no-color.
func (f *Flags) EffectiveNoColor() bool {
	if f.HumanFriendly {
		return false
	}
	return f.NoColor || f.Agent
}

// EffectiveNoInput returns true when prompts must be skipped.
func (f *Flags) EffectiveNoInput() bool { return f.NoInput || f.Agent }

// EffectiveYes returns true when confirmation prompts auto-accept.
func (f *Flags) EffectiveYes() bool { return f.Yes || f.Agent }

// Mode returns the chosen output mode after applying --agent's defaults.
func (f *Flags) Mode() OutputMode {
	switch {
	case f.CSV:
		return ModeCSV
	case f.Plain:
		return ModePlain
	case f.EffectiveJSON():
		return ModeJSON
	default:
		return ModeAuto
	}
}

// Source returns the chosen data source.
func (f *Flags) Source() DataSource {
	switch DataSource(f.DataSource) {
	case DataSourceLive, DataSourceLocal:
		return DataSource(f.DataSource)
	default:
		return DataSourceAuto
	}
}

// Selector parses and returns the --select spec.
func (f *Flags) Selector() selectfields.Selector {
	return selectfields.Parse(f.Select)
}
