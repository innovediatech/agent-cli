// Package output is the single render-and-deliver point for agent-native
// CLIs. It applies --select projection, picks the right rendering mode (JSON
// vs CSV vs plain vs auto-table), and routes the result to stdout, a file,
// or a webhook based on --deliver.
//
// Discipline: data ALWAYS goes to the chosen sink; human summaries go to
// stderr (and only when the sink is a terminal). Piped/agent consumers get
// clean output on the data stream.
package output

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/innovediatech/agent-cli/agent"
	"github.com/innovediatech/agent-cli/exitcode"
)

// Sink is the parsed --deliver value.
type Sink struct {
	Kind   SinkKind
	Target string // path for file, url for webhook, empty for stdout
}

type SinkKind int

const (
	SinkStdout SinkKind = iota
	SinkFile
	SinkWebhook
)

// ParseSink decodes a --deliver string. Supported forms:
//
//	stdout            → SinkStdout
//	file:<path>       → SinkFile
//	webhook:<url>     → SinkWebhook (http: or https: only)
//
// Returns an exitcode.Errorf(Usage) for unknown schemes.
func ParseSink(spec string) (Sink, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" || spec == "stdout" {
		return Sink{Kind: SinkStdout}, nil
	}
	switch {
	case strings.HasPrefix(spec, "file:"):
		path := strings.TrimPrefix(spec, "file:")
		if path == "" {
			return Sink{}, exitcode.Errorf(exitcode.Usage, "--deliver file: requires a path")
		}
		return Sink{Kind: SinkFile, Target: path}, nil
	case strings.HasPrefix(spec, "webhook:"):
		url := strings.TrimPrefix(spec, "webhook:")
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			return Sink{}, exitcode.Errorf(exitcode.Usage, "--deliver webhook: requires http(s):// URL, got %q", url)
		}
		return Sink{Kind: SinkWebhook, Target: url}, nil
	default:
		return Sink{}, exitcode.Errorf(exitcode.Usage,
			"--deliver: unknown sink %q; supported: stdout, file:<path>, webhook:<url>", spec)
	}
}

// Write is the standard render-and-deliver helper. It:
//
//  1. Applies --select projection to data.
//  2. Renders to bytes using the chosen mode (JSON / CSV / plain / auto).
//  3. Delivers to the chosen sink (stdout / file / webhook).
//
// The summary callback, if non-nil, is invoked with a one-line stderr summary
// to print after data lands — but only when the data sink is a terminal
// stdout. Piped consumers never see the summary on their data stream.
//
// data may be any JSON-marshalable value. Envelopes from the envelope
// package are handled transparently.
func Write(w io.Writer, flags *agent.Flags, data any, summary func() string) error {
	sink, err := ParseSink(flags.Deliver)
	if err != nil {
		return err
	}

	// Normalize through JSON so structs, maps, and slices land in a single
	// shape (map[string]any / []any / primitives). This is what makes
	// --select projection and --csv flattening work uniformly regardless of
	// whether the caller passed an envelope.Envelope, a domain struct, or a
	// hand-built map.
	normalized, err := normalize(data)
	if err != nil {
		return fmt.Errorf("normalizing data: %w", err)
	}

	projected := flags.Selector().Apply(normalized)

	// Decide if JSON should be indented. The principle: indented output
	// helps humans, hurts agents. So:
	//   --compact / --agent  → never indented
	//   --pretty             → always indented (explicit human override)
	//   default              → indented ONLY when writing to a TTY stdout
	indent := false
	if !flags.EffectiveCompact() {
		switch {
		case flags.Pretty:
			indent = true
		case sink.Kind == SinkStdout && isTerminal(w):
			indent = true
		}
	}

	body, err := render(flags, projected, indent)
	if err != nil {
		return err
	}

	if err := deliver(w, sink, body); err != nil {
		return err
	}

	if summary != nil && sink.Kind == SinkStdout && isTerminal(w) {
		fmt.Fprintln(os.Stderr, summary())
	}
	return nil
}

// normalize round-trips data through encoding/json so every value becomes
// map[string]any / []any / primitives. This lets downstream code (selectfields,
// CSV flattening) treat all inputs uniformly without reflection.
func normalize(v any) (any, error) {
	buf, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(buf, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func render(flags *agent.Flags, data any, indent bool) ([]byte, error) {
	mode := flags.Mode()
	// Auto mode: terminal-friendly text when interactive; JSON otherwise.
	if mode == agent.ModeAuto {
		// Without knowing the sink, we make the safe choice: JSON. A CLI
		// that wants human-table rendering on a terminal can detect that
		// itself and bypass this helper. Keeping auto = JSON keeps the
		// library's contract predictable for piped consumers.
		mode = agent.ModeJSON
	}
	switch mode {
	case agent.ModeJSON:
		var buf []byte
		var err error
		if indent {
			buf, err = json.MarshalIndent(data, "", "  ")
		} else {
			buf, err = json.Marshal(data)
		}
		if err != nil {
			return nil, fmt.Errorf("rendering JSON: %w", err)
		}
		buf = append(buf, '\n')
		return buf, nil

	case agent.ModeCSV:
		rows, ok := toRows(data)
		if !ok {
			return nil, exitcode.Errorf(exitcode.Usage,
				"--csv requires array of objects or a single object; got %T", data)
		}
		var sb strings.Builder
		writer := csv.NewWriter(&sb)
		if len(rows) > 0 {
			headers := keysSorted(rows[0])
			if err := writer.Write(headers); err != nil {
				return nil, err
			}
			for _, row := range rows {
				rec := make([]string, len(headers))
				for i, h := range headers {
					rec[i] = stringify(row[h])
				}
				if err := writer.Write(rec); err != nil {
					return nil, err
				}
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return nil, err
		}
		return []byte(sb.String()), nil

	case agent.ModePlain:
		rows, ok := toRows(data)
		if !ok {
			return []byte(stringify(data) + "\n"), nil
		}
		var sb strings.Builder
		if len(rows) > 0 {
			headers := keysSorted(rows[0])
			for _, row := range rows {
				fields := make([]string, len(headers))
				for i, h := range headers {
					fields[i] = stringify(row[h])
				}
				sb.WriteString(strings.Join(fields, "\t"))
				sb.WriteByte('\n')
			}
		}
		return []byte(sb.String()), nil
	}
	return nil, exitcode.Errorf(exitcode.Usage, "unsupported output mode: %q", mode)
}

func deliver(w io.Writer, sink Sink, body []byte) error {
	switch sink.Kind {
	case SinkStdout:
		_, err := w.Write(body)
		return err
	case SinkFile:
		return writeFileAtomic(sink.Target, body)
	case SinkWebhook:
		return postWebhook(sink.Target, body)
	default:
		return exitcode.Errorf(exitcode.Usage, "unhandled sink kind")
	}
}

// writeFileAtomic writes via tmp + rename so partial writes never replace
// the target on failure.
func writeFileAtomic(path string, body []byte) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("opening temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("writing temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("closing temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("renaming %s to %s: %w", tmpPath, path, err)
	}
	return nil
}

func postWebhook(url string, body []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("building webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return exitcode.Errorf(exitcode.API, "webhook POST to %s failed: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		fmt.Fprintf(os.Stderr, "webhook %s returned HTTP %d\n", url, resp.StatusCode)
		return exitcode.Wrap(exitcode.FromHTTP(resp.StatusCode),
			errors.New("webhook delivery failed"))
	}
	return nil
}

// isTerminal returns true if w is *os.File pointing at a terminal.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// toRows flattens data into a slice of map[string]any. Returns false when
// the data shape isn't a row-table.
//
// Envelope-shape unwrap: if data is a map with a "results" key whose value is
// an array of objects, the array is used as the rows. This lets tabular modes
// "do the right thing" when handed an envelope without forcing every caller
// to remember to unwrap manually.
func toRows(data any) ([]map[string]any, bool) {
	if m, ok := data.(map[string]any); ok {
		if results, hasResults := m["results"]; hasResults {
			if arr, isArr := results.([]any); isArr {
				if rows, ok := arrayOfMaps(arr); ok {
					return rows, true
				}
			}
			if inner, isMap := results.(map[string]any); isMap {
				return []map[string]any{inner}, true
			}
		}
	}
	switch x := data.(type) {
	case map[string]any:
		return []map[string]any{x}, true
	case []any:
		return arrayOfMaps(x)
	}
	return nil, false
}

func arrayOfMaps(arr []any) ([]map[string]any, bool) {
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		out = append(out, m)
	}
	return out, true
}

func keysSorted(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool, int, int32, int64, float32, float64:
		return fmt.Sprintf("%v", x)
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprintf("%v", x)
		}
		return string(b)
	}
}
