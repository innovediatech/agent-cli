// Package selectfields implements --select dotted-path projection.
//
// Spec syntax:
//
//	"id"               -> top-level field
//	"a,b,c"            -> multiple top-level fields
//	"user.email"       -> nested object field
//	"items.name"       -> array auto-traversed; project each element
//	"user.tags"        -> whole subtree if "tags" not further qualified
//
// An empty spec returns the input unchanged. Fields that don't exist in the
// input are silently dropped (rather than producing nulls or errors).
//
// Key matching is case-style tolerant: a spec key is tried verbatim first,
// then its snake↔camel rendering. So `--select first_name` projects from an
// API that returns `firstName`, and vice versa. The output key is whatever
// the caller asked for.
//
// The projection operates on `any` values that came out of encoding/json's
// decoder (map[string]any, []any, primitives). It does not use reflection on
// arbitrary structs — encode to JSON first if you have a strongly-typed value.
package selectfields

import (
	"strings"
)

// Selector is a compiled --select spec, ready to apply.
type Selector struct {
	// nil root means "no selection — pass through unchanged"
	root *node
}

type node struct {
	// leaf means "include the whole subtree starting here"
	leaf bool
	// children maps a field name to its sub-selection
	children map[string]*node
}

// Parse compiles a --select spec.
//
// Whitespace around commas and dots is tolerated. Empty / whitespace-only
// spec yields a Selector whose Apply is a no-op.
func Parse(spec string) Selector {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Selector{}
	}
	root := &node{children: map[string]*node{}}
	for _, raw := range strings.Split(spec, ",") {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		insertPath(root, strings.Split(path, "."))
	}
	if len(root.children) == 0 {
		return Selector{}
	}
	return Selector{root: root}
}

func insertPath(n *node, parts []string) {
	if len(parts) == 0 {
		n.leaf = true
		return
	}
	key := strings.TrimSpace(parts[0])
	if key == "" {
		// Skip empty path segments rather than erroring; "a..b" → "a.b"
		insertPath(n, parts[1:])
		return
	}
	child, ok := n.children[key]
	if !ok {
		child = &node{children: map[string]*node{}}
		n.children[key] = child
	}
	if len(parts) == 1 {
		// Leaf: include whole subtree at this node, drop deeper restrictions
		child.leaf = true
		child.children = nil
	} else if !child.leaf {
		// Already-leaf children stay leaves; otherwise descend
		insertPath(child, parts[1:])
	}
}

// Empty reports whether this selector will pass values through unchanged.
func (s Selector) Empty() bool { return s.root == nil }

// Apply returns a projected copy of v.
//
// Behavior:
//   - Empty selector → return v unchanged.
//   - v is map[string]any → keep only selected keys; recurse into them.
//   - v is []any → traverse element-wise; each element projected the same way.
//   - v is a primitive (string/number/bool/nil) → returned as-is when its
//     parent path was selected; not addressable by deeper selections.
//
// Apply does NOT mutate v.
func (s Selector) Apply(v any) any {
	if s.root == nil {
		return v
	}
	return applyNode(s.root, v)
}

func applyNode(n *node, v any) any {
	if n.leaf {
		return v
	}

	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, child := range n.children {
			val, ok := lookupKey(x, key)
			if !ok {
				continue
			}
			out[key] = applyNode(child, val)
		}
		return out

	case []any:
		out := make([]any, 0, len(x))
		for _, item := range x {
			out = append(out, applyNode(n, item))
		}
		return out

	default:
		// Primitive at a non-leaf selection node: caller wanted to descend
		// into a scalar, which makes no sense. Drop it.
		return nil
	}
}

// lookupKey resolves a map entry, trying the key verbatim then its
// snake↔camel rendering. Lets `--select first_name` match an API that
// returns `firstName` (and vice versa). Mirrors the fallback in
// mirror.lookupFieldValue so the two projection layers behave the same.
func lookupKey(m map[string]any, key string) (any, bool) {
	if v, ok := m[key]; ok {
		return v, true
	}
	if alt := snakeToCamel(key); alt != key {
		if v, ok := m[alt]; ok {
			return v, true
		}
	}
	if alt := camelToSnake(key); alt != key {
		if v, ok := m[alt]; ok {
			return v, true
		}
	}
	return nil, false
}

// snakeToCamel turns "first_name" into "firstName". Empty segments
// between underscores are dropped ("a__b" → "aB"); leading or trailing
// underscores in JSON keys aren't worth handling specially.
func snakeToCamel(s string) string {
	if !strings.ContainsRune(s, '_') {
		return s
	}
	parts := strings.Split(s, "_")
	var b strings.Builder
	b.Grow(len(s))
	b.WriteString(parts[0])
	for _, p := range parts[1:] {
		if p == "" {
			b.WriteByte('_')
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		b.WriteString(p[1:])
	}
	return b.String()
}

// camelToSnake turns "firstName" into "first_name". Naive: every
// uppercase rune becomes "_x" (so "URLPath" becomes "u_r_l_path"). Good
// enough for matching JSON keys, which conventionally avoid acronym runs.
func camelToSnake(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
