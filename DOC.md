# yaml Reference

`yaml` encodes and decodes the de-facto YAML configuration format. It
targets Go 1.24+ and has no third-party dependencies.

Ukrainian version: **[DOC.UK.md](DOC.UK.md)**.

## Purpose

The package answers one question - **"what does this configuration file
say?"** - for the files a service reads at start-up: settings, manifests,
curated catalogs, test fixtures.

It is not a complete YAML 1.2 implementation, and that is the design.
Complete YAML is a large language with corners that almost no
configuration file uses and that every reader disagrees about. This
package covers the part that files really contain, and refuses the rest
by name, so that an input either decodes to what it looks like or fails
with a line number.

## When to use it

- Reading configuration, manifests and fixtures into Go structs.
- Writing them back out - generated catalogs, exported settings - where
  the output must be stable byte for byte.
- Anywhere the input may come from a user, since the decoder is bounded
  in depth and in alias expansion.

## When not to use it

- You need several documents in one stream, `%TAG` directives, custom
  application tags, or non-scalar mapping keys.
- You need the exact type inference of YAML 1.1 (see *Scalars* below).
- You need to preserve comments and formatting across a read-modify-write
  cycle. This package decodes to values, not to a document tree.

## API

```go
func Unmarshal(data []byte, v any) error
func UnmarshalStrict(data []byte, v any) error
func Marshal(v any) ([]byte, error)

type SyntaxError struct{ Line int; Msg string }
type TypeError   struct{ Line int; Msg string }
```

That is the whole surface. Everything else is expressed through Go types
and struct tags, as in `encoding/json`.

### Unmarshal

`v` must be a non-nil pointer. An empty document (blank, or only
comments) leaves it untouched and returns nil.

Decoding into an interface value produces:

| YAML | Go |
|---|---|
| mapping | `map[string]any` |
| sequence | `[]any` |
| string | `string` |
| integer | `int64` |
| float | `float64` |
| boolean | `bool` |
| null | `nil` |

Mapping keys are strings, as in JSON, so a decoded document is directly
comparable with one that came from `encoding/json`.

### UnmarshalStrict

`Unmarshal` ignores keys the target does not know, which is right for a
document several versions of a program must read. `UnmarshalStrict`
makes them an error, which is right for a file a person maintains: there
the unknown key is nearly always a typo in a known one, and skipping it
means the setting the author wrote never takes effect.

### Marshal

Collections are written as indented blocks, two spaces per level; an
empty mapping or sequence is written `{}` or `[]`. Map keys are sorted -
numerically for numeric keys, lexically otherwise - so the same value
always produces the same bytes. Struct fields keep declaration order.

Strings are quoted whenever writing them bare would read back as
something else - a number, a boolean, a document marker, or a value some
other reader would split across lines. U+0085, U+2028 and U+2029 are
written as `\N`, `\L` and `\P` for that last reason: a reader that
honours them would see one scalar as two.

A string that is not valid UTF-8 is an error: YAML escapes name
characters, not bytes, so such a value has no spelling.

## Struct mapping

```go
type Source struct {
    Slug    string   `yaml:"slug"`
    Quality int      `yaml:"quality"`
    Tags    []string `yaml:"tags,omitempty"`
    Secret  string   `yaml:"-"`
    Note    string   // key "note"
}
```

- `yaml:"name"` sets the key.
- `,omitempty` drops the field when it is a zero value, an empty string,
  slice, map or array, or a nil pointer or interface.
- `yaml:"-"` skips the field in both directions.
- An untagged field answers to its **lower-cased** name, because YAML
  keys are conventionally lower case.
- An embedded struct with no tag contributes its fields directly, as in
  `encoding/json`. `,inline` on an embedded struct asks for the same
  thing and is accepted; on a named field it is refused, because the
  catch-all it asks for is not implemented and ignoring it would drop
  every key meant to land there.
- A field the struct declares itself wins over one of the same name
  reached through an embedded struct, whichever is written first.
- Any other tag option is refused rather than ignored.
- Key lookup is exact first, then case-insensitive.
- Keys the struct does not know are ignored, unless the document was
  read with `UnmarshalStrict`.

A type implementing `encoding.TextUnmarshaler` receives the scalar's
text as written, before any schema resolution - so a type of your own
decides what `2026-08-30` means. The same type implementing
`encoding.TextMarshaler` is written as the text it produces.

## Scalars

Plain (unquoted) scalars resolve by the YAML 1.2 core schema. Quoted and
block scalars are always strings.

| Form | Resolves to |
|---|---|
| empty, `~`, `null`, `Null`, `NULL` | null |
| `true`, `True`, `TRUE`, `false`, `False`, `FALSE` | bool |
| `42`, `-17`, `+5` | int |
| `0x1F`, `0o644` | int |
| `3.14`, `.5`, `1e3`, `.inf`, `-.inf`, `.nan` | float |
| anything else | string |

Two consequences are worth stating plainly, because they are where
readers of YAML most often disagree with each other.

**`yes`, `no`, `on`, `off` are strings.** The core schema has exactly two
booleans. A `bool` field still accepts those spellings, and so does an
explicit `!!bool`, because there the target states the intent; a field of
type `any` receives the string.

**A leading zero is refused.** `0644` is octal under YAML 1.1 and decimal
under 1.2. Both readings are defensible and they differ, so the parser
declines to choose:

```
yaml: line 1: "0644" is ambiguous: a leading zero meant octal in YAML 1.1
and decimal in 1.2; write 0o644 for octal, 644 for decimal, or quote it
for a string
```

Write `0o644`, `644`, or `"0644"`. `0`, `00` and `0x1F` are unaffected,
being the same number whichever way they are read.

Underscores in numbers (`1_000`) are not part of the core schema and
resolve as strings. Timestamps are strings too; give the field a type of
your own with an `UnmarshalText` method if you want them parsed.

A null cannot be a mapping key. `~: 1` is refused, because decoding it
would give the empty key, where it would collide with a real `"": 1` and
one of the two values would disappear without a word. Quoted, `"~"` is
ordinary text and works.

### Tags

`!!str`, `!!int`, `!!float`, `!!bool` and `!!null` force an
interpretation, and a value that does not fit the tag is an error rather
than a silent fallback. `!!float` accepts an integer spelling; `!!bool`
accepts the YAML 1.1 words.

## Structure

Supported: block mappings and sequences nested by indentation; flow
collections `[a, b]` and `{k: v}`, which may span lines; a single
`key: value` pair standing as a sequence entry (`[2, indent: 1]` is
`[2, {indent: 1}]`); comments; an optional leading `---` and trailing
`...`.

Indentation is spaces. A tab in indentation is an error, because a tab is
not valid YAML indentation and treating it as one is how two editors come
to disagree about a file. After an indicator a tab is ordinary
separation, so `key:<tab>value` and `-<tab>item` both read as expected.

### Block scalars

`|` keeps line breaks, `>` folds them into spaces. Both take the
chomping indicators `-` (strip the final break) and `+` (keep all trailing
breaks), and an explicit indentation digit.

Inside a folded scalar, a more-indented line keeps its own layout: the
breaks around it are not folded away. A blank line adds a break of its
own.

## Anchors, aliases and merge keys

`&name` marks a value, `*name` repeats it:

```yaml
defaults: &defaults
  retries: 3
  timeout: 30

fast:
  <<: *defaults
  timeout: 5      # fast.retries is 3, fast.timeout is 5
```

A `<<` key merges a mapping, or a sequence of mappings, into the mapping
that holds it. A key the mapping states itself always wins over a merged
one, and an earlier entry of a merge sequence wins over a later one: a
merge fills gaps and never overwrites.

An alias may only name an anchor that is already complete. That makes a
reference cycle unrepresentable, rather than something the decoder has to
detect.

## Limits

The decoder is meant to be safe on input written to attack it.

| Limit | Value | What it stops |
|---|---|---|
| nesting depth | 512 | a document that recurses until the stack ends |
| decode operations | 1,000,000 | aliases that expand a small document into a huge graph |
| alias direction | backward only | reference cycles |
| encoding | UTF-8 only | bytes that no reader could agree on |

Exceeding a limit is an ordinary error, not a panic.

## Errors

Every error names the line:

```
yaml: line 2: unterminated flow sequence
yaml: line 7: mapping key "slug" already defined at line 4
yaml: line 3: tab characters are not allowed in indentation
yaml: line 5: cannot decode string into int
```

The line is also reachable without reading the message, which is the
point of the two error types. A malformed document gives a
`*SyntaxError`; a document that is well formed but does not fit the
target gives a `*TypeError`:

```go
var se *yaml.SyntaxError
if errors.As(err, &se) {
    editor.Highlight(se.Line)
}
```

Duplicate mapping keys are an error, not a last-one-wins merge: in a
configuration file a repeated key is a mistake, and the silent version of
that mistake is the expensive one.

## Not supported

Refused with a descriptive error, rather than half-implemented:

- several documents in one input;
- `%YAML` and `%TAG` directives;
- custom and application tags, and `!!binary`;
- complex (non-scalar) mapping keys, including the `?` form;
- comment and formatting preservation.
