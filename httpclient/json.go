package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// DoJSON is the ergonomic JSON wrapper around Do. reqBody, when non-nil,
// is marshaled to JSON and sent with Content-Type: application/json (the
// underlying Do sets the header when a body is present). respBody, when
// non-nil, receives the decoded response.
//
// The response body is sanitized for known XSSI/BOM prefixes before
// decoding so brittle upstreams don't trip json.Decode.
//
// A nil respBody is supported (for endpoints that return 204 No Content or
// when the caller intentionally discards the body); the response is still
// drained so the connection can be reused.
//
// Response headers are discarded. Use DoJSONHeaders when the caller needs
// to inspect Link / X-Request-ID / etc.
func (c *Client) DoJSON(ctx context.Context, method, path string, reqBody, respBody any) error {
	_, err := c.DoJSONHeaders(ctx, method, path, reqBody, respBody)
	return err
}

// DoJSONHeaders behaves like DoJSON but also returns the response headers,
// which is the escape hatch for APIs that put pagination cursors, request
// IDs, rate-limit metadata, or other essential signals in HTTP headers
// rather than the response body.
//
// Headers are returned even when respBody is nil or empty, so callers can
// always inspect them. On a transport error or 4xx/5xx (returned as
// *APIError), the returned http.Header is nil.
func (c *Client) DoJSONHeaders(ctx context.Context, method, path string, reqBody, respBody any) (http.Header, error) {
	var body io.Reader
	if reqBody != nil {
		buf, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("httpclient: marshaling request body: %w", err)
		}
		body = bytes.NewReader(buf)
	}

	resp, err := c.Do(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	defer drainAndClose(resp)

	if resp.StatusCode >= 400 {
		return nil, &APIError{
			Method:     method,
			URL:        resp.Request.URL.String(),
			StatusCode: resp.StatusCode,
			Body:       readBoundedBody(resp.Body, MaxErrorBodyBytes),
		}
	}

	if respBody == nil {
		return resp.Header, nil
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.Header, fmt.Errorf("httpclient: reading response body: %w", err)
	}
	raw = sanitizeJSONResponse(raw)
	if len(bytes.TrimSpace(raw)) == 0 {
		return resp.Header, nil
	}
	if err := json.Unmarshal(raw, respBody); err != nil {
		return resp.Header, fmt.Errorf("httpclient: decoding response: %w", err)
	}
	return resp.Header, nil
}

// Limiter is the minimal rate-limiting interface. Implementations should
// be safe for concurrent use. A nil Limiter on Config disables pacing.
type Limiter interface {
	// Wait blocks until the next request is allowed or ctx is canceled.
	Wait(ctx context.Context) error
	// OnSuccess is called after a non-retried 2xx response.
	OnSuccess()
	// OnRateLimit is called when a 429 is observed (whether or not the
	// request will be retried).
	OnRateLimit()
}
