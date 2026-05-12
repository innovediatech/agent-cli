package httpclient

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Decision is the classifier's verdict on a single attempt.
type Decision int

const (
	// Accept treats the response/error as final. The client returns it
	// to the caller without further retries.
	Accept Decision = iota
	// Retry schedules another attempt with the policy's backoff.
	Retry
	// RetryAfterHeader retries honoring the upstream Retry-After header,
	// falling back to the policy backoff when the header is missing.
	RetryAfterHeader
)

// RetryPolicy controls retry pacing. Zero value resolves to
// DefaultRetryPolicy via withDefaults.
type RetryPolicy struct {
	// MaxAttempts is the total number of attempts (initial + retries).
	// Default 3.
	MaxAttempts int
	// BaseBackoff is the first retry wait. Effective wait is
	// BaseBackoff * 2^attempt up to MaxBackoff. Default 1s.
	BaseBackoff time.Duration
	// MaxBackoff caps the exponential backoff. Default 30s.
	MaxBackoff time.Duration
	// Jitter, when true, applies ±25% uniform jitter to each backoff so
	// retries from many concurrent CLIs don't synchronize.
	Jitter bool
}

// DefaultRetryPolicy is the policy applied when Config.Retry is zero.
var DefaultRetryPolicy = RetryPolicy{
	MaxAttempts: 3,
	BaseBackoff: 1 * time.Second,
	MaxBackoff:  30 * time.Second,
	Jitter:      false,
}

func (p RetryPolicy) withDefaults() RetryPolicy {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = DefaultRetryPolicy.MaxAttempts
	}
	if p.BaseBackoff <= 0 {
		p.BaseBackoff = DefaultRetryPolicy.BaseBackoff
	}
	if p.MaxBackoff <= 0 {
		p.MaxBackoff = DefaultRetryPolicy.MaxBackoff
	}
	return p
}

// DefaultClassifier retries on network errors, 429, and 5xx; accepts
// everything else (including 4xx other than 429). Wrap it to layer in
// auth-style behavior:
//
//	cfg.Classifier = func(resp *http.Response, err error) httpclient.Decision {
//	    if resp != nil && resp.StatusCode == http.StatusUnauthorized {
//	        clearMyTokenCache()
//	        return httpclient.Retry
//	    }
//	    return httpclient.DefaultClassifier(resp, err)
//	}
func DefaultClassifier(resp *http.Response, err error) Decision {
	if err != nil {
		return Retry
	}
	if resp == nil {
		return Accept
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return RetryAfterHeader
	case resp.StatusCode >= 500:
		return Retry
	default:
		return Accept
	}
}

// backoff returns the wait duration before the next attempt. RetryAfterHeader
// honors the Retry-After header on resp; everything else uses the policy's
// exponential backoff.
func (c *Client) backoff(attempt int, decision Decision, resp *http.Response) time.Duration {
	if decision == RetryAfterHeader {
		if wait, ok := parseRetryAfter(resp); ok {
			if wait > c.policy.MaxBackoff {
				return c.policy.MaxBackoff
			}
			return wait
		}
	}
	wait := time.Duration(float64(c.policy.BaseBackoff) * math.Pow(2, float64(attempt)))
	if wait > c.policy.MaxBackoff {
		wait = c.policy.MaxBackoff
	}
	if c.policy.Jitter {
		// ±25% — multiplicative so small backoffs jitter proportionally.
		factor := 0.75 + 0.5*c.rng.Float64()
		wait = time.Duration(float64(wait) * factor)
	}
	return wait
}

// parseRetryAfter handles delta-seconds and HTTP-date forms of the
// Retry-After header. Returns ok=false when the header is missing or
// unparseable so the caller can fall back to its policy.
func parseRetryAfter(resp *http.Response) (time.Duration, bool) {
	if resp == nil {
		return 0, false
	}
	header := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if header == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(header, 10, 64); err == nil {
		if seconds <= 0 {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	if t, err := http.ParseTime(header); err == nil {
		wait := time.Until(t)
		if wait <= 0 {
			return 0, false
		}
		return wait, true
	}
	return 0, false
}
