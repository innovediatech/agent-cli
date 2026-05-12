// Package introspect emits a JSON description of a Cobra command tree.
//
// An agent that drops into a shell with an unfamiliar CLI shouldn't have to
// walk --help subcommand by subcommand to learn the surface. An `agent-context`
// subcommand backed by this package gives the agent the entire command tree
// in one fetch.
//
// The output is intentionally compact: command names, flags (name + type +
// default + usage), and subcommand recursion. No long descriptions, no
// auto-generated `help` / `completion` clutter.
package introspect

import (
	"encoding/json"
	"io"
	"sort"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// SchemaVersion is bumped when the JSON shape changes incompatibly.
const SchemaVersion = "1"

// CLISchema is the root document.
type CLISchema struct {
	SchemaVersion string    `json:"schema_version"`
	CLI           CLIMeta   `json:"cli"`
	GlobalFlags   []Flag    `json:"global_flags,omitempty"`
	Commands      []Command `json:"commands"`
}

// CLIMeta describes the program itself.
type CLIMeta struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
}

// Command describes one node in the tree.
type Command struct {
	Name        string    `json:"name"`
	Use         string    `json:"use,omitempty"`
	Short       string    `json:"short,omitempty"`
	Aliases     []string  `json:"aliases,omitempty"`
	Flags       []Flag    `json:"flags,omitempty"`
	Subcommands []Command `json:"subcommands,omitempty"`
}

// Flag describes a single CLI flag.
type Flag struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand,omitempty"`
	Type      string `json:"type"`
	Default   string `json:"default,omitempty"`
	Usage     string `json:"usage,omitempty"`
}

// Emit builds a CLISchema from a root Cobra command.
//
// Auto-generated `help` and `completion` subcommands are filtered out.
// Hidden commands (Hidden=true) are also filtered. Subcommand recursion is
// deterministic via sort-by-name so the schema is stable across runs.
func Emit(root *cobra.Command, version string) CLISchema {
	return CLISchema{
		SchemaVersion: SchemaVersion,
		CLI: CLIMeta{
			Name:        root.Name(),
			Description: root.Short,
			Version:     version,
		},
		GlobalFlags: flagsOf(root.PersistentFlags()),
		Commands:    childrenOf(root),
	}
}

// EmitJSON writes Emit's output as JSON to w. Always compact JSON — the
// caller can pretty-print if needed; the default contract is small.
func EmitJSON(root *cobra.Command, version string, w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(Emit(root, version))
}

// EmitCommand registers an `agent-context` subcommand on root that emits the
// schema. Call once during command setup.
func EmitCommand(root *cobra.Command, version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent-context",
		Short: "Emit a JSON description of this CLI for agent self-discovery",
		RunE: func(cmd *cobra.Command, args []string) error {
			return EmitJSON(root, version, cmd.OutOrStdout())
		},
	}
	root.AddCommand(cmd)
	return cmd
}

func childrenOf(parent *cobra.Command) []Command {
	kids := parent.Commands()
	out := make([]Command, 0, len(kids))
	for _, c := range kids {
		if shouldSkip(c) {
			continue
		}
		out = append(out, Command{
			Name:        c.Name(),
			Use:         c.Use,
			Short:       c.Short,
			Aliases:     c.Aliases,
			Flags:       flagsOf(c.LocalFlags()),
			Subcommands: childrenOf(c),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func shouldSkip(c *cobra.Command) bool {
	if c.Hidden || !c.IsAvailableCommand() {
		return true
	}
	switch c.Name() {
	case "help", "completion":
		return true
	}
	return false
}

func flagsOf(fs *pflag.FlagSet) []Flag {
	if fs == nil {
		return nil
	}
	var out []Flag
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		out = append(out, Flag{
			Name:      f.Name,
			Shorthand: f.Shorthand,
			Type:      f.Value.Type(),
			Default:   f.DefValue,
			Usage:     f.Usage,
		})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
