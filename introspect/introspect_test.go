package introspect

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/innovediatech/agent-cli/agent"
)

func newDemoTree(t *testing.T) *cobra.Command {
	t.Helper()
	root := &cobra.Command{Use: "mycli", Short: "demo"}
	_ = agent.Bind(root)

	parent := &cobra.Command{Use: "deals", Short: "manage deals"}
	parent.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "list deals",
		Run:   func(cmd *cobra.Command, args []string) {},
	})
	parent.AddCommand(&cobra.Command{
		Use:    "secret",
		Short:  "internal-only",
		Hidden: true,
		Run:    func(cmd *cobra.Command, args []string) {},
	})
	root.AddCommand(parent)
	return root
}

func TestEmitShape(t *testing.T) {
	root := newDemoTree(t)
	schema := Emit(root, "1.2.3")

	if schema.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version mismatch: got %q", schema.SchemaVersion)
	}
	if schema.CLI.Name != "mycli" || schema.CLI.Version != "1.2.3" {
		t.Errorf("CLI meta wrong: %+v", schema.CLI)
	}
	if len(schema.Commands) != 1 || schema.Commands[0].Name != "deals" {
		t.Fatalf("expected one top-level command (deals), got %+v", schema.Commands)
	}
	deals := schema.Commands[0]
	// hidden 'secret' should be skipped; only 'list' visible
	if len(deals.Subcommands) != 1 || deals.Subcommands[0].Name != "list" {
		t.Errorf("subcommands wrong: %+v", deals.Subcommands)
	}
}

func TestEmitJSONExcludesNoise(t *testing.T) {
	root := newDemoTree(t)
	var buf bytes.Buffer
	if err := EmitJSON(root, "v1", &buf); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	// Cobra auto-registers 'help' and 'completion'; both should be filtered.
	if strings.Contains(s, `"name":"help"`) {
		t.Errorf("emit should drop 'help', got %s", s)
	}
	if strings.Contains(s, `"name":"completion"`) {
		t.Errorf("emit should drop 'completion', got %s", s)
	}
}

func TestEmitJSONIncludesGlobalFlags(t *testing.T) {
	root := newDemoTree(t)
	var buf bytes.Buffer
	if err := EmitJSON(root, "v1", &buf); err != nil {
		t.Fatal(err)
	}
	var got struct {
		GlobalFlags []struct {
			Name string `json:"name"`
		} `json:"global_flags"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"agent": false, "json": false, "compact": false,
		"select": false, "deliver": false, "pretty": false,
	}
	for _, f := range got.GlobalFlags {
		if _, ok := want[f.Name]; ok {
			want[f.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("global_flags missing %q", name)
		}
	}
}

func TestEmitCommandRegisters(t *testing.T) {
	root := newDemoTree(t)
	EmitCommand(root, "v1")

	// Find the agent-context subcommand and run it.
	var ac *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "agent-context" {
			ac = c
			break
		}
	}
	if ac == nil {
		t.Fatal("agent-context not registered")
	}

	var buf bytes.Buffer
	ac.SetOut(&buf)
	if err := ac.RunE(ac, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"name":"deals"`) {
		t.Errorf("agent-context output should mention deals; got %s", buf.String())
	}
}
