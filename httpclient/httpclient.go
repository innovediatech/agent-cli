// Package httpclient is the shared HTTP transport for Innovedia CLIs.
//
// It wraps net/http.Client with retry + backoff, structured error
// classification (composing with package exitcode), header injection, and
// a request/response hook surface that callers use to layer auth,
// observability, or per-call mutation without rebuilding the transport.
//
// The client is intentionally stateless beyond the underlying http.Client:
// auth tokens, on-disk caches, dry-run framing, and output projection all
// stay in the caller. The two consumer shapes that drove this design are
// platform-cli/internal/authclient (auth lifecycle + 401-retry) and the
// Printing Press petstore client (multi-attempt backoff + adaptive rate
// limiting). Both compose cleanly on top: authclient becomes a thin hook
// pair; petstore's adaptive limiter satisfies the Limiter interface.
package httpclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultTimeout is applied when Config.Timeout is zero and the caller did
// not supply an HTTPClient. Matches platform-cli's authclient.
const DefaultTimeout = 30 * time.Second

// DefaultUserAgent is set when Config.UserAgent is empty.
const DefaultUserAgent = "innovedia-agent-cli/0.1"

// Client executes HTTP requests with retry, classification, and header
// defaults. Construct with New. Safe for concurrent use.
type Client struct {
	hc          *http.Client
	base        string
	headers     http.Header
	userAgent   string
	policy      RetryPolicy
	requestHook func(context.Context, *http.Request) error
	classifier  func(*http.Response, error) Decision
	limiter     Limiter
	rng         *rand.Rand
}

// Config holds construction parameters. All fields are optional except
// when noted; zero values resolve to documented defaults.
type Config struct {
	// BaseURL is prepended to any relative path passed to Do. Absolute
	// paths (those starting with "http://" or "https://") bypass it.
	BaseURL string
	// Timeout applied to the underlying http.Client when HTTPClient is nil.
	// Defaults to DefaultTimeout.
	Timeout time.Duration
	// HTTPClient overrides the default transport. When non-nil, Timeout is
	// ignored; the caller owns timeout configuration on the supplied client.
	HTTPClient *http.Client
	// Headers are injected on every request before the RequestHook runs,
	// so the hook can override them.
	Headers http.Header
	// UserAgent sets the User-Agent header when not already present on a
	// request. Defaults to DefaultUserAgent. Set to "-" to suppress.
	UserAgent string
	// Retry controls backoff + max-attempts. Zero value resolves to
	// DefaultRetryPolicy.
	Retry RetryPolicy
	// RequestHook runs after Headers + UserAgent have been applied and
	// before the request is sent. Use it to inject auth, signing, or
	// per-request headers. Returning an error aborts the attempt and is
	// surfaced to the caller without retry.
	RequestHook func(context.Context, *http.Request) error
	// Classifier decides whether a response/error should be retried.
	// Zero value resolves to DefaultClassifier. Wrap DefaultClassifier
	// to add auth-style behavior (e.g. 401 → re-login → Retry).
	Classifier func(*http.Response, error) Decision
	// Limiter, when non-nil, paces requests via Wait before each attempt
	// and receives success/rate-limit feedback. Nil disables pacing.
	Limiter Limiter
}

// New constructs a Client. Returns an error only for invalid construction
// input (currently: malformed BaseURL).
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL != "" {
		if _, err := url.Parse(cfg.BaseURL); err != nil {
			return nil, fmt.Errorf("httpclient: invalid BaseURL %q: %w", cfg.BaseURL, err)
		}
	}
	hc := cfg.HTTPClient
	if hc == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = DefaultTimeout
		}
		hc = &http.Client{Timeout: timeout}
	}
	ua := cfg.UserAgent
	if ua == "" {
		ua = DefaultUserAgent
	}
	policy := cfg.Retry.withDefaults()
	classifier := cfg.Classifier
	if classifier == nil {
		classifier = DefaultClassifier
	}

	return &Client{
		hc:          hc,
		base:        strings.TrimRight(cfg.BaseURL, "/"),
		headers:     cfg.Headers.Clone(),
		userAgent:   ua,
		policy:      policy,
		requestHook: cfg.RequestHook,
		classifier:  classifier,
		limiter:     cfg.Limiter,
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
	}, nil
}

// Do executes method+path with the optional body, applying retry policy,
// header injection, and the request/response hooks.
//
// Do follows net/http.Client.Do semantics: a non-nil error means a
// transport-level failure (or the classifier's Retry attempts exhausted
// without producing a response). Non-2xx HTTP responses are returned as
// real responses with no error — the caller is responsible for checking
// resp.StatusCode and closing resp.Body. Use DoJSON for the opinionated
// flow that converts non-2xx into a typed APIError.
//
// body, when non-nil, is read into memory once so it can be replayed
// across retries. Streams larger than memory should be sent through a
// caller-owned http.Request and c.hc directly.
func (c *Client) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	if ctx == nil {
		return nil, errors.New("httpclient: nil context")
	}

	var bodyBytes []byte
	if body != nil {
		buf, err := io.ReadAll(body)
		if err != nil {
			return nil, fmt.Errorf("httpclient: reading request body: %w", err)
		}
		bodyBytes = buf
	}

	target, err := c.resolveURL(path)
	if err != nil {
		return nil, err
	}

	var (
		lastStatus int
		lastBody   []byte
		lastErr    error
	)

	for attempt := 0; attempt < c.policy.MaxAttempts; attempt++ {
		if c.limiter != nil {
			if err := c.limiter.Wait(ctx); err != nil {
				return nil, err
			}
		}

		req, err := c.newRequest(ctx, method, target, bodyBytes)
		if err != nil {
			return nil, err
		}
		if c.requestHook != nil {
			if err := c.requestHook(ctx, req); err != nil {
				return nil, err
			}
		}

		resp, err := c.hc.Do(req)
		decision := c.classifier(resp, err)

		if decision == Accept {
			if err != nil {
				return nil, &APIError{Method: method, URL: target, Err: err}
			}
			if resp.StatusCode < 400 && c.limiter != nil {
				c.limiter.OnSuccess()
			}
			return resp, nil
		}

		// Decision is Retry or RetryAfterHeader. Capture state for the
		// terminal-wrap path, then drain so the connection can be reused.
		if resp != nil {
			if resp.StatusCode == http.StatusTooManyRequests && c.limiter != nil {
				c.limiter.OnRateLimit()
			}
			lastStatus = resp.StatusCode
			lastBody = readBoundedBody(resp.Body, MaxErrorBodyBytes)
		}
		lastErr = err

		if attempt+1 >= c.policy.MaxAttempts {
			drainAndClose(resp)
			break
		}

		// backoff reads resp.Header for Retry-After; safe to call before drain.
		wait := c.backoff(attempt, decision, resp)
		drainAndClose(resp)
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}

	return nil, &APIError{
		Method:     method,
		URL:        target,
		StatusCode: lastStatus,
		Body:       lastBody,
		Err:        lastErr,
	}
}

func (c *Client) resolveURL(path string) (string, error) {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path, nil
	}
	if c.base == "" {
		return "", fmt.Errorf("httpclient: relative path %q with no BaseURL", path)
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.base + path, nil
}

func (c *Client) newRequest(ctx context.Context, method, target string, bodyBytes []byte) (*http.Request, error) {
	var body io.Reader
	if bodyBytes != nil {
		body = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, fmt.Errorf("httpclient: building request: %w", err)
	}
	for k, vs := range c.headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	switch {
	case c.userAgent == "-":
		// Suppress Go's default User-Agent by setting an explicit empty
		// header. net/http only injects its default when the key is
		// absent, so an empty Set wins.
		req.Header.Set("User-Agent", "")
	case req.Header.Get("User-Agent") == "":
		req.Header.Set("User-Agent", c.userAgent)
	}
	if bodyBytes != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func readBoundedBody(body io.Reader, max int) []byte {
	if body == nil {
		return nil
	}
	buf, _ := io.ReadAll(io.LimitReader(body, int64(max)))
	return buf
}
