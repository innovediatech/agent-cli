package agent

import (
	"testing"

	"github.com/spf13/cobra"
)

func bindOnRoot(t *testing.T) (*cobra.Command, *Flags) {
	t.Helper()
	cmd := &cobra.Command{Use: "test"}
	flags := Bind(cmd)
	return cmd, flags
}

func TestAgentImpliesAgentDefaults(t *testing.T) {
	cmd, flags := bindOnRoot(t)
	cmd.SetArgs([]string{"--agent"})
	if err := cmd.ParseFlags([]string{"--agent"}); err != nil {
		t.Fatal(err)
	}
	if !flags.EffectiveJSON() {
		t.Errorf("--agent should imply json")
	}
	if !flags.EffectiveCompact() {
		t.Errorf("--agent should imply compact")
	}
	if !flags.EffectiveNoColor() {
		t.Errorf("--agent should imply no-color")
	}
	if !flags.EffectiveNoInput() {
		t.Errorf("--agent should imply no-input")
	}
	if !flags.EffectiveYes() {
		t.Errorf("--agent should imply yes")
	}
	if flags.Mode() != ModeJSON {
		t.Errorf("Mode() = %q, want json", flags.Mode())
	}
}

func TestExplicitFormatOverridesAgent(t *testing.T) {
	cmd, flags := bindOnRoot(t)
	if err := cmd.ParseFlags([]string{"--agent", "--csv"}); err != nil {
		t.Fatal(err)
	}
	if flags.Mode() != ModeCSV {
		t.Errorf("--csv should win over --agent's implied json, got %q", flags.Mode())
	}
	if flags.EffectiveJSON() {
		t.Errorf("--csv should disable EffectiveJSON")
	}
}

func TestHumanFriendlyOverridesNoColor(t *testing.T) {
	cmd, flags := bindOnRoot(t)
	if err := cmd.ParseFlags([]string{"--agent", "--human-friendly"}); err != nil {
		t.Fatal(err)
	}
	if flags.EffectiveNoColor() {
		t.Errorf("--human-friendly should disable no-color even under --agent")
	}
}

func TestDefaultMode(t *testing.T) {
	_, flags := bindOnRoot(t)
	if flags.Mode() != ModeAuto {
		t.Errorf("default Mode() = %q, want auto", flags.Mode())
	}
}

func TestSourceDefaultsToAuto(t *testing.T) {
	_, flags := bindOnRoot(t)
	if flags.Source() != DataSourceAuto {
		t.Errorf("default Source() = %q, want auto", flags.Source())
	}
}

func TestSourceExplicit(t *testing.T) {
	cmd, flags := bindOnRoot(t)
	if err := cmd.ParseFlags([]string{"--data-source", "local"}); err != nil {
		t.Fatal(err)
	}
	if flags.Source() != DataSourceLocal {
		t.Errorf("--data-source=local not honored")
	}
}

func TestSourceUnknownFallsBackToAuto(t *testing.T) {
	cmd, flags := bindOnRoot(t)
	if err := cmd.ParseFlags([]string{"--data-source", "garbage"}); err != nil {
		t.Fatal(err)
	}
	if flags.Source() != DataSourceAuto {
		t.Errorf("unknown source should fall back to auto, got %q", flags.Source())
	}
}

func TestSelector(t *testing.T) {
	cmd, flags := bindOnRoot(t)
	if err := cmd.ParseFlags([]string{"--select", "id,name"}); err != nil {
		t.Fatal(err)
	}
	sel := flags.Selector()
	if sel.Empty() {
		t.Fatal("selector should not be empty after --select")
	}
	in := map[string]any{"id": 1, "name": "x", "extra": "y"}
	got := sel.Apply(in)
	want := map[string]any{"id": 1, "name": "x"}
	if m, ok := got.(map[string]any); !ok || m["id"] != want["id"] || m["name"] != want["name"] || m["extra"] != nil {
		t.Fatalf("Selector().Apply did not project correctly, got %v", got)
	}
}
