package httpclient

import (
	"net/http"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   time.Duration
		ok     bool
	}{
		{"missing", "", 0, false},
		{"zero", "0", 0, false},
		{"negative", "-5", 0, false},
		{"seconds", "5", 5 * time.Second, true},
		{"unparseable", "soon", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{Header: http.Header{}}
			if tc.header != "" {
				resp.Header.Set("Retry-After", tc.header)
			}
			got, ok := parseRetryAfter(resp)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Fatalf("got = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseRetryAfterDate(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	future := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)
	resp.Header.Set("Retry-After", future)
	got, ok := parseRetryAfter(resp)
	if !ok {
		t.Fatal("expected ok=true for future HTTP-date")
	}
	// HTTP-date has second precision; allow generous slack.
	if got < 25*time.Second || got > 35*time.Second {
		t.Errorf("got = %v, want ~30s", got)
	}
}

func TestParseRetryAfterPastDate(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	past := time.Now().Add(-5 * time.Second).UTC().Format(http.TimeFormat)
	resp.Header.Set("Retry-After", past)
	_, ok := parseRetryAfter(resp)
	if ok {
		t.Error("past HTTP-date should not return ok=true")
	}
}

func TestRetryPolicyDefaults(t *testing.T) {
	p := RetryPolicy{}.withDefaults()
	if p.MaxAttempts != DefaultRetryPolicy.MaxAttempts {
		t.Errorf("MaxAttempts = %d", p.MaxAttempts)
	}
	if p.BaseBackoff != DefaultRetryPolicy.BaseBackoff {
		t.Errorf("BaseBackoff = %v", p.BaseBackoff)
	}
	if p.MaxBackoff != DefaultRetryPolicy.MaxBackoff {
		t.Errorf("MaxBackoff = %v", p.MaxBackoff)
	}
}

func TestDefaultClassifier(t *testing.T) {
	cases := []struct {
		name   string
		status int
		err    error
		want   Decision
	}{
		{"2xx", 200, nil, Accept},
		{"204", 204, nil, Accept},
		{"4xx", 400, nil, Accept},
		{"404", 404, nil, Accept},
		{"429", 429, nil, RetryAfterHeader},
		{"500", 500, nil, Retry},
		{"503", 503, nil, Retry},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var resp *http.Response
			if tc.status != 0 {
				resp = &http.Response{StatusCode: tc.status}
			}
			got := DefaultClassifier(resp, tc.err)
			if got != tc.want {
				t.Errorf("got = %d, want %d", got, tc.want)
			}
		})
	}
}
