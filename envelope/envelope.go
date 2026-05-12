// Package envelope defines the canonical response shape every Innovedia
// agent-native CLI uses: a Meta block (provenance) wrapping the data.
//
// Agents that parse output always know where to look:
//   - .results: the data
//   - .meta.source: "live" | "local"
//   - .meta.synced_at: when the local mirror was last refreshed
//   - .meta.reason: short hint about why the source was chosen (e.g.,
//     "live unavailable, fell back to local")
//   - .meta.request_id: upstream correlation id when available
//   - .meta.next_cursor: opaque cursor to pass back as --cursor for the next page
package envelope

import "time"

// Source classifies where Results came from.
type Source string

const (
	SourceLive  Source = "live"
	SourceLocal Source = "local"
)

// Meta is the provenance attached to every envelope.
type Meta struct {
	Source    Source     `json:"source"`
	SyncedAt  *time.Time `json:"synced_at,omitempty"`
	Reason    string     `json:"reason,omitempty"`
	RequestID string     `json:"request_id,omitempty"`
	// NextCursor is an opaque pagination token. When non-empty, agents can
	// re-issue the command with --cursor=<value> to fetch the next page.
	// Empty (omitted) means no further pages.
	NextCursor string `json:"next_cursor,omitempty"`
}

// Envelope wraps a result payload of any shape.
//
// We keep Results as `any` rather than a generic so envelopes flow through
// shared output / projection code without type acrobatics. Callers that want
// type safety can cast at the call site.
type Envelope struct {
	Meta    Meta `json:"meta"`
	Results any  `json:"results"`
}

// Live constructs an envelope for data fetched live.
func Live(results any) Envelope {
	return Envelope{
		Meta:    Meta{Source: SourceLive},
		Results: results,
	}
}

// Local constructs an envelope for data read from a local mirror.
//
// syncedAt should be the cursor timestamp of the last successful sync; pass
// zero time to omit. reason is a short, human-readable hint (e.g. "live API
// unreachable, served from local mirror").
func Local(results any, syncedAt time.Time, reason string) Envelope {
	env := Envelope{
		Meta:    Meta{Source: SourceLocal, Reason: reason},
		Results: results,
	}
	if !syncedAt.IsZero() {
		env.Meta.SyncedAt = &syncedAt
	}
	return env
}

// WithRequestID is a fluent setter for the upstream correlation id.
func (e Envelope) WithRequestID(id string) Envelope {
	e.Meta.RequestID = id
	return e
}

// WithReason is a fluent setter for the reason hint.
func (e Envelope) WithReason(reason string) Envelope {
	e.Meta.Reason = reason
	return e
}

// WithNextCursor is a fluent setter for the pagination cursor. Pass empty
// string to indicate no next page (the field is omitted in JSON either way).
func (e Envelope) WithNextCursor(cursor string) Envelope {
	e.Meta.NextCursor = cursor
	return e
}
