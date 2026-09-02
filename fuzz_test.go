package yaml

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

var fuzzSeeds = []string{
	"",
	"a: 1\n",
	"a:\n  b: [1, 2]\n",
	"- 1\n- two\n- true\n",
	"a: &x 1\nb: *x\n",
	"base: &b {a: 1}\nd:\n  <<: *b\n  c: 2\n",
	"v: |\n  line\n  line\n",
	"v: >\n  fold\n   deeper\n\n  end\n",
	"v: [a, b: 1, {c: 2}]\n",
	"---\na: 1\n...\n",
	"\"quoted key\": 'single'\n",
	"v: !!str 42\n",
	"v: 0o755\n",
	"# only a comment\n",
	"a: \n b: \n  c: \n",
	"v: [[[[[[]]]]]]\n",
}

// FuzzUnmarshal checks that no input makes the parser panic or hang, and
// that anything it does accept survives a trip through the encoder
// unchanged. A parser that reads untrusted config files has to fail by
// returning an error, never by crashing the process.
func FuzzUnmarshal(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var got any
		if err := Unmarshal(data, &got); err != nil {
			return // a rejected document is a fine outcome
		}

		out, err := Marshal(got)
		if err != nil {
			t.Fatalf("accepted a document it cannot re-encode: %v\ninput: %q\nvalue: %#v",
				err, data, got)
		}
		var back any
		if err := Unmarshal(out, &back); err != nil {
			t.Fatalf("cannot read back its own output: %v\ninput: %q\noutput: %q",
				err, data, out)
		}
		if !reflect.DeepEqual(got, back) {
			t.Fatalf("round trip changed the value\ninput:  %q\nvalue:  %#v\noutput: %q\nback:   %#v",
				data, got, out, back)
		}
	})
}

// FuzzUnmarshalStruct exercises the reflect paths, where a wrong type
// assertion would panic rather than return an error.
func FuzzUnmarshalStruct(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var into struct {
			Slug       string            `yaml:"slug"`
			Quality    int               `yaml:"quality"`
			Ratio      float64           `yaml:"ratio"`
			On         bool              `yaml:"on"`
			Tags       []string          `yaml:"tags"`
			Meta       map[string]string `yaml:"meta"`
			Nested     *collection       `yaml:"nested"`
			Anything   any               `yaml:"anything"`
			Unsigned   uint16            `yaml:"unsigned"`
			Fixed      [2]int            `yaml:"fixed"`
			Text       upperText         `yaml:"text"`
			unexported int
		}
		_ = Unmarshal(data, &into)
		_ = into.unexported
	})
}

var expandSeeds = []string{
	"a: ${HOST}\n",
	"a: $HOST\n",
	"a: \"pre ${HOST} post\"\n",
	"a: 'literal ${HOST}'\n",
	"a: |\n  block ${HOST}\n",
	"a: cost: $100\n",
	"a: pa$$word\n",
	"a: ${HOST\n",
	"a: ${}\n",
	"a: $\n",
	"a: [\"${HOST}\", $HOST]\n",
	"${HOST}: v\n",
	"a: ${HOST}${HOST}\n",
}

// shape describes a decoded document's structure with every scalar
// replaced by one token. It is what expansion must never change: a
// variable holding a colon or a newline substitutes into one value, and
// a value is all it may ever become.
func shape(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = shape(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = shape(e)
		}
		return out
	}
	return "scalar"
}

// FuzzExpandKeepsShape is the security property of WithExpand written as
// a test: whatever an environment variable holds, substituting it may
// change what a value is and never what the document is. A reader that
// expanded the bytes before parsing would let a variable add a key, and
// that is how a configuration file gets rewritten by its own
// environment.
func FuzzExpandKeepsShape(f *testing.F) {
	for _, s := range append(append([]string{}, fuzzSeeds...), expandSeeds...) {
		f.Add([]byte(s), "plain")
	}

	f.Fuzz(func(t *testing.T, data []byte, value string) {
		// The operating system, not this package, refuses a NUL inside
		// an environment variable, so such a value cannot be tested.
		if strings.ContainsRune(value, 0) {
			t.Skip("a NUL cannot be put in the environment")
		}
		t.Setenv("HOST", value)

		var plain any
		if err := Unmarshal(data, &plain); err != nil {
			return // a rejected document proves nothing here
		}

		var expanded any
		if err := Unmarshal(data, &expanded, WithExpand()); err != nil {
			// Only a malformed reference may be refused here, and only
			// when expansion was asked for.
			var se *SyntaxError
			if !errors.As(err, &se) {
				t.Fatalf("expansion refused a readable document with %T: %v\ninput: %q",
					err, err, data)
			}
			return
		}

		if !reflect.DeepEqual(shape(plain), shape(expanded)) {
			t.Fatalf("expansion changed the shape of the document\ninput: %q\nvalue: %q\nplain:    %#v\nexpanded: %#v",
				data, value, plain, expanded)
		}
	})
}

// FuzzExpandText exercises the substituter itself, where the rule that
// keeps "cost: $100" intact lives. With nothing defined, expanding twice
// must give what expanding once gave: a pass that left work behind would
// mean a value could be substituted into a second time.
func FuzzExpandText(f *testing.F) {
	seeds := []string{
		"", "$", "$$", "${", "${}", "$1", "$100", "pa$$word",
		"cost: $100", "$1 == \"x\"", "${NAME}", "$NAME", "$_A1",
		"${NAME}${NAME}", "a${NAME}b", "${NAME", "$ {NAME}", "${-}",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		once, err := expand(s, 1, false)
		if err != nil {
			return // an unterminated reference is refused, and says so
		}
		twice, err := expand(once, 1, false)
		if err != nil {
			t.Fatalf("its own output is malformed to it: %q -> %q: %v", s, once, err)
		}
		if once != twice {
			t.Fatalf("expansion left work behind\ninput: %q\nonce:  %q\ntwice: %q",
				s, once, twice)
		}
		if !strings.Contains(s, "$") && once != s {
			t.Fatalf("text with no %q changed: %q -> %q", "$", s, once)
		}
	})
}

// FuzzDefaults pins what a default is for: it fills a key the document
// left out, and it never overrules one the document set. The two structs
// differ only in their def tags, so any field the plain decode filled
// must survive the decode that has defaults.
func FuzzDefaults(f *testing.F) {
	seeds := []string{
		"", "host: x\n", "port: 1\n", "port: null\n", "host: x\nport: 2\n",
		"timeout: 5s\n", "timeout: null\n", "ratio: 1.5\n", "on: true\n",
		"host: [1]\n", "port: {}\n", "port: not-a-number\n",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var withDefs struct {
			Host    string        `yaml:"host" def:"localhost"`
			Port    int           `yaml:"port" def:"8080"`
			Timeout time.Duration `yaml:"timeout" def:"30s"`
			Ratio   float64       `yaml:"ratio" def:"0.5"`
			On      bool          `yaml:"on" def:"true"`
			Ptr     *int          `yaml:"ptr" def:"7"`
		}
		var noDefs struct {
			Host    string        `yaml:"host"`
			Port    int           `yaml:"port"`
			Timeout time.Duration `yaml:"timeout"`
			Ratio   float64       `yaml:"ratio"`
			On      bool          `yaml:"on"`
			Ptr     *int          `yaml:"ptr"`
		}

		if err := Unmarshal(data, &noDefs); err != nil {
			return
		}
		if err := Unmarshal(data, &withDefs); err != nil {
			// Defaults may only add failures of their own making: a def
			// that does not fit its field. They must not make a document
			// unreadable that was readable without them.
			var te *TypeError
			if !errors.As(err, &te) {
				t.Fatalf("defaults refused a readable document with %T: %v\ninput: %q",
					err, err, data)
			}
			return
		}

		a := reflect.ValueOf(noDefs)
		b := reflect.ValueOf(withDefs)
		for i := 0; i < a.NumField(); i++ {
			fv := a.Field(i)
			if fv.IsZero() {
				continue // absent, or set to the zero value: the default may speak
			}
			if !reflect.DeepEqual(fv.Interface(), b.Field(i).Interface()) {
				t.Fatalf("a default overruled the document in field %s\ninput: %q\nwithout: %#v\nwith:    %#v",
					a.Type().Field(i).Name, data, noDefs, withDefs)
			}
		}
	})
}
