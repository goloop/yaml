package yaml

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Every case below stands for a defect the package once had. They are
// kept together so that a regression is recognised as one.

// A quoted key used to trim exactly one blank before the value, so the
// rest of the run stayed in the value and hid its opening quote.
func TestKeySeparatorIsFullyTrimmed(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want any
	}{
		{"quoted key, several spaces", "\"k\":   \"v\"\n", "v"},
		{"quoted key, tab", "\"k\":\tv\n", "v"},
		{"quoted key, mixed blanks", "\"k\": \t \"v\"\n", "v"},
		{"plain key, several spaces", "k:   v\n", "v"},
		{"plain key, tab", "k:\tv\n", "v"},
		{"single-quoted key", "'k':   'v'\n", "v"},
		{"quoted key, flow value", "\"k\":   [1]\n", []any{int64(1)}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			if err := Unmarshal([]byte(tc.src), &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got["k"], tc.want) {
				t.Errorf("got %#v, want %#v", got["k"], tc.want)
			}
		})
	}
}

// A tab is a legal separator after a sequence dash, and it is not part
// of the item.
func TestSequenceDashSeparator(t *testing.T) {
	for _, src := range []string{"-\tone\n-\ttwo\n", "- one\n- two\n"} {
		var got []string
		if err := Unmarshal([]byte(src), &got); err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if !reflect.DeepEqual(got, []string{"one", "two"}) {
			t.Errorf("%q: got %#v", src, got)
		}
	}
	// A dash with nothing blank after it is still a plain scalar.
	var s string
	if err := Unmarshal([]byte("-item\n"), &s); err != nil {
		t.Fatal(err)
	}
	if s != "-item" {
		t.Errorf("got %q, want %q", s, "-item")
	}
}

// U+0085, U+2028 and U+2029 end a line for some readers. Writing one
// bare would turn a one-line scalar into two.
func TestLineBreakingCharactersAreEscaped(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"a\u0085b", `"a\Nb"`},
		{"a\u2028b", `"a\Lb"`},
		{"a\u2029b", `"a\Pb"`},
	} {
		out := marshalString(t, map[string]string{"v": tc.in})
		if out != "v: "+tc.want+"\n" {
			t.Errorf("%q: got %q, want %q", tc.in, out, "v: "+tc.want+"\n")
		}
		var back map[string]string
		if err := Unmarshal([]byte(out), &back); err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if back["v"] != tc.in {
			t.Errorf("%q came back as %q", tc.in, back["v"])
		}
	}
	// A non-breaking space is not a break and needs no escape.
	if out := marshalString(t, map[string]string{"v": "a\u00a0b"}); out != "v: a\u00a0b\n" {
		t.Errorf("NBSP should stay bare, got %q", out)
	}
}

// A field the struct declares itself wins over one reached through an
// embedded struct, whichever is met first. This is what encoding/json
// does, and a struct that behaved differently under the two would be a
// trap.
func TestShallowFieldWins(t *testing.T) {
	type deep struct {
		B int `yaml:"b"`
	}
	type mid struct {
		deep
		C int `yaml:"c"`
	}
	type top struct {
		mid
		B int `yaml:"b"` // declared after the embedding, still wins
	}

	var got top
	if err := Unmarshal([]byte("b: 7\nc: 9\n"), &got); err != nil {
		t.Fatal(err)
	}
	if got.B != 7 {
		t.Errorf("top.B = %d, want 7", got.B)
	}
	if got.mid.deep.B != 0 {
		t.Errorf("deep.B = %d, want 0 (the outer field took the key)", got.mid.deep.B)
	}
	if got.C != 9 {
		t.Errorf("C = %d, want 9", got.C)
	}

	// Marshal follows declaration order and writes each key once.
	got.mid.deep.B = 1
	out := marshalString(t, got)
	if strings.Count(out, "b:") != 1 {
		t.Errorf("key written more than once:\n%s", out)
	}
	if out != "c: 9\nb: 7\n" {
		t.Errorf("got:\n%swant:\nc: 9\nb: 7\n", out)
	}
}

// An option that changes what a field means must never be ignored.
func TestTagOptions(t *testing.T) {
	t.Run("inline on an embedded struct is honoured", func(t *testing.T) {
		type inner struct {
			B int `yaml:"b"`
		}
		type outer struct {
			inner `yaml:",inline"`
			A     int `yaml:"a"`
		}
		var got outer
		if err := Unmarshal([]byte("a: 1\nb: 2\n"), &got); err != nil {
			t.Fatal(err)
		}
		if got.A != 1 || got.B != 2 {
			t.Errorf("got %+v, want A=1 B=2", got)
		}
	})

	t.Run("inline on a named field is refused", func(t *testing.T) {
		var got struct {
			A    int            `yaml:"a"`
			Rest map[string]any `yaml:",inline"`
		}
		err := Unmarshal([]byte("a: 1\nx: 2\n"), &got)
		if err == nil {
			t.Fatal("a field that would have silently lost keys was accepted")
		}
		if !strings.Contains(err.Error(), "only supported on an embedded struct") {
			t.Errorf("got %q", err)
		}
	})

	t.Run("an unknown option is refused", func(t *testing.T) {
		var got struct {
			A int `yaml:"a,flow"`
		}
		err := Unmarshal([]byte("a: 1\n"), &got)
		if err == nil {
			t.Fatal("an unimplemented option was accepted")
		}
		if !strings.Contains(err.Error(), "unsupported tag option") {
			t.Errorf("got %q", err)
		}
	})

	t.Run("the error also reaches Marshal", func(t *testing.T) {
		var v struct {
			A int `yaml:"a,flow"`
		}
		if _, err := Marshal(v); err == nil {
			t.Error("Marshal accepted an unimplemented option")
		}
	})
}

// A type built from text cannot be filled key by key: a mapping there
// means the document disagrees with the target, and every key would be
// dropped as unknown.
func TestMappingIntoTextTargetFails(t *testing.T) {
	var got struct {
		V time.Time `yaml:"v"`
	}
	err := Unmarshal([]byte("v: {}\n"), &got)
	if err == nil {
		t.Fatal("a mapping was silently accepted into a time.Time")
	}
	if !strings.Contains(err.Error(), "cannot decode a mapping") {
		t.Errorf("got %q", err)
	}

	// A scalar still works through the text path.
	if err := Unmarshal([]byte("v: 2026-08-31T10:00:00Z\n"), &got); err != nil {
		t.Fatal(err)
	}
	if got.V.Year() != 2026 {
		t.Errorf("got %v", got.V)
	}
}

// A null names nothing, so it cannot be a key: decoding it would give
// the empty key, which a real "" key already owns, and one of the two
// values would disappear.
func TestNullKeyRefused(t *testing.T) {
	for _, src := range []string{"~: 1\n", "null: 1\n", "NULL: 1\n"} {
		err := Unmarshal([]byte(src), new(map[string]any))
		if err == nil {
			t.Errorf("%q: a null key was accepted", src)
			continue
		}
		if !strings.Contains(err.Error(), "null mapping key") {
			t.Errorf("%q: got %q", src, err)
		}
	}

	// Quoted, it is ordinary text and keeps working.
	var got map[string]any
	if err := Unmarshal([]byte("\"~\": 1\n\"\": 2\n"), &got); err != nil {
		t.Fatal(err)
	}
	if got["~"] != int64(1) || got[""] != int64(2) {
		t.Errorf("got %#v", got)
	}
}

func TestStrictMode(t *testing.T) {
	type conf struct {
		Retries int `yaml:"retries"`
	}

	t.Run("a typo is an error", func(t *testing.T) {
		var c conf
		err := Unmarshal([]byte("retires: 3\n"), &c, WithStrict())
		if err == nil {
			t.Fatal("the typo was skipped")
		}
		if !strings.Contains(err.Error(), `unknown key "retires"`) {
			t.Errorf("got %q", err)
		}
		var te *TypeError
		if !errors.As(err, &te) || te.Line != 1 {
			t.Errorf("want a *TypeError on line 1, got %#v", err)
		}
	})

	t.Run("the lenient form still skips it", func(t *testing.T) {
		var c conf
		if err := Unmarshal([]byte("retires: 3\n"), &c); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("known keys decode either way", func(t *testing.T) {
		var c conf
		if err := Unmarshal([]byte("retries: 3\n"), &c, WithStrict()); err != nil {
			t.Fatal(err)
		}
		if c.Retries != 3 {
			t.Errorf("got %d", c.Retries)
		}
	})

	t.Run("a map target has no unknown keys", func(t *testing.T) {
		var m map[string]int
		if err := Unmarshal([]byte("anything: 1\n"), &m, WithStrict()); err != nil {
			t.Fatal(err)
		}
	})
}

// The line number is the reason the error types exist, so it has to be
// reachable without parsing the message.
func TestErrorTypesCarryTheLine(t *testing.T) {
	var se *SyntaxError
	err := Unmarshal([]byte("a: 1\nb: [1, 2\n"), new(any))
	if !errors.As(err, &se) {
		t.Fatalf("want a *SyntaxError, got %#v", err)
	}
	if se.Line != 2 {
		t.Errorf("Line = %d, want 2", se.Line)
	}
	if strings.Contains(se.Msg, "line") {
		t.Errorf("Msg should not repeat the prefix: %q", se.Msg)
	}
	if !strings.HasPrefix(se.Error(), "yaml: line 2: ") {
		t.Errorf("Error() = %q", se.Error())
	}

	var te *TypeError
	err = Unmarshal([]byte("a: 1\nb: hello\n"), &struct {
		A int `yaml:"a"`
		B int `yaml:"b"`
	}{})
	if !errors.As(err, &te) {
		t.Fatalf("want a *TypeError, got %#v", err)
	}
	if te.Line != 2 {
		t.Errorf("Line = %d, want 2", te.Line)
	}

	// The two are distinct: a malformed document is not a type mismatch.
	if errors.As(err, &se) {
		t.Error("a type mismatch reported itself as a syntax error")
	}
}
