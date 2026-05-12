// Package exitcode defines the typed exit codes every Innovedia agent-native
// CLI uses. Agents branch on exit code without parsing output.
package exitcode

import (
	"errors"
	"fmt"
	"net/http"
)

const (
	Success     = 0
	Usage       = 2
	NotFound    = 3
	Auth        = 4
	API         = 5
	RateLimited = 7
	Config      = 10
)

// CodedError is the contract a caller can satisfy to control its own exit
// code. Useful for upstream packages that already have rich error types and
// don't want their errors classified by inference.
type CodedError interface {
	error
	ExitCode() int
}

type coded struct {
	error
	code int
}

func (c coded) ExitCode() int { return c.code }
func (c coded) Unwrap() error { return c.error }

// New wraps msg as a CodedError with the given code.
func New(code int, msg string) error {
	return coded{error: errors.New(msg), code: code}
}

// Errorf is the printf-shaped constructor.
func Errorf(code int, format string, args ...any) error {
	return coded{error: fmt.Errorf(format, args...), code: code}
}

// Wrap attaches a code to an existing error, preserving the chain.
func Wrap(code int, err error) error {
	if err == nil {
		return nil
	}
	return coded{error: err, code: code}
}

// FromHTTP picks the right exit code for an HTTP status. Used by HTTP clients
// that want to map upstream responses to typed exits without bespoke logic at
// every call site.
func FromHTTP(status int) int {
	switch {
	case status == 0:
		return API
	case status >= 200 && status < 300:
		return Success
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return Auth
	case status == http.StatusNotFound:
		return NotFound
	case status == http.StatusTooManyRequests:
		return RateLimited
	case status >= 400 && status < 500:
		return Usage
	case status >= 500:
		return API
	default:
		return API
	}
}

// Classify walks err's chain and returns the deepest explicit code. If no
// CodedError is found in the chain, returns API as the catch-all for
// unexpected upstream failures. nil → Success.
func Classify(err error) int {
	if err == nil {
		return Success
	}
	var c CodedError
	if errors.As(err, &c) {
		return c.ExitCode()
	}
	return API
}
