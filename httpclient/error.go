package httpclient

import (
	"bytes"
	"fmt"

	"github.com/innovediatech/agent-cli/exitcode"
)

// MaxErrorBodyBytes caps how much of a non-2xx response body is captured
// into APIError.Body. Big upstream error pages don't help diagnosis past
// a few KB and balloon log lines.
const MaxErrorBodyBytes = 4096

// APIError is the structured error returned by Do for terminal failures.
// It implements exitcode.CodedError so exitcode.Classify resolves it
// to a typed exit code without per-CLI mapping.
type APIError struct {
	Method     string
	URL        string
	StatusCode int    // 0 when Err is non-nil and the request never produced a response
	Body       []byte // truncated to MaxErrorBodyBytes; empty when Err is non-nil
	Err        error  // underlying network/transport error, if any
}

// Error implements error.
func (e *APIError) Error() string {
	if e == nil {
		return "<nil APIError>"
	}
	if e.StatusCode == 0 && e.Err != nil {
		return fmt.Sprintf("%s %s: %v", e.Method, e.URL, e.Err)
	}
	if len(e.Body) > 0 {
		return fmt.Sprintf("%s %s returned HTTP %d: %s", e.Method, e.URL, e.StatusCode, truncatedString(e.Body))
	}
	return fmt.Sprintf("%s %s returned HTTP %d", e.Method, e.URL, e.StatusCode)
}

// Unwrap exposes the underlying transport error to errors.Is / errors.As.
func (e *APIError) Unwrap() error { return e.Err }

// ExitCode maps the APIError to a typed exit code. Network errors map to
// exitcode.API; HTTP status codes route through exitcode.FromHTTP.
func (e *APIError) ExitCode() int {
	if e == nil {
		return exitcode.API
	}
	if e.StatusCode == 0 {
		return exitcode.API
	}
	return exitcode.FromHTTP(e.StatusCode)
}

func truncatedString(b []byte) string {
	const display = 200
	if len(b) <= display {
		return string(b)
	}
	return string(b[:display]) + "..."
}

// sanitizeJSONResponse strips known JSONP/XSSI guard prefixes and the
// UTF-8 BOM. For clean JSON the function is a no-op. Lifted from the
// Printing Press petstore client where it has shipped against real
// upstream APIs (Slack, Google, etc.).
func sanitizeJSONResponse(body []byte) []byte {
	body = bytes.TrimPrefix(body, []byte("\xEF\xBB\xBF"))
	prefixes := [][]byte{
		[]byte(")]}'\n"),
		[]byte(")]}'"),
		[]byte("{}&&"),
		[]byte("for(;;);"),
		[]byte("while(1);"),
	}
	for _, p := range prefixes {
		if bytes.HasPrefix(body, p) {
			body = bytes.TrimPrefix(body, p)
			body = bytes.TrimLeft(body, " \t\r\n")
			break
		}
	}
	return body
}
