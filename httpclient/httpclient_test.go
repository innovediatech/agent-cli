package httpclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/innovediatech/agent-cli/exitcode"
	"github.com/innovediatech/agent-cli/httpclient"
)

// fastPolicy keeps test runtimes bounded.
var fastPolicy = httpclient.RetryPolicy{
	MaxAttempts: 3,
	BaseBackoff: 1 * time.Millisecond,
	MaxBackoff:  5 * time.Millisecond,
}

func newClient(t *testing.T, srv *httptest.Server, mutate ...func(*httpclient.Config)) *httpclient.Client {
	t.Helper()
	cfg := httpclient.Config{
		BaseURL: srv.URL,
		Retry:   fastPolicy,
	}
	for _, m := range mutate {
		m(&cfg)
	}
	c, err := httpclient.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestDoSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	resp, err := c.Do(context.Background(), "GET", "/whatever", nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestDoHeadersAndUserAgent(t *testing.T) {
	var gotUA, gotX string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotX = r.Header.Get("X-Innovedia")
		w.WriteHeader(204)
	}))
	defer srv.Close()

	c := newClient(t, srv, func(cfg *httpclient.Config) {
		cfg.Headers = http.Header{"X-Innovedia": []string{"yes"}}
		cfg.UserAgent = "test-ua/1.0"
	})
	resp, err := c.Do(context.Background(), "GET", "/", nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	if gotUA != "test-ua/1.0" {
		t.Errorf("UA = %q, want %q", gotUA, "test-ua/1.0")
	}
	if gotX != "yes" {
		t.Errorf("X-Innovedia = %q, want %q", gotX, "yes")
	}
}

func TestDoSuppressUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(204)
	}))
	defer srv.Close()

	c := newClient(t, srv, func(cfg *httpclient.Config) {
		cfg.UserAgent = "-"
	})
	resp, err := c.Do(context.Background(), "GET", "/", nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	// Go's net/http sends an empty UA when none is set; the client adds
	// its own only when "-" is not specified. Empty is what we expect.
	if gotUA != "" {
		t.Errorf("UA = %q, want empty (suppressed)", gotUA)
	}
}

func TestRequestHookOverrides(t *testing.T) {
	var gotAuth, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(204)
	}))
	defer srv.Close()

	c := newClient(t, srv, func(cfg *httpclient.Config) {
		cfg.UserAgent = "default-ua"
		cfg.RequestHook = func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer abc123")
			req.Header.Set("User-Agent", "hook-override")
			return nil
		}
	})
	resp, err := c.Do(context.Background(), "GET", "/", nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	if gotAuth != "Bearer abc123" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotUA != "hook-override" {
		t.Errorf("UA = %q, want hook-override", gotUA)
	}
}

func TestRequestHookErrorAborts(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	hookErr := errors.New("hook failed")
	c := newClient(t, srv, func(cfg *httpclient.Config) {
		cfg.RequestHook = func(_ context.Context, _ *http.Request) error { return hookErr }
	})
	_, err := c.Do(context.Background(), "GET", "/", nil)
	if !errors.Is(err, hookErr) {
		t.Fatalf("err = %v, want hookErr", err)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("server hit %d times, want 0", got)
	}
}

func TestRetryOn5xx(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n < 3 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
		fmt.Fprintln(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	resp, err := c.Do(context.Background(), "GET", "/", nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestNoRetryOn4xx(t *testing.T) {
	// Do follows net/http semantics: non-2xx responses are returned as-is.
	// Only DoJSON converts them to APIError. Verify both surfaces here.
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(400)
		fmt.Fprintln(w, "bad request")
	}))
	defer srv.Close()

	c := newClient(t, srv)
	resp, err := c.Do(context.Background(), "GET", "/", nil)
	if err != nil {
		t.Fatalf("Do err = %v, want nil for non-2xx", err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on 4xx)", got)
	}
}

func TestRetryOn429HonorsRetryAfter(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n < 2 {
			w.Header().Set("Retry-After", "0") // unparseable as positive; we fall back to policy
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	var limiterRL, limiterOK int32
	c := newClient(t, srv, func(cfg *httpclient.Config) {
		cfg.Limiter = &countingLimiter{onRL: &limiterRL, onOK: &limiterOK}
	})
	resp, err := c.Do(context.Background(), "GET", "/", nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
	if atomic.LoadInt32(&limiterRL) != 1 {
		t.Errorf("OnRateLimit calls = %d, want 1", limiterRL)
	}
	if atomic.LoadInt32(&limiterOK) != 1 {
		t.Errorf("OnSuccess calls = %d, want 1", limiterOK)
	}
}

func TestRetryAfterHeaderRespected(t *testing.T) {
	var attempts []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts = append(attempts, time.Now())
		if len(attempts) == 1 {
			w.Header().Set("Retry-After", "1") // 1 second — but MaxBackoff caps at 5ms
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	start := time.Now()
	resp, err := c.Do(context.Background(), "GET", "/", nil)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	// MaxBackoff caps the Retry-After value, so the elapsed time should
	// be well under the 1s the header asked for.
	if elapsed > 500*time.Millisecond {
		t.Errorf("elapsed = %v, MaxBackoff cap should have kept it short", elapsed)
	}
}

func TestRetriesExhausted(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(503)
		fmt.Fprintln(w, "still broken")
	}))
	defer srv.Close()

	c := newClient(t, srv)
	_, err := c.Do(context.Background(), "GET", "/", nil)
	if err == nil {
		t.Fatal("want error")
	}
	var apiErr *httpclient.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want APIError, got %T", err)
	}
	if apiErr.StatusCode != 503 {
		t.Errorf("status = %d", apiErr.StatusCode)
	}
	if got := atomic.LoadInt32(&hits); got != int32(fastPolicy.MaxAttempts) {
		t.Errorf("attempts = %d, want %d", got, fastPolicy.MaxAttempts)
	}
	if got := exitcode.Classify(err); got != exitcode.API {
		t.Errorf("exit code = %d, want %d (API)", got, exitcode.API)
	}
}

func TestNetworkErrorRetries(t *testing.T) {
	// Server we close immediately so dialing fails consistently.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	c := newClient(t, srv)
	_, err := c.Do(context.Background(), "GET", "/", nil)
	if err == nil {
		t.Fatal("want error")
	}
	var apiErr *httpclient.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want APIError, got %T", err)
	}
	if apiErr.StatusCode != 0 {
		t.Errorf("status = %d, want 0 (network error)", apiErr.StatusCode)
	}
	if apiErr.Err == nil {
		t.Error("APIError.Err should be non-nil for network errors")
	}
	if got := exitcode.Classify(err); got != exitcode.API {
		t.Errorf("exit code = %d, want %d (API)", got, exitcode.API)
	}
}

func TestCustomClassifier401Retry(t *testing.T) {
	// Simulates the authclient pattern: first attempt sees 401,
	// caller's classifier triggers a re-auth retry, second attempt
	// succeeds.
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n == 1 {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	var reLogged int32
	classifier := func(resp *http.Response, err error) httpclient.Decision {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			atomic.AddInt32(&reLogged, 1)
			return httpclient.Retry
		}
		return httpclient.DefaultClassifier(resp, err)
	}

	c := newClient(t, srv, func(cfg *httpclient.Config) {
		cfg.Classifier = classifier
	})
	resp, err := c.Do(context.Background(), "GET", "/", nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	if atomic.LoadInt32(&reLogged) != 1 {
		t.Errorf("reLogged = %d, want 1", reLogged)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
}

func TestBodyReplayedOnRetry(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if len(bodies) < 2 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	resp, err := c.Do(context.Background(), "POST", "/", strings.NewReader(`{"x":1}`))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	if len(bodies) != 2 {
		t.Fatalf("got %d attempts, want 2", len(bodies))
	}
	for i, body := range bodies {
		if body != `{"x":1}` {
			t.Errorf("attempt %d body = %q, want %q", i, body, `{"x":1}`)
		}
	}
}

func TestContextCancelInBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c, err := httpclient.New(httpclient.Config{
		BaseURL: srv.URL,
		Retry: httpclient.RetryPolicy{
			MaxAttempts: 5,
			BaseBackoff: 50 * time.Millisecond,
			MaxBackoff:  100 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = c.Do(ctx, "GET", "/", nil)
	if err == nil {
		t.Fatal("want ctx error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
}

func TestAbsolutePathBypassesBaseURL(t *testing.T) {
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srvB.Close()

	c := newClient(t, srvA)
	resp, err := c.Do(context.Background(), "GET", srvB.URL+"/anything", nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200 (should hit srvB, not srvA)", resp.StatusCode)
	}
}

func TestRelativePathNoBaseURLErrors(t *testing.T) {
	c, err := httpclient.New(httpclient.Config{Retry: fastPolicy})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Do(context.Background(), "GET", "/x", nil)
	if err == nil {
		t.Fatal("want error for relative path with no BaseURL")
	}
}

func TestDoJSONRoundtrip(t *testing.T) {
	type req struct {
		Name string `json:"name"`
	}
	type resp struct {
		Greeting string `json:"greeting"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		var in req
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Fatalf("decode: %v", err)
		}
		_ = json.NewEncoder(w).Encode(resp{Greeting: "hi " + in.Name})
	}))
	defer srv.Close()

	c := newClient(t, srv)
	var out resp
	if err := c.DoJSON(context.Background(), "POST", "/greet", req{Name: "jayson"}, &out); err != nil {
		t.Fatalf("DoJSON: %v", err)
	}
	if out.Greeting != "hi jayson" {
		t.Errorf("greeting = %q", out.Greeting)
	}
}

func TestDoJSONSanitizesXSSIPrefix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, ")]}'\n"+`{"ok":true}`)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	var out struct {
		Ok bool `json:"ok"`
	}
	if err := c.DoJSON(context.Background(), "GET", "/", nil, &out); err != nil {
		t.Fatalf("DoJSON: %v", err)
	}
	if !out.Ok {
		t.Error("ok = false, want true")
	}
}

func TestDoJSONNilRespBodyDrains(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"ignored":true}`)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	if err := c.DoJSON(context.Background(), "GET", "/", nil, nil); err != nil {
		t.Fatalf("DoJSON: %v", err)
	}
}

func TestDoJSONErrorReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"error":"missing"}`)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	var out map[string]any
	err := c.DoJSON(context.Background(), "GET", "/x", nil, &out)
	if err == nil {
		t.Fatal("want error")
	}
	var apiErr *httpclient.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want APIError, got %T", err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("status = %d", apiErr.StatusCode)
	}
	if got := exitcode.Classify(err); got != exitcode.NotFound {
		t.Errorf("exit code = %d, want %d (NotFound)", got, exitcode.NotFound)
	}
}

func TestNilContext(t *testing.T) {
	c, err := httpclient.New(httpclient.Config{BaseURL: "http://x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	//nolint:staticcheck // testing nil-context guard intentionally
	_, err = c.Do(nil, "GET", "/", nil)
	if err == nil {
		t.Fatal("want error for nil context")
	}
}

// Retry-After date-form parsing is exercised in the internal parseRetryAfter
// test (retry_internal_test.go) where we can read the helper directly without
// the second-precision wall-clock rounding that makes a black-box test flaky.

// countingLimiter is a minimal Limiter that records callback counts. It
// never blocks in Wait so test runtimes stay bounded.
type countingLimiter struct {
	onWait *int32
	onOK   *int32
	onRL   *int32
}

func (l *countingLimiter) Wait(_ context.Context) error {
	if l.onWait != nil {
		atomic.AddInt32(l.onWait, 1)
	}
	return nil
}
func (l *countingLimiter) OnSuccess() {
	if l.onOK != nil {
		atomic.AddInt32(l.onOK, 1)
	}
}
func (l *countingLimiter) OnRateLimit() {
	if l.onRL != nil {
		atomic.AddInt32(l.onRL, 1)
	}
}

// silence unused warnings when adding new tests.
var _ = strconv.Itoa
