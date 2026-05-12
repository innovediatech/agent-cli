package selectfields

import (
	"encoding/json"
	"reflect"
	"testing"
)

// j parses a JSON string into the `any` shape encoding/json produces. Test
// inputs go through json so we exercise the same shapes runtime code sees.
func j(t *testing.T, raw string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("bad JSON in test setup: %v", err)
	}
	return v
}

func TestParseEmpty(t *testing.T) {
	cases := []string{"", "   ", "\t\n"}
	for _, c := range cases {
		if !Parse(c).Empty() {
			t.Fatalf("Parse(%q) should be empty", c)
		}
	}
}

func TestApplyEmptyPassthrough(t *testing.T) {
	in := j(t, `{"a":1,"b":2}`)
	out := Parse("").Apply(in)
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("empty selector should pass through; got %v", out)
	}
}

func TestTopLevelSingle(t *testing.T) {
	in := j(t, `{"id":1,"name":"a","extra":"x"}`)
	got := Parse("id").Apply(in)
	want := map[string]any{"id": 1.0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTopLevelMultiple(t *testing.T) {
	in := j(t, `{"id":1,"name":"a","extra":"x"}`)
	got := Parse("id,name").Apply(in)
	want := map[string]any{"id": 1.0, "name": "a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNested(t *testing.T) {
	in := j(t, `{"user":{"id":1,"email":"x@y","name":"a"},"unused":true}`)
	got := Parse("user.email").Apply(in)
	want := map[string]any{"user": map[string]any{"email": "x@y"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestArrayTraverse(t *testing.T) {
	in := j(t, `{"items":[{"id":1,"name":"a","cost":10},{"id":2,"name":"b","cost":20}]}`)
	got := Parse("items.id,items.name").Apply(in)
	want := map[string]any{
		"items": []any{
			map[string]any{"id": 1.0, "name": "a"},
			map[string]any{"id": 2.0, "name": "b"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestArrayAtRoot(t *testing.T) {
	in := j(t, `[{"id":1,"name":"a","x":99},{"id":2,"name":"b","x":99}]`)
	got := Parse("id").Apply(in)
	want := []any{
		map[string]any{"id": 1.0},
		map[string]any{"id": 2.0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestMissingFieldsDropped(t *testing.T) {
	in := j(t, `{"a":1}`)
	got := Parse("a,b,c").Apply(in)
	want := map[string]any{"a": 1.0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPartialPathOnPrimitive(t *testing.T) {
	in := j(t, `{"name":"alice"}`)
	got := Parse("name.first").Apply(in)
	// `name` is a string; descending into it produces nil.
	want := map[string]any{"name": nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLeafSubtreeIncluded(t *testing.T) {
	// Selecting "user" includes the whole user subtree.
	in := j(t, `{"user":{"id":1,"profile":{"city":"X","zip":"00000"}},"extra":"x"}`)
	got := Parse("user").Apply(in)
	want := map[string]any{"user": map[string]any{
		"id":      1.0,
		"profile": map[string]any{"city": "X", "zip": "00000"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestMixedLeafAndDescent(t *testing.T) {
	// "user" and "user.email" -> leaf wins; whole user kept.
	in := j(t, `{"user":{"id":1,"email":"x","other":"y"},"z":"z"}`)
	got := Parse("user,user.email").Apply(in)
	want := map[string]any{"user": map[string]any{"id": 1.0, "email": "x", "other": "y"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWhitespaceTolerance(t *testing.T) {
	in := j(t, `{"a":1,"b":2,"c":3}`)
	got := Parse(" a , b ").Apply(in)
	want := map[string]any{"a": 1.0, "b": 2.0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestEmptySegmentsTolerated(t *testing.T) {
	in := j(t, `{"a":{"b":1}}`)
	got := Parse("a..b").Apply(in)
	want := map[string]any{"a": map[string]any{"b": 1.0}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDoesNotMutateInput(t *testing.T) {
	in := j(t, `{"a":{"b":1,"c":2}}`)
	original, _ := json.Marshal(in)
	_ = Parse("a.b").Apply(in)
	after, _ := json.Marshal(in)
	if string(original) != string(after) {
		t.Fatalf("Apply mutated input: %s -> %s", original, after)
	}
}
