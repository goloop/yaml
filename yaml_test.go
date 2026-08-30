package yaml

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

// decodeAny is the shorthand the table tests use: parse into an
// interface and hand back whatever the document turned out to be.
func decodeAny(t *testing.T, src string) any {
	t.Helper()
	var got any
	if err := Unmarshal([]byte(src), &got); err != nil {
		t.Fatalf("Unmarshal(%q): %v", src, err)
	}
	return got
}

func TestScalarResolution(t *testing.T) {
	tests := []struct {
		src  string
		want any
	}{
		// Null in each of its spellings, including an absent value.
		{"v:", nil},
		{"v: ~", nil},
		{"v: null", nil},
		{"v: Null", nil},
		{"v: NULL", nil},

		// Booleans are the core schema's three spellings only.
		{"v: true", true},
		{"v: True", true},
		{"v: TRUE", true},
		{"v: false", false},
		{"v: False", false},

		// yes/no/on/off are YAML 1.1 booleans and stay strings here.
		{"v: yes", "yes"},
		{"v: no", "no"},
		{"v: on", "on"},
		{"v: off", "off"},
		{"v: y", "y"},

		// Integers.
		{"v: 0", int64(0)},
		{"v: 42", int64(42)},
		{"v: -17", int64(-17)},
		{"v: +5", int64(5)},
		{"v: 0x1F", int64(31)},
		{"v: 0xff", int64(255)},
		{"v: 0o644", int64(420)},
		{"v: 00", int64(0)},
		{"v: -9223372036854775808", int64(math.MinInt64)},

		// Floats.
		{"v: 3.14", 3.14},
		{"v: -0.5", -0.5},
		{"v: .5", 0.5},
		{"v: 5.", 5.0},
		{"v: 1e3", 1000.0},
		{"v: 1.5E-3", 0.0015},
		{"v: .inf", math.Inf(1)},
		{"v: -.INF", math.Inf(-1)},

		// Everything else is a string, quoting included.
		{"v: hello", "hello"},
		{`v: "123"`, "123"},
		{`v: '42'`, "42"},
		{`v: "true"`, "true"},
		{"v: 12:30", "12:30"},
		{"v: 2026-08-30", "2026-08-30"},
		{"v: 1_000", "1_000"},
		{"v: a b c", "a b c"},

		// An explicit tag overrides the schema in both directions.
		{"v: !!str 42", "42"},
		{"v: !!int 42", int64(42)},
		{"v: !!float 42", 42.0},
		{"v: !!bool yes", true},
		{"v: !!str true", "true"},
	}

	for _, tc := range tests {
		got := decodeAny(t, tc.src)
		m, ok := got.(map[string]any)
		if !ok {
			t.Errorf("%q: got %T, want a mapping", tc.src, got)
			continue
		}
		v := m["v"]
		if f, isF := tc.want.(float64); isF && math.IsNaN(f) {
			if g, isG := v.(float64); !isG || !math.IsNaN(g) {
				t.Errorf("%q: got %#v, want NaN", tc.src, v)
			}
			continue
		}
		if !reflect.DeepEqual(v, tc.want) {
			t.Errorf("%q: got %#v (%T), want %#v (%T)",
				tc.src, v, v, tc.want, tc.want)
		}
	}
}

func TestNaN(t *testing.T) {
	m := decodeAny(t, "v: .nan").(map[string]any)
	f, ok := m["v"].(float64)
	if !ok || !math.IsNaN(f) {
		t.Fatalf("got %#v, want NaN", m["v"])
	}
}

// Leading zeros are the one place where YAML 1.1 and 1.2 disagree in a
// way that changes a value silently, so the parser refuses them.
func TestAmbiguousOctalRejected(t *testing.T) {
	for _, src := range []string{"v: 0644", "v: 012", "v: -0700", "v: 08"} {
		var got any
		err := Unmarshal([]byte(src), &got)
		if err == nil {
			t.Errorf("%q: expected an error, got %#v", src, got)
			continue
		}
		if !strings.Contains(err.Error(), "ambiguous") {
			t.Errorf("%q: error should explain the ambiguity, got %v", src, err)
		}
	}
	// Quoting resolves it, and so does an explicit base.
	if v := decodeAny(t, `v: "0644"`).(map[string]any)["v"]; v != "0644" {
		t.Errorf(`quoted: got %#v, want "0644"`, v)
	}
	if v := decodeAny(t, "v: 0o644").(map[string]any)["v"]; v != int64(420) {
		t.Errorf("0o644: got %#v, want 420", v)
	}
}

func TestBlockStructure(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want any
	}{
		{
			name: "nested mappings",
			src: `
topic:
  slug: agriculture
  position: 850
`,
			want: map[string]any{"topic": map[string]any{
				"slug": "agriculture", "position": int64(850)}},
		},
		{
			name: "sequence under a key",
			src: `
tags:
  - a
  - b
`,
			want: map[string]any{"tags": []any{"a", "b"}},
		},
		{
			name: "sequence at the key's own column",
			src: `
tags:
- a
- b
`,
			want: map[string]any{"tags": []any{"a", "b"}},
		},
		{
			name: "sequence of mappings, compact form",
			src: `
entries:
  - slug: one
    quality: 3
  - slug: two
    quality: 2
`,
			want: map[string]any{"entries": []any{
				map[string]any{"slug": "one", "quality": int64(3)},
				map[string]any{"slug": "two", "quality": int64(2)},
			}},
		},
		{
			name: "nested sequences",
			src: `
- - a
  - b
- - c
`,
			want: []any{[]any{"a", "b"}, []any{"c"}},
		},
		{
			name: "comments and blank lines are ignored",
			src: `
# leading comment
a: 1   # trailing comment

# another
b: 2
`,
			want: map[string]any{"a": int64(1), "b": int64(2)},
		},
		{
			name: "document markers",
			src: `---
a: 1
...
`,
			want: map[string]any{"a": int64(1)},
		},
		{
			name: "root scalar on the marker line",
			src:  "--- hello\n",
			want: "hello",
		},
		{
			name: "empty value is null",
			src:  "a:\nb: 1\n",
			want: map[string]any{"a": nil, "b": int64(1)},
		},
		{
			name: "quoted keys and values",
			src:  "\"key one\": \"value: with colon\"\n'k2': 'it''s'\n",
			want: map[string]any{"key one": "value: with colon", "k2": "it's"},
		},
		{
			name: "multi-line plain scalar folds",
			src: `
text: one
  two
  three
`,
			want: map[string]any{"text": "one two three"},
		},
		{
			name: "multi-line quoted scalar folds",
			src: `
text: "one
  two"
`,
			want: map[string]any{"text": "one two"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeAny(t, tc.src)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %#v\nwant %#v", got, tc.want)
			}
		})
	}
}

func TestFlowCollections(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want any
	}{
		{"flow sequence", "v: [a, b, c]", map[string]any{
			"v": []any{"a", "b", "c"}}},
		{"flow mapping", "v: {a: 1, b: 2}", map[string]any{
			"v": map[string]any{"a": int64(1), "b": int64(2)}}},
		{"nested flow", "v: {k: [1, {n: 2}]}", map[string]any{
			"v": map[string]any{"k": []any{int64(1),
				map[string]any{"n": int64(2)}}}}},
		{"empty flow", "a: []\nb: {}", map[string]any{
			"a": []any{}, "b": map[string]any{}}},
		{"trailing comma", "v: [a, b,]", map[string]any{
			"v": []any{"a", "b"}}},
		{"quoted inside flow", `v: ["a, b", 'c']`, map[string]any{
			"v": []any{"a, b", "c"}}},
		{"colon inside a plain scalar", "v: [http://example.com]",
			map[string]any{"v": []any{"http://example.com"}}},
		// A single key:value pair may stand as a sequence entry.
		{"single pair in a sequence", "v: [2, 2, SwitchCase: 1]",
			map[string]any{"v": []any{int64(2), int64(2),
				map[string]any{"SwitchCase": int64(1)}}}},
		{"pair with a flow value", "v: [2, allow: [warn, error]]",
			map[string]any{"v": []any{int64(2),
				map[string]any{"allow": []any{"warn", "error"}}}}},
		{"flow spanning lines", "v: [a,\n     b,\n     c]",
			map[string]any{"v": []any{"a", "b", "c"}}},
		{"comment inside flow", "v: [a, # note\n     b]",
			map[string]any{"v": []any{"a", "b"}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeAny(t, tc.src)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %#v\nwant %#v", got, tc.want)
			}
		})
	}
}

func TestBlockScalars(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"literal keeps breaks", "v: |\n  one\n  two\n", "one\ntwo\n"},
		{"literal strip", "v: |-\n  one\n  two\n", "one\ntwo"},
		{"literal keep", "v: |+\n  one\n\n", "one\n\n"},
		{"folded joins lines", "v: >\n  one\n  two\n", "one two\n"},
		{"folded strip", "v: >-\n  one\n  two\n", "one two"},
		{"folded blank line breaks", "v: >\n  one\n\n  two\n", "one\ntwo\n"},
		// A more-indented line inside a folded scalar keeps its own
		// layout, and the breaks around it are not folded away.
		{"folded keeps indented lines", "v: >\n  one\n   deeper\n  two\n",
			"one\n deeper\ntwo\n"},
		{"folded indented then blank", "v: >\n  one\n   deeper\n\n  two\n",
			"one\n deeper\n\ntwo\n"},
		{"explicit indent", "v: |2\n   one\n  two\n", " one\ntwo\n"},
		{"empty literal", "v: |\n", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := decodeAny(t, tc.src).(map[string]any)
			got, _ := m["v"].(string)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAnchorsAliasesMerge(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want any
	}{
		{
			name: "alias repeats a scalar",
			src:  "a: &x 1\nb: *x\n",
			want: map[string]any{"a": int64(1), "b": int64(1)},
		},
		{
			name: "alias repeats a collection",
			src:  "a: &x [1, 2]\nb: *x\n",
			want: map[string]any{"a": []any{int64(1), int64(2)},
				"b": []any{int64(1), int64(2)}},
		},
		{
			name: "merge fills what the mapping omits",
			src: `
base: &base
  a: 1
  b: 2
derived:
  <<: *base
  b: 20
  c: 3
`,
			want: map[string]any{
				"base":    map[string]any{"a": int64(1), "b": int64(2)},
				"derived": map[string]any{"a": int64(1), "b": int64(20), "c": int64(3)},
			},
		},
		{
			name: "merge from a sequence, earlier wins",
			src: `
one: &one
  a: 1
two: &two
  a: 2
  b: 2
merged:
  <<: [*one, *two]
`,
			want: map[string]any{
				"one":    map[string]any{"a": int64(1)},
				"two":    map[string]any{"a": int64(2), "b": int64(2)},
				"merged": map[string]any{"a": int64(1), "b": int64(2)},
			},
		},
		{
			name: "anchor on a sequence item",
			src:  "list:\n  - &x one\n  - *x\n",
			want: map[string]any{"list": []any{"one", "one"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeAny(t, tc.src)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %#v\nwant %#v", got, tc.want)
			}
		})
	}
}
