package yaml

import (
	"reflect"
	"strings"
	"testing"
)

// The double-quoted escapes, each of which names a character rather than
// a byte. \x, \u and \U differ only in how many hex digits they take.
func TestEscapes(t *testing.T) {
	tests := []struct{ src, want string }{
		{`"\0"`, "\x00"},
		{`"\a"`, "\a"},
		{`"\b"`, "\b"},
		{`"\t"`, "\t"},
		{`"\n"`, "\n"},
		{`"\v"`, "\v"},
		{`"\f"`, "\f"},
		{`"\r"`, "\r"},
		{`"\e"`, "\x1b"},
		{`"\ "`, " "},
		{`"\""`, `"`},
		{`"\/"`, "/"},
		{`"\\"`, `\`},
		{`"\N"`, "\u0085"}, // next line
		{`"\_"`, "\u00a0"}, // non-breaking space
		{`"\L"`, "\u2028"}, // line separator
		{`"\P"`, "\u2029"}, // paragraph separator
		{`"\x41"`, "A"},
		{`"й"`, "й"},
		{`"\U0001F600"`, "\U0001F600"},
		{`"a\nb"`, "a\nb"},
		// A backslash at end of line joins the lines with nothing
		// between them, unlike a plain fold, which inserts a space.
		{"\"one\\\n  two\"", "onetwo"},
	}

	for _, tc := range tests {
		var got map[string]string
		if err := Unmarshal([]byte("v: "+tc.src+"\n"), &got); err != nil {
			t.Errorf("%s: %v", tc.src, err)
			continue
		}
		if got["v"] != tc.want {
			t.Errorf("%s: got %q, want %q", tc.src, got["v"], tc.want)
		}
	}
}

func TestEscapeErrors(t *testing.T) {
	for _, src := range []string{
		`v: "\q"`,      // unknown escape
		`v: "\x4"`,     // truncated hex
		`v: "\u00"`,    // truncated
		`v: "\xZZ"`,    // not hex
		"v: \"a\\",     // dangling backslash at end of input
		`v: 'unclosed`, // unterminated single quote
	} {
		if err := Unmarshal([]byte(src+"\n"), new(map[string]any)); err == nil {
			t.Errorf("%s: expected an error", src)
		}
	}
}

// Anchors and tags work inside flow collections too, not only in blocks.
func TestFlowProperties(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want any
	}{
		{"anchor and alias in a sequence", "v: [&a x, *a]",
			map[string]any{"v": []any{"x", "x"}}},
		{"anchor and alias in a mapping", "v: {a: &n 1, b: *n}",
			map[string]any{"v": map[string]any{"a": int64(1), "b": int64(1)}}},
		{"tag in a sequence", "v: [!!str 42, 42]",
			map[string]any{"v": []any{"42", int64(42)}}},
		{"anchor on a nested collection", "a: [&s [1], *s]",
			map[string]any{"a": []any{[]any{int64(1)}, []any{int64(1)}}}},
		{"set-style mapping without values", "v: {a, b}",
			map[string]any{"v": map[string]any{"a": nil, "b": nil}}},
		{"plain scalar folding across lines", "v: [one\n     two]",
			map[string]any{"v": []any{"one two"}}},
		{"blank line inside a flow scalar", "v: [one\n\n     two]",
			map[string]any{"v": []any{"one\ntwo"}}},
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

func TestFlowErrors(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"unterminated sequence", "v: [a, b", "unterminated flow sequence"},
		{"unterminated mapping", "v: {a: 1", "unterminated flow mapping"},
		{"missing separator", "v: [a b] c", "after flow collection"},
		{"empty entry", "v: [,a]", "expected a value"},
		{"duplicate flow key", "v: {a: 1, a: 2}", "already defined"},
		{"collection as a key", "v: {[1]: 2}", "keys must be scalars"},
		{"duplicate anchor", "v: [&a &b x]", "duplicate anchor"},
		{"unsupported tag in flow", "v: [!!binary x]", "unsupported tag"},
		{"empty anchor name", "v: [& x]", "empty anchor"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Unmarshal([]byte(tc.src+"\n"), new(any))
			if err == nil {
				t.Fatalf("expected an error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestTaggedScalarErrors(t *testing.T) {
	tests := []struct{ src, want string }{
		{"v: !!int nope", "is not an int"},
		{"v: !!float nope", "is not a float"},
		{"v: !!bool maybe", "is not a bool"},
		{"v: !!null something", "is not empty"},
		{"v: !!map x", "cannot apply to a scalar"},
	}
	for _, tc := range tests {
		err := Unmarshal([]byte(tc.src+"\n"), new(any))
		if err == nil {
			t.Errorf("%s: expected an error", tc.src)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %q, want it to mention %q", tc.src, err, tc.want)
		}
	}

	// !!float accepts an integer spelling, and !!bool the 1.1 words.
	var got map[string]any
	if err := Unmarshal([]byte("a: !!float 7\nb: !!bool off\n"), &got); err != nil {
		t.Fatal(err)
	}
	if got["a"] != 7.0 || got["b"] != false {
		t.Errorf("got %#v", got)
	}
}

// A bool field accepts the YAML 1.1 spellings even though they resolve
// as strings on their own, because a bool field says what was meant.
func TestBoolFieldAcceptsElevenSpellings(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want bool
	}{
		{"yes", true}, {"Yes", true}, {"YES", true}, {"on", true},
		{"y", true}, {"true", true},
		{"no", false}, {"off", false}, {"n", false}, {"false", false},
	} {
		var got struct {
			V bool `yaml:"v"`
		}
		if err := Unmarshal([]byte("v: "+tc.src+"\n"), &got); err != nil {
			t.Errorf("%s: %v", tc.src, err)
			continue
		}
		if got.V != tc.want {
			t.Errorf("%s: got %v, want %v", tc.src, got.V, tc.want)
		}
	}

	// A word that is not a boolean at all still fails.
	var got struct {
		V bool `yaml:"v"`
	}
	if err := Unmarshal([]byte("v: perhaps\n"), &got); err == nil {
		t.Error("perhaps was accepted as a bool")
	}
}

func TestOmitEmptyKinds(t *testing.T) {
	type all struct {
		S        string         `yaml:"s,omitempty"`
		I        int            `yaml:"i,omitempty"`
		U        uint           `yaml:"u,omitempty"`
		F        float64        `yaml:"f,omitempty"`
		B        bool           `yaml:"b,omitempty"`
		Sl       []int          `yaml:"sl,omitempty"`
		M        map[string]int `yaml:"m,omitempty"`
		P        *int           `yaml:"p,omitempty"`
		A        any            `yaml:"a,omitempty"`
		Arr      [0]int         `yaml:"arr,omitempty"`
		Ституція string         `yaml:"keep"`
	}
	got := marshalString(t, all{Ституція: "always"})
	if got != "keep: always\n" {
		t.Errorf("every empty field should be dropped, got:\n%s", got)
	}

	n := 5
	got = marshalString(t, all{
		S: "x", I: 1, U: 2, F: 0.5, B: true,
		Sl: []int{1}, M: map[string]int{"k": 1}, P: &n, A: "y",
		Ституція: "always",
	})
	for _, want := range []string{"s: x", "i: 1", "u: 2", "f: 0.5", "b: true",
		"sl:", "m:", "p: 5", `a: "y"`, "keep: always"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestMarshalPointers(t *testing.T) {
	n := 7
	pp := &n
	got := marshalString(t, map[string]any{"a": &pp, "b": (*int)(nil)})
	if got != "a: 7\nb: null\n" {
		t.Errorf("got %q", got)
	}

	// A pointer whose type marshals itself is the value, not a hop.
	u := upperText{s: "X"}
	if got := marshalString(t, map[string]any{"v": &u}); got != "v: x\n" {
		t.Errorf("got %q", got)
	}
}

func TestMarshalKeyKinds(t *testing.T) {
	t.Run("bool keys", func(t *testing.T) {
		got := marshalString(t, map[bool]int{true: 1, false: 0})
		if got != "false: 0\ntrue: 1\n" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("keys that need quoting", func(t *testing.T) {
		got := marshalString(t, map[string]int{"yes": 1, "a b": 2})
		if got != "a b: 2\n\"yes\": 1\n" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("a collection cannot be a key", func(t *testing.T) {
		if _, err := Marshal(map[[1]int]int{{1}: 2}); err == nil {
			t.Error("an array key was accepted")
		}
	})
}

func TestMarshalRejectsInvalidUTF8(t *testing.T) {
	if _, err := Marshal(map[string]string{"v": "bad\xfe"}); err == nil {
		t.Error("invalid UTF-8 was encoded")
	}
	if err := Unmarshal([]byte("v: bad\xfe\n"), new(any)); err == nil {
		t.Error("invalid UTF-8 was parsed")
	}
}

func TestQuoteControlCharacters(t *testing.T) {
	got := marshalString(t, map[string]string{"v": "a\x01b\x7f"})
	if got != "v: \"a\\x01b\\x7f\"\n" {
		t.Errorf("got %q", got)
	}
	var back map[string]string
	if err := Unmarshal([]byte(got), &back); err != nil {
		t.Fatal(err)
	}
	if back["v"] != "a\x01b\x7f" {
		t.Errorf("round trip gave %q", back["v"])
	}
}

// A document marker has to survive being the value of a string, or the
// encoder writes something its own parser reads as structure.
func TestDocumentMarkersAsStrings(t *testing.T) {
	for _, s := range []string{"---", "...", "--- x", "... x"} {
		out := marshalString(t, map[string]string{"v": s})
		var back map[string]string
		if err := Unmarshal([]byte(out), &back); err != nil {
			t.Errorf("%q: %v (wrote %q)", s, err, out)
			continue
		}
		if back["v"] != s {
			t.Errorf("%q came back as %q (wrote %q)", s, back["v"], out)
		}
	}
	// As a document, "---" opens it and "..." closes it.
	var v any
	if err := Unmarshal([]byte("---\t\n"), &v); err != nil || v != nil {
		t.Errorf("got %#v, %v; want an empty document", v, err)
	}
}

func TestEmbeddedPointerStruct(t *testing.T) {
	type Inner struct {
		B int `yaml:"b"`
	}
	type Outer struct {
		*Inner
		A int `yaml:"a"`
	}
	var got Outer
	if err := Unmarshal([]byte("a: 1\nb: 2\n"), &got); err != nil {
		t.Fatal(err)
	}
	if got.A != 1 || got.Inner == nil || got.B != 2 {
		t.Fatalf("got %+v", got)
	}
	if out := marshalString(t, got); out != "b: 2\na: 1\n" {
		t.Errorf("got %q", out)
	}
}

func TestBlockScalarHeaderErrors(t *testing.T) {
	for _, src := range []string{
		"v: |--\n  x\n",
		"v: |22\n  x\n",
		"v: |x\n  x\n",
	} {
		if err := Unmarshal([]byte(src), new(any)); err == nil {
			t.Errorf("%q: expected an error", src)
		}
	}
}
