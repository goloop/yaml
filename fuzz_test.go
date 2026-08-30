package yaml

import (
	"reflect"
	"testing"
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
