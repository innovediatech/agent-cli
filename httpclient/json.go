package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
func (c *Client) DoJSON(ctx context.Context, method, path string, reqBody, respBody any) error {
	var body io.Reader
	if reqBody != nil {
		buf, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("httpclient: marshaling request body: %w", err)
		}
		body = bytes.NewReader(buf)
	}

	resp, err := c.Do(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer drainAndClose(resp)

	if resp.StatusCode >= 400 {
		return &APIError{
			Method:     method,
			URL:        resp.Request.URL.String(),
			StatusCode: resp.StatusCode,
			Body:       readBoundedBody(resp.Body, MaxErrorBodyBytes),
		}
	}

	if respBody == nil {
		return nil
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("httpclient: reading response body: %w", err)
	}
	raw = sanitizeJSONResponse(raw)
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, respBody); err != nil {
		return fmt.Errorf("httpclient: decoding response: %w", err)
	}
	return nil
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
