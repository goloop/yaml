package yaml_test

import (
	"fmt"
	"log"

	"github.com/goloop/yaml"
)

func ExampleUnmarshal() {
	src := []byte(`
name: catalog
port: 8080
debug: true
tags: [rss, atom]
`)

	var cfg struct {
		Name  string   `yaml:"name"`
		Port  int      `yaml:"port"`
		Debug bool     `yaml:"debug"`
		Tags  []string `yaml:"tags"`
	}
	if err := yaml.Unmarshal(src, &cfg); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%s listens on %d, debug=%v, tags=%v\n",
		cfg.Name, cfg.Port, cfg.Debug, cfg.Tags)
	// Output: catalog listens on 8080, debug=true, tags=[rss atom]
}

func ExampleMarshal() {
	type source struct {
		Slug    string   `yaml:"slug"`
		Quality int      `yaml:"quality"`
		Tags    []string `yaml:"tags,omitempty"`
		Note    string   `yaml:"note,omitempty"`
	}

	out, err := yaml.Marshal([]source{
		{Slug: "agfundernews", Quality: 3, Tags: []string{"agtech"}},
		{Slug: "farm-progress", Quality: 2},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(string(out))
	// Output:
	// - slug: agfundernews
	//   quality: 3
	//   tags:
	//     - agtech
	// - slug: farm-progress
	//   quality: 2
}

// Decoding into an interface yields map[string]any, []any and the basic
// types, the way encoding/json does.
func ExampleUnmarshal_intoInterface() {
	var v any
	if err := yaml.Unmarshal([]byte("a: 1\nb: [x, true]\n"), &v); err != nil {
		log.Fatal(err)
	}
	m := v.(map[string]any)
	fmt.Printf("%T %v\n", m["a"], m["a"])
	fmt.Printf("%T %v\n", m["b"], m["b"])
	// Output:
	// int64 1
	// []interface {} [x true]
}

// An anchor names a value and an alias repeats it; a "<<" key merges a
// mapping into another one, filling only the keys it does not state
// itself.
func ExampleUnmarshal_merge() {
	src := []byte(`
defaults: &defaults
  retries: 3
  timeout: 30

fast:
  <<: *defaults
  timeout: 5
`)

	var cfg struct {
		Fast struct {
			Retries int `yaml:"retries"`
			Timeout int `yaml:"timeout"`
		} `yaml:"fast"`
	}
	if err := yaml.Unmarshal(src, &cfg); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("retries=%d timeout=%d\n", cfg.Fast.Retries, cfg.Fast.Timeout)
	// Output: retries=3 timeout=5
}

// Block scalars carry text: "|" keeps the line breaks, ">" folds them
// into spaces.
func ExampleUnmarshal_blockScalars() {
	src := []byte("literal: |\n  first\n  second\nfolded: >\n  first\n  second\n")

	var v struct {
		Literal string `yaml:"literal"`
		Folded  string `yaml:"folded"`
	}
	if err := yaml.Unmarshal(src, &v); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("literal=%q\nfolded=%q\n", v.Literal, v.Folded)
	// Output:
	// literal="first\nsecond\n"
	// folded="first second\n"
}

// A leading zero meant octal in YAML 1.1 and means decimal in 1.2. The
// two readings differ, so rather than pick one quietly the parser asks
// for a spelling that cannot be misread.
func ExampleUnmarshal_ambiguousOctal() {
	var v any
	err := yaml.Unmarshal([]byte("mode: 0644\n"), &v)
	fmt.Println(err)

	var ok struct {
		Mode int `yaml:"mode"`
	}
	if err := yaml.Unmarshal([]byte("mode: 0o644\n"), &ok); err != nil {
		log.Fatal(err)
	}
	fmt.Println(ok.Mode)
	// Output:
	// yaml: line 1: "0644" is ambiguous: a leading zero meant octal in YAML 1.1 and decimal in 1.2; write 0o644 for octal, 644 for decimal, or quote it for a string
	// 420
}

// Errors name the line they were found on, because a config file that
// fails to load is read by a person looking for the mistake.
func ExampleUnmarshal_errors() {
	err := yaml.Unmarshal([]byte("a: 1\nb: [1, 2\n"), new(any))
	fmt.Println(err)
	// Output: yaml: line 2: unterminated flow sequence
}
