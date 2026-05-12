package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"

	"github.com/innovediatech/agent-cli/agent"
	"github.com/innovediatech/agent-cli/exitcode"
	"github.com/innovediatech/agent-cli/httpclient"
)

func newFlags(t *testing.T, args ...string) *agent.Flags {
	t.Helper()
	cmd := &cobra.Command{Use: "test"}
	f := agent.Bind(cmd)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestParseSink(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
		want    Sink
	}{
		{"", false, Sink{Kind: SinkStdout}},
		{"stdout", false, Sink{Kind: SinkStdout}},
		{"file:/tmp/x", false, Sink{Kind: SinkFile, Target: "/tmp/x"}},
		{"webhook:https://example.com/x", false, Sink{Kind: SinkWebhook, Target: "https://example.com/x"}},
		{"webhook:not-a-url", true, Sink{}},
		{"file:", true, Sink{}},
		{"junk", true, Sink{}},
	}
	for _, tc := range cases {
		got, err := ParseSink(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseSink(%q) expected error, got nil", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSink(%q) unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseSink(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestWriteJSONStdout(t *testing.T) {
	var buf bytes.Buffer
	flags := newFlags(t, "--agent")
	data := map[string]any{"id": 1, "name": "alice"}
	if err := Write(&buf, flags, data, nil); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, buf.String())
	}
	if got["name"] != "alice" {
		t.Errorf("expected name=alice, got %v", got)
	}
}

func TestWriteCompactSingleLine(t *testing.T) {
	var buf bytes.Buffer
	flags := newFlags(t, "--json", "--compact")
	data := map[string]any{"id": 1, "name": "alice"}
	if err := Write(&buf, flags, data, nil); err != nil {
		t.Fatal(err)
	}
	// One newline at the end, no indentation.
	s := strings.TrimRight(buf.String(), "\n")
	if strings.Contains(s, "\n") {
		t.Errorf("compact mode should produce single line, got %q", buf.String())
	}
}

func TestWriteSelectProjection(t *testing.T) {
	var buf bytes.Buffer
	flags := newFlags(t, "--agent", "--select", "id")
	data := map[string]any{"id": 1, "name": "alice", "extra": "x"}
	if err := Write(&buf, flags, data, nil); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(buf.Bytes(), &got)
	if _, hasName := got["name"]; hasName {
		t.Errorf("--select id should drop name; got %v", got)
	}
	if _, hasID := got["id"]; !hasID {
		t.Errorf("--select id should keep id; got %v", got)
	}
}

func TestWriteCSV(t *testing.T) {
	var buf bytes.Buffer
	flags := newFlags(t, "--csv")
	data := []any{
		map[string]any{"id": 1, "name": "a"},
		map[string]any{"id": 2, "name": "b"},
	}
	if err := Write(&buf, flags, data, nil); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "id,name") {
		t.Errorf("expected CSV header id,name; got %q", got)
	}
	if !strings.Contains(got, "1,a") || !strings.Contains(got, "2,b") {
		t.Errorf("expected CSV rows; got %q", got)
	}
}

func TestWritePlainTabs(t *testing.T) {
	var buf bytes.Buffer
	flags := newFlags(t, "--plain")
	data := []any{
		map[string]any{"id": 1, "name": "a"},
	}
	if err := Write(&buf, flags, data, nil); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "1\ta\n") {
		t.Errorf("expected tab-separated row; got %q", got)
	}
}

func TestDeliverFileAtomic(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.json")

	var nullSink bytes.Buffer
	flags := newFlags(t, "--agent", "--deliver", "file:"+target)
	data := map[string]any{"id": 1, "name": "alice"}
	if err := Write(&nullSink, flags, data, nil); err != nil {
		t.Fatal(err)
	}
	if nullSink.Len() != 0 {
		t.Errorf("with --deliver file:, the io.Writer should not receive body; got %q", nullSink.String())
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "alice") {
		t.Errorf("file body missing data: %s", body)
	}
}

func TestDeliverWebhook(t *testing.T) {
	var received string
	var receivedCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := readAll(r)
		received = b
		receivedCT = r.Header.Get("Content-Type")
		w.WriteHeader(204)
	}))
	defer srv.Close()

	var sink bytes.Buffer
	flags := newFlags(t, "--agent", "--deliver", "webhook:"+srv.URL)
	data := map[string]any{"id": 1}
	if err := Write(&sink, flags, data, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(received, `"id":1`) {
		t.Errorf("webhook body missing data: %s", received)
	}
	if receivedCT != "application/json" {
		t.Errorf("expected application/json, got %q", receivedCT)
	}
}

func TestDeliverWebhookRetriesOn5xx(t *testing.T) {
	var hits int32
	var received string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n == 1 {
			w.WriteHeader(503)
			return
		}
		b, _ := readAll(r)
		received = b
		w.WriteHeader(204)
	}))
	defer srv.Close()

	var sink bytes.Buffer
	flags := newFlags(t, "--agent", "--deliver", "webhook:"+srv.URL)
	if err := Write(&sink, flags, map[string]any{"id": 1}, nil); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("attempts = %d, want 2 (one 5xx retry then success)", got)
	}
	if !strings.Contains(received, `"id":1`) {
		t.Errorf("body not delivered on retry: %q", received)
	}
}

func TestDeliverWebhookExhaustsRetriesReturnsAPIError(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":"server fire"}`))
	}))
	defer srv.Close()

	var sink bytes.Buffer
	flags := newFlags(t, "--agent", "--deliver", "webhook:"+srv.URL)
	err := Write(&sink, flags, map[string]any{"id": 1}, nil)
	if err == nil {
		t.Fatal("expected error after exhausted retries, got nil")
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Errorf("attempts = %d, want 3 (DefaultRetryPolicy.MaxAttempts)", got)
	}
	var apiErr *httpclient.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err type = %T, want *httpclient.APIError; err=%v", err, err)
	}
	if apiErr.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", apiErr.StatusCode)
	}
	if !strings.Contains(string(apiErr.Body), "server fire") {
		t.Errorf("Body did not capture upstream payload: %q", apiErr.Body)
	}
	if got := exitcode.Classify(err); got != exitcode.FromHTTP(500) {
		t.Errorf("exitcode.Classify = %d, want %d (FromHTTP 500)", got, exitcode.FromHTTP(500))
	}
}

func TestDeliverWebhookNoRetryOn4xx(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(400)
	}))
	defer srv.Close()

	var sink bytes.Buffer
	flags := newFlags(t, "--agent", "--deliver", "webhook:"+srv.URL)
	err := Write(&sink, flags, map[string]any{"id": 1}, nil)
	if err == nil {
		t.Fatal("expected error on 4xx, got nil")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("attempts = %d, want 1 (4xx must not retry)", got)
	}
	var apiErr *httpclient.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err type = %T, want *httpclient.APIError", err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", apiErr.StatusCode)
	}
}

func readAll(r *http.Request) (string, error) {
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(r.Body); err != nil {
		return "", err
	}
	return buf.String(), nil
}
