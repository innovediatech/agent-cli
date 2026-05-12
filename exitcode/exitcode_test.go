package exitcode

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestFromHTTP(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   int
	}{
		{"ok", 200, Success},
		{"created", 201, Success},
		{"no-content", 204, Success},
		{"bad-request", 400, Usage},
		{"unauthorized", 401, Auth},
		{"forbidden", 403, Auth},
		{"not-found", 404, NotFound},
		{"too-many", http.StatusTooManyRequests, RateLimited},
		{"server-err", 500, API},
		{"bad-gateway", 502, API},
		{"zero", 0, API},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FromHTTP(tc.status); got != tc.want {
				t.Fatalf("FromHTTP(%d) = %d, want %d", tc.status, got, tc.want)
			}
		})
	}
}

func TestClassifyNil(t *testing.T) {
	if got := Classify(nil); got != Success {
		t.Fatalf("Classify(nil) = %d, want %d", got, Success)
	}
}

func TestClassifyUnknown(t *testing.T) {
	err := errors.New("anything")
	if got := Classify(err); got != API {
		t.Fatalf("Classify(plain) = %d, want %d (API)", got, API)
	}
}

func TestClassifyExplicit(t *testing.T) {
	err := New(NotFound, "missing")
	if got := Classify(err); got != NotFound {
		t.Fatalf("Classify(NotFound) = %d, want %d", got, NotFound)
	}
}

func TestClassifyWrapped(t *testing.T) {
	root := New(Auth, "token expired")
	wrapped := fmt.Errorf("checking session: %w", root)
	doubleWrapped := fmt.Errorf("api call failed: %w", wrapped)
	if got := Classify(doubleWrapped); got != Auth {
		t.Fatalf("Classify(double-wrapped) = %d, want %d (Auth)", got, Auth)
	}
}

func TestWrapNil(t *testing.T) {
	if err := Wrap(NotFound, nil); err != nil {
		t.Fatalf("Wrap(NotFound, nil) should be nil, got %v", err)
	}
}

func TestErrorfPreservesFormat(t *testing.T) {
	err := Errorf(Config, "missing %s in %q", "API_KEY", "config.toml")
	want := `missing API_KEY in "config.toml"`
	if err.Error() != want {
		t.Fatalf("Errorf().Error() = %q, want %q", err.Error(), want)
	}
	if Classify(err) != Config {
		t.Fatalf("Classify(Errorf(Config, ...)) != Config")
	}
}
