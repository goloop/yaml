package yaml

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// The corpus test runs this package over a body of real YAML that lives
// outside the repository: configuration, manifests, catalogs and API
// descriptions written by people who were not thinking about this
// parser. It is the difference between passing the cases we thought of
// and passing the ones we did not.
//
// Those files are not ours to publish, so none of them is committed
// here. Point YAML_CORPUS at one or more directories to run it:
//
//	YAML_CORPUS=~/some/configs go test -run TestCorpus -v
//
// With the variable unset the test skips, so a fresh clone is green
// without anything extra on disk. testdata/shapes.yaml carries the
// constructs the corpus turned out to contain, so the shapes stay
// covered even when the corpus is not there.

func corpusFiles(t *testing.T) []string {
	t.Helper()

	roots := os.Getenv("YAML_CORPUS")
	if roots == "" {
		t.Skip("set YAML_CORPUS to a directory of real YAML to run this")
	}

	var files []string
	for _, root := range filepath.SplitList(roots) {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		_ = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return nil
			}
			if ext := filepath.Ext(p); ext == ".yaml" || ext == ".yml" {
				files = append(files, p)
			}
			return nil
		})
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Skipf("no .yaml or .yml files under %q", roots)
	}
	return files
}

// TestCorpusRoundTrip checks the two properties that need no second
// opinion: what this package writes, it reads back unchanged, and what
// it refuses, it refuses with a typed error carrying a line.
//
// A file this package will not read is not by itself a failure - real
// corpora contain template placeholders, multi-document streams and
// tags this package refuses by name. What would be a failure is
// refusing without saying where.
func TestCorpusRoundTrip(t *testing.T) {
	files := corpusFiles(t)

	var parsed, refused int
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}

		var got any
		if err := Unmarshal(data, &got); err != nil {
			var se *SyntaxError
			var te *TypeError
			switch {
			case errors.As(err, &se):
				if se.Line < 1 {
					t.Errorf("%s: refused without a line: %v", f, err)
				}
			case errors.As(err, &te):
				if te.Line < 1 {
					t.Errorf("%s: refused without a line: %v", f, err)
				}
			default:
				t.Errorf("%s: refused with an untyped error: %v", f, err)
			}
			refused++
			continue
		}
		parsed++

		out, err := Marshal(got)
		if err != nil {
			t.Errorf("%s: cannot re-encode what was just read: %v", f, err)
			continue
		}
		var back any
		if err := Unmarshal(out, &back); err != nil {
			t.Errorf("%s: cannot read own output: %v", f, err)
			continue
		}
		if !reflect.DeepEqual(got, back) {
			t.Errorf("%s: a round trip changed the value", f)
		}
	}

	t.Logf("%d files: %d parsed and round-tripped, %d refused with a line",
		len(files), parsed, refused)
	if parsed == 0 {
		t.Fatal("nothing in the corpus parsed at all")
	}
}

// TestCorpusStrictIsNotStricterThanTheParser checks that WithStrict only
// ever adds unknown-key errors. Decoding into a map has no unknown keys,
// so anything the plain decode accepts a strict decode must accept too.
func TestCorpusStrictIsNotStricterThanTheParser(t *testing.T) {
	files := corpusFiles(t)

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var lax any
		if err := Unmarshal(data, &lax); err != nil {
			continue
		}
		var strict any
		if err := Unmarshal(data, &strict, WithStrict()); err != nil {
			t.Errorf("%s: strict refused what the parser accepted: %v", f, err)
			continue
		}
		if !reflect.DeepEqual(lax, strict) {
			t.Errorf("%s: strict decoding changed the value", f)
		}
	}
}

// TestShapes covers the constructs the real corpus turned out to be made
// of, from a fixture small enough to live in the repository. The file is
// written for this test, not copied from anywhere.
func TestShapes(t *testing.T) {
	data, err := os.ReadFile("testdata/shapes.yaml")
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}

	// An anchor and the alias that reuses it must be the same value.
	base, ok := got["base"].(map[string]any)
	if !ok {
		t.Fatalf("base = %#v", got["base"])
	}
	reused, ok := got["reused"].(map[string]any)
	if !ok {
		t.Fatalf("reused = %#v", got["reused"])
	}
	if !reflect.DeepEqual(base, reused) {
		t.Errorf("an alias did not reproduce its anchor")
	}

	// A merge fills gaps and never overwrites what the mapping states.
	merged, ok := got["merged"].(map[string]any)
	if !ok {
		t.Fatalf("merged = %#v", got["merged"])
	}
	if merged["retries"] != int64(5) {
		t.Errorf("the mapping's own key lost to the merged one: %#v", merged)
	}
	if merged["timeout"] != "30s" {
		t.Errorf("the merge did not fill a gap: %#v", merged)
	}

	// A literal block keeps its breaks; a folded one turns them into
	// spaces.
	if got["literal"] != "line one\nline two\n" {
		t.Errorf("literal = %q", got["literal"])
	}
	if got["folded"] != "one long line of prose\n" {
		t.Errorf("folded = %q", got["folded"])
	}

	// Flow collections, nested.
	flow, ok := got["flow"].([]any)
	if !ok || len(flow) != 3 {
		t.Fatalf("flow = %#v", got["flow"])
	}
	if m, ok := flow[2].(map[string]any); !ok || m["kind"] != "rss" {
		t.Errorf("flow[2] = %#v", flow[2])
	}

	// The scalar resolutions this package is opinionated about.
	if got["yes_is_a_string"] != "yes" {
		t.Errorf("yes = %#v", got["yes_is_a_string"])
	}
	if got["quoted_number"] != "8080" {
		t.Errorf("quoted number = %#v", got["quoted_number"])
	}
	if got["hex"] != int64(31) {
		t.Errorf("hex = %#v", got["hex"])
	}
	if got["empty_is_null"] != nil {
		t.Errorf("empty = %#v", got["empty_is_null"])
	}

	// And it all survives a round trip.
	out, err := Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, back) {
		t.Error("a round trip changed the fixture")
	}
}
