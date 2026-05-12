package output

import (
	"bytes"
	"strings"
	"testing"
)

// When piped (writer is *bytes.Buffer, not a tty) and no explicit mode,
// the default should be compact — pipes never benefit from indentation.
func TestPipedDefaultIsCompact(t *testing.T) {
	var buf bytes.Buffer
	flags := newFlags(t) // no args; pure defaults
	data := map[string]any{"id": 1, "name": "alice"}
	if err := Write(&buf, flags, data, nil); err != nil {
		t.Fatal(err)
	}
	// Compact JSON has no leading whitespace inside the object.
	if strings.Contains(buf.String(), "\n  ") {
		t.Errorf("piped default should be compact (no indent), got %q", buf.String())
	}
}

// --pretty must force indentation even when piped — explicit human override.
func TestPrettyForcesIndentEvenPiped(t *testing.T) {
	var buf bytes.Buffer
	flags := newFlags(t, "--pretty")
	data := map[string]any{"id": 1, "name": "alice"}
	if err := Write(&buf, flags, data, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "\n  ") {
		t.Errorf("--pretty should force indented JSON, got %q", buf.String())
	}
}

// --compact wins over --pretty.
func TestCompactBeatsPretty(t *testing.T) {
	var buf bytes.Buffer
	flags := newFlags(t, "--pretty", "--compact")
	data := map[string]any{"id": 1, "name": "alice"}
	if err := Write(&buf, flags, data, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "\n  ") {
		t.Errorf("--compact should beat --pretty, got %q", buf.String())
	}
}
