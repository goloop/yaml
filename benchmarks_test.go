package yaml

import (
	"strings"
	"testing"
)

// catalogDoc is the shape this package was written for: a curated list
// of records, the kind a service reads at start-up.
var catalogDoc = []byte(`
topic:
  slug: agriculture
  title_en: "Agriculture"
  position: 850

entries:
  - slug: agriculture-agfundernews
    title: "AgFunderNews"
    description_en: "Agrifood investing, startups and food-system reporting"
    site_url: "https://agfundernews.com"
    collection: { kind: rss, ident: "https://agfundernews.com/feed" }
    also_topics: [agtech]
    tags: [agriculture, agtech]
    lang: en
    country_code: US
    access: { level: open }
    quality: 3
  - slug: agriculture-farm-progress
    title: "Farm Progress"
    description_en: "Practical agriculture markets, policy and production news"
    site_url: "https://www.farmprogress.com"
    collection: { kind: rss, ident: "https://www.farmprogress.com/rss.xml" }
    tags: [agriculture]
    lang: en
    country_code: US
    access: { level: open }
    quality: 2
`)

type benchTopic struct {
	Slug    string `yaml:"slug"`
	TitleEn string `yaml:"title_en"`
	Pos     int    `yaml:"position"`
}

type benchEntry struct {
	Slug        string `yaml:"slug"`
	Title       string `yaml:"title"`
	Description string `yaml:"description_en"`
	SiteURL     string `yaml:"site_url"`
	Collection  struct {
		Kind  string `yaml:"kind"`
		Ident string `yaml:"ident"`
	} `yaml:"collection"`
	AlsoTopics []string `yaml:"also_topics"`
	Tags       []string `yaml:"tags"`
	Lang       string   `yaml:"lang"`
	Country    string   `yaml:"country_code"`
	Access     struct {
		Level string `yaml:"level"`
	} `yaml:"access"`
	Quality int `yaml:"quality"`
}

type benchFile struct {
	Topic   benchTopic   `yaml:"topic"`
	Entries []benchEntry `yaml:"entries"`
}

func BenchmarkUnmarshalStruct(b *testing.B) {
	b.SetBytes(int64(len(catalogDoc)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var f benchFile
		if err := Unmarshal(catalogDoc, &f); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalInterface(b *testing.B) {
	b.SetBytes(int64(len(catalogDoc)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var v any
		if err := Unmarshal(catalogDoc, &v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshal(b *testing.B) {
	var f benchFile
	if err := Unmarshal(catalogDoc, &f); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Marshal(f); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalScalars(b *testing.B) {
	doc := []byte(strings.Repeat("k: 12345\n", 1) +
		"s: some plain text\nb: true\nf: 3.14\nn: ~\nq: \"quoted\"\n")
	b.SetBytes(int64(len(doc)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var v any
		if err := Unmarshal(doc, &v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalDeepSequence(b *testing.B) {
	var sb strings.Builder
	for i := 0; i < 500; i++ {
		sb.WriteString("- item\n")
	}
	doc := []byte(sb.String())
	b.SetBytes(int64(len(doc)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var v []string
		if err := Unmarshal(doc, &v); err != nil {
			b.Fatal(err)
		}
	}
}
