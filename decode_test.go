package yaml

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type collection struct {
	Kind  string `yaml:"kind"`
	Ident string `yaml:"ident"`
}

type entry struct {
	Slug       string     `yaml:"slug"`
	Title      string     `yaml:"title"`
	Collection collection `yaml:"collection"`
	AlsoTopics []string   `yaml:"also_topics"`
	Quality    int        `yaml:"quality"`
	State      bool       `yaml:"state_affiliated"`
	Note       string     `yaml:"note,omitempty"`
	Skipped    string     `yaml:"-"`
	Untagged   string
}

func TestUnmarshalStruct(t *testing.T) {
	src := `
slug: agriculture-agfundernews
title: "AgFunderNews"
collection: { kind: rss, ident: "https://example.com/feed" }
also_topics: [agtech, food]
quality: 3
state_affiliated: false
untagged: from-lowercase-name
skipped: ignored
unknown_key: also ignored
`
	var got entry
	if err := Unmarshal([]byte(src), &got); err != nil {
		t.Fatal(err)
	}
	want := entry{
		Slug:       "agriculture-agfundernews",
		Title:      "AgFunderNews",
		Collection: collection{Kind: "rss", Ident: "https://example.com/feed"},
		AlsoTopics: []string{"agtech", "food"},
		Quality:    3,
		Untagged:   "from-lowercase-name",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %#v\nwant %#v", got, want)
	}
}

type inner struct {
	B int `yaml:"b"`
}

type outer struct {
	inner  `yaml:",inline"`
	A      int     `yaml:"a"`
	Ptr    *int    `yaml:"ptr"`
	NilPtr *int    `yaml:"nilptr"`
	Nested *inner  `yaml:"nested"`
	Any    any     `yaml:"any"`
	F      float64 `yaml:"f"`
}

func TestUnmarshalKinds(t *testing.T) {
	src := `
a: 1
b: 2
ptr: 7
nilptr: ~
nested:
  b: 9
any:
  x: [1, two]
f: 3
`
	var got outer
	if err := Unmarshal([]byte(src), &got); err != nil {
		t.Fatal(err)
	}
	if got.A != 1 || got.B != 2 {
		t.Errorf("a=%d b=%d, want 1 and 2 (embedded struct flattens)", got.A, got.B)
	}
	if got.Ptr == nil || *got.Ptr != 7 {
		t.Errorf("ptr = %v, want 7", got.Ptr)
	}
	if got.NilPtr != nil {
		t.Errorf("nilptr = %v, want nil", got.NilPtr)
	}
	if got.Nested == nil || got.Nested.B != 9 {
		t.Errorf("nested = %+v, want b=9", got.Nested)
	}
	wantAny := map[string]any{"x": []any{int64(1), "two"}}
	if !reflect.DeepEqual(got.Any, wantAny) {
		t.Errorf("any = %#v, want %#v", got.Any, wantAny)
	}
	// An integer scalar is acceptable where a float is asked for.
	if got.F != 3 {
		t.Errorf("f = %v, want 3", got.F)
	}
}

func TestUnmarshalContainers(t *testing.T) {
	t.Run("map of strings", func(t *testing.T) {
		var got map[string]string
		if err := Unmarshal([]byte("a: one\nb: two\n"), &got); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"a": "one", "b": "two"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %#v, want %#v", got, want)
		}
	})

	t.Run("map with integer keys", func(t *testing.T) {
		var got map[int]string
		if err := Unmarshal([]byte("1: one\n2: two\n"), &got); err != nil {
			t.Fatal(err)
		}
		want := map[int]string{1: "one", 2: "two"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %#v, want %#v", got, want)
		}
	})

	t.Run("slice", func(t *testing.T) {
		var got []int
		if err := Unmarshal([]byte("- 1\n- 2\n- 3\n"), &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, []int{1, 2, 3}) {
			t.Errorf("got %#v", got)
		}
	})

	t.Run("array pads with zeros", func(t *testing.T) {
		var got [4]int
		if err := Unmarshal([]byte("- 1\n- 2\n"), &got); err != nil {
			t.Fatal(err)
		}
		if got != [4]int{1, 2, 0, 0} {
			t.Errorf("got %#v", got)
		}
	})

	t.Run("array too small", func(t *testing.T) {
		var got [1]int
		err := Unmarshal([]byte("- 1\n- 2\n"), &got)
		if err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("bytes take the text", func(t *testing.T) {
		var got struct {
			V []byte `yaml:"v"`
		}
		if err := Unmarshal([]byte("v: hello\n"), &got); err != nil {
			t.Fatal(err)
		}
		if string(got.V) != "hello" {
			t.Errorf("got %q", got.V)
		}
	})

	t.Run("empty document leaves the target alone", func(t *testing.T) {
		got := map[string]string{"kept": "yes"}
		if err := Unmarshal([]byte("# only a comment\n"), &got); err != nil {
			t.Fatal(err)
		}
		if got["kept"] != "yes" {
			t.Errorf("got %#v, want the original value", got)
		}
	})
}

// upperText proves the encoding.TextUnmarshaler path, and that the
// decoder hands over the scalar's text rather than a resolved value.
type upperText struct{ s string }

func (u *upperText) UnmarshalText(b []byte) error {
	if string(b) == "boom" {
		return fmt.Errorf("refused")
	}
	u.s = strings.ToUpper(string(b))
	return nil
}

func (u upperText) MarshalText() ([]byte, error) {
	return []byte(strings.ToLower(u.s)), nil
}

func TestTextUnmarshaler(t *testing.T) {
	var got struct {
		V   upperText  `yaml:"v"`
		P   *upperText `yaml:"p"`
		Num upperText  `yaml:"num"`
	}
	src := "v: hello\np: world\nnum: 42\n"
	if err := Unmarshal([]byte(src), &got); err != nil {
		t.Fatal(err)
	}
	if got.V.s != "HELLO" {
		t.Errorf("v = %q, want HELLO", got.V.s)
	}
	if got.P == nil || got.P.s != "WORLD" {
		t.Errorf("p = %+v, want WORLD", got.P)
	}
	// The text is handed over as written, even when the core schema
	// would have called it an integer.
	if got.Num.s != "42" {
		t.Errorf("num = %q, want 42", got.Num.s)
	}

	t.Run("error is reported with its line", func(t *testing.T) {
		var v struct {
			V upperText `yaml:"v"`
		}
		err := Unmarshal([]byte("a: 1\nv: boom\n"), &v)
		if err == nil || !strings.Contains(err.Error(), "line 2") {
			t.Fatalf("got %v, want an error naming line 2", err)
		}
	})
}

func TestDecodeErrors(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		target any
		want   string
	}{
		{"tab indentation", "a:\n\tb: 1\n", new(map[string]any), "tab"},
		{"duplicate key", "a: 1\na: 2\n", new(map[string]any), "already defined"},
		{"unknown alias", "a: *missing\n", new(map[string]any), "unknown anchor"},
		{"two documents", "a: 1\n---\nb: 2\n", new(map[string]any), "multiple documents"},
		{"directive", "%YAML 1.2\na: 1\n", new(map[string]any), "directives"},
		{"unterminated quote", `a: "oops`, new(map[string]any), "unterminated"},
		{"explicit key", "? a\n: 1\n", new(map[string]any), "explicit mapping keys"},
		{"unsupported tag", "a: !!binary Zm9v\n", new(map[string]any), "unsupported tag"},
		{"string into int", "a: hello\n", new(struct {
			A int `yaml:"a"`
		}), "cannot decode"},
		{"overflow", "a: 300\n", new(struct {
			A int8 `yaml:"a"`
		}), "overflows"},
		{"negative into uint", "a: -1\n", new(struct {
			A uint `yaml:"a"`
		}), "negative"},
		{"mapping into slice", "a: 1\n", new([]int), "cannot decode"},
		{"sequence into struct", "- 1\n", new(struct{}), "cannot decode"},
		{"merge of a scalar", "a: &x 1\nb:\n  <<: *x\n", new(map[string]any), "merge key"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Unmarshal([]byte(tc.src), tc.target)
			if err == nil {
				t.Fatalf("expected an error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %q, want it to mention %q", err, tc.want)
			}
		})
	}

	t.Run("not a pointer", func(t *testing.T) {
		if err := Unmarshal([]byte("a: 1"), map[string]any{}); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("nil pointer", func(t *testing.T) {
		var p *map[string]any
		if err := Unmarshal([]byte("a: 1"), p); err == nil {
			t.Fatal("expected an error")
		}
	})
}

// The decoder has to stay bounded on input written to attack it: an
// alias is a shared pointer, so a short document can name a very large
// tree.
func TestExpansionIsBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString("a: &a [x, x, x, x, x, x, x, x, x]\n")
	prev := "a"
	for i := 0; i < 7; i++ {
		name := string(rune('b' + i))
		fmt.Fprintf(&b, "%s: &%s [", name, name)
		for j := 0; j < 9; j++ {
			if j > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "*%s", prev)
		}
		b.WriteString("]\n")
		prev = name
	}

	var got any
	err := Unmarshal([]byte(b.String()), &got)
	if err == nil {
		t.Fatal("a document expanding to billions of values was accepted")
	}
	if !strings.Contains(err.Error(), "expands to more than") {
		t.Errorf("got %q, want the expansion limit", err)
	}
}

func TestDepthIsBounded(t *testing.T) {
	src := "v: " + strings.Repeat("[", 4000) + strings.Repeat("]", 4000)
	var got any
	err := Unmarshal([]byte(src), &got)
	if err == nil {
		t.Fatal("a document nested 4000 deep was accepted")
	}
	if !strings.Contains(err.Error(), "deep") {
		t.Errorf("got %q, want the depth limit", err)
	}
}

// A cycle is not representable: an alias may only name an anchor that is
// already complete, so this must read as an unknown anchor rather than
// loop forever.
func TestSelfReferenceIsRejected(t *testing.T) {
	err := Unmarshal([]byte("a: &x [*x]\n"), new(any))
	if err == nil {
		t.Fatal("a self-referencing alias was accepted")
	}
	if !strings.Contains(err.Error(), "unknown anchor") {
		t.Errorf("got %q", err)
	}
}
