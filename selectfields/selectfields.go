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
			val, ok := x[key]
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
