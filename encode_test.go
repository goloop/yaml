package yaml

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func marshalString(t *testing.T, v any) string {
	t.Helper()
	b, err := Marshal(v)
	if err != nil {
		t.Fatalf("Marshal(%#v): %v", v, err)
	}
	return string(b)
}

func TestMarshalShape(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"root scalar", "hello", "hello\n"},
		{"root null", nil, "null\n"},
		{"root sequence", []int{1, 2}, "- 1\n- 2\n"},
		{"empty map", map[string]any{}, "{}\n"},
		{"empty slice", []int{}, "[]\n"},
		{
			name: "keys are sorted",
			in:   map[string]any{"b": 2, "a": 1, "c": 3},
			want: "a: 1\nb: 2\nc: 3\n",
		},
		{
			name: "nested mapping",
			in: map[string]any{"topic": map[string]any{
				"slug": "x", "position": 850}},
			want: "topic:\n  position: 850\n  slug: x\n",
		},
		{
			name: "empty collections stay on the key's line",
			in:   map[string]any{"a": []int{}, "b": map[string]int{}},
			want: "a: []\nb: {}\n",
		},
		{
			name: "sequence of mappings is compact",
			in: map[string]any{"entries": []any{
				map[string]any{"slug": "one", "q": 3},
				map[string]any{"slug": "two", "q": 2},
			}},
			want: "entries:\n  - q: 3\n    slug: one\n  - q: 2\n    slug: two\n",
		},
		{
			name: "nested sequences",
			in:   [][]int{{1, 2}, {3}},
			want: "- - 1\n  - 2\n- - 3\n",
		},
		{
			name: "integer keys sort by value, not by text",
			in:   map[int]string{10: "ten", 9: "nine", 1: "one"},
			want: "1: one\n9: nine\n10: ten\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := marshalString(t, tc.in); got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

// A string has to come back a string: anything that would read as a
// number, a boolean or a null gets quotes.
func TestMarshalQuoting(t *testing.T) {
	tests := []struct{ in, want string }{
		{"hello", "hello"},
		{"hello world", "hello world"},
		{"", `""`},
		{"123", `"123"`},
		{"1.5", `"1.5"`},
		{"true", `"true"`},
		{"null", `"null"`},
		{"~", `"~"`},
		{"yes", `"yes"`},
		{"off", `"off"`},
		{"0644", `"0644"`},
		{" padded", `" padded"`},
		{"trailing ", `"trailing "`},
		{"a: b", `"a: b"`},
		{"ends:", `"ends:"`},
		{"has # hash", `"has # hash"`},
		{"#leading", `"#leading"`},
		{"- dash", `"- dash"`},
		{"-dash", "-dash"},
		{"a:b", "a:b"},
		{"[bracket", `"[bracket"`},
		{"line\nbreak", `"line\nbreak"`},
		{"tab\there", `"tab\there"`},
		// Neither a backslash nor a quote means anything inside a plain
		// scalar, so quoting them would be noise.
		{"back\\slash", `back\slash`},
		{`quo"te`, `quo"te`},
		{"кирилиця", "кирилиця"},
	}

	for _, tc := range tests {
		got := marshalString(t, map[string]string{"v": tc.in})
		want := "v: " + tc.want + "\n"
		if got != want {
			t.Errorf("%q: got %q, want %q", tc.in, got, want)
		}
		// Whatever the quoting, it has to survive the trip back.
		var back map[string]string
		if err := Unmarshal([]byte(got), &back); err != nil {
			t.Errorf("%q: reparse failed: %v", tc.in, err)
			continue
		}
		if back["v"] != tc.in {
			t.Errorf("%q: came back as %q", tc.in, back["v"])
		}
	}
}

func TestMarshalNumbers(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{42, "42"},
		{-17, "-17"},
		{uint8(255), "255"},
		{3.14, "3.14"},
		// A float that would print without a dot needs one, or it reads
		// back as an integer.
		{1.0, "1.0"},
		{float32(2), "2.0"},
		{math.Inf(1), ".inf"},
		{math.Inf(-1), "-.inf"},
		{math.NaN(), ".nan"},
		{1e21, "1e+21"},
		{true, "true"},
		{false, "false"},
	}

	for _, tc := range tests {
		got := marshalString(t, map[string]any{"v": tc.in})
		want := "v: " + tc.want + "\n"
		if got != want {
			t.Errorf("%#v: got %q, want %q", tc.in, got, want)
		}
	}
}

type marshalStruct struct {
	Zulu    string   `yaml:"zulu"`
	Alpha   int      `yaml:"alpha"`
	Omitted string   `yaml:"omitted,omitempty"`
	Kept    string   `yaml:"kept,omitempty"`
	Zero    int      `yaml:"zero,omitempty"`
	List    []string `yaml:"list,omitempty"`
	Hidden  string   `yaml:"-"`
	private string
}

func TestMarshalStruct(t *testing.T) {
	got := marshalString(t, marshalStruct{
		Zulu: "z", Alpha: 1, Kept: "here", Hidden: "no", private: "no",
	})
	// Declaration order is kept: a struct says what order its fields go
	// in, unlike a map, which has none.
	want := "zulu: z\nalpha: 1\nkept: here\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarshalTextMarshaler(t *testing.T) {
	got := marshalString(t, map[string]any{"v": upperText{s: "HELLO"}})
	if got != "v: hello\n" {
		t.Errorf("got %q", got)
	}
}

func TestMarshalRejectsUnrepresentable(t *testing.T) {
	if _, err := Marshal(map[string]any{"v": func() {}}); err == nil {
		t.Error("a func was accepted")
	}
	if _, err := Marshal(map[string]any{"v": make(chan int)}); err == nil {
		t.Error("a channel was accepted")
	}
}

// Marshal has to be deterministic: the same value must produce the same
// bytes every time, or a generated file churns in version control.
func TestMarshalIsStable(t *testing.T) {
	in := map[string]any{
		"z": 1, "a": 2, "m": 3, "b": 4, "y": 5, "c": 6, "x": 7,
	}
	first := marshalString(t, in)
	for i := 0; i < 50; i++ {
		if got := marshalString(t, in); got != first {
			t.Fatalf("run %d differs:\n%s\nvs\n%s", i, got, first)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	cases := []any{
		map[string]any{"a": int64(1), "b": "two", "c": true, "d": nil},
		map[string]any{"list": []any{int64(1), "two", false}},
		map[string]any{"nested": map[string]any{
			"deep": map[string]any{"deeper": []any{int64(1)}}}},
		map[string]any{"empty_map": map[string]any{}, "empty_list": []any{}},
		map[string]any{"tricky": []any{"yes", "123", "", "a: b", "#x"}},
		[]any{map[string]any{"k": "v"}, map[string]any{"k": "w"}},
		"just a scalar",
	}

	for _, in := range cases {
		out, err := Marshal(in)
		if err != nil {
			t.Errorf("Marshal(%#v): %v", in, err)
			continue
		}
		var back any
		if err := Unmarshal(out, &back); err != nil {
			t.Errorf("Unmarshal(%q): %v", out, err)
			continue
		}
		if !reflect.DeepEqual(in, back) {
			t.Errorf("round trip changed the value\nin:   %#v\nyaml: %s\nout:  %#v",
				in, out, back)
		}
	}
}

func TestRoundTripStruct(t *testing.T) {
	in := entry{
		Slug:       "s",
		Title:      "T",
		Collection: collection{Kind: "rss", Ident: "https://x/feed"},
		AlsoTopics: []string{"a", "b"},
		Quality:    3,
		State:      true,
		Untagged:   "u",
	}
	out, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var back entry
	if err := Unmarshal(out, &back); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !reflect.DeepEqual(in, back) {
		t.Errorf("in:   %#v\nyaml:\n%sout:  %#v", in, out, back)
	}
}

// Marshal writes two spaces per level and never a tab, since a tab is
// not legal YAML indentation.
func TestMarshalNeverIndentsWithTabs(t *testing.T) {
	out := marshalString(t, map[string]any{
		"a": map[string]any{"b": map[string]any{"c": []any{"d"}}}})
	if strings.Contains(out, "\t") {
		t.Errorf("output contains a tab:\n%q", out)
	}
	want := "a:\n  b:\n    c:\n      - d\n"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}
