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
func Unmarshal(data []byte, v any, opts ...Option) error
func UnmarshalFile(filename string, v any, opts ...Option) error
func UnmarshalReader(r io.Reader, v any, opts ...Option) error
func UnmarshalString(s string, v any, opts ...Option) error

func Marshal(v any, opts ...Option) ([]byte, error)
func MarshalFile(filename string, v any, opts ...Option) error
func MarshalWriter(w io.Writer, v any, opts ...Option) error
func MarshalString(v any, opts ...Option) (string, error)

type Option func(*settings)

type Unmarshaler interface{ UnmarshalYAML(decode func(any) error) error }
type Marshaler   interface{ MarshalYAML() (any, error) }

type SyntaxError struct{ Line int; Msg string }
type TypeError   struct{ Line int; Msg string }
```

The four forms of each direction differ only in where the bytes come
from or go. Everything else is expressed through options, Go types and
struct tags, as in `encoding/json`.

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

### Marshal

Collections are written as indented blocks, two spaces per level unless
`WithIndent` says otherwise; an
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

## Options

An option sets a call-level default that a field tag can override, so
precedence runs **field tag, then option, then built-in default**.

| Option | Effect |
|---|---|
| `WithStrict()` | a key the target does not know is an error |
| `WithRequiredAll()` | every leaf field is required |
| `WithExpand()` | replace `${NAME}` and `$NAME` from the environment |
| `WithExpandStrict()` | the same, and refuse a name that is not set |
| `WithTimeLayout(layout)` | default layout for `time.Time` (default RFC3339) |
| `WithFileMode(mode)` | permissions `MarshalFile` creates a file with (default 0o644) |
| `WithIndent(n)` | spaces per level of nesting (default 2) |
| `WithParser[T](fn)` | how to read a `T` from text |
| `WithEncoder[T](fn)` | how to write a `T` as text |

### WithStrict

`Unmarshal` ignores keys the target does not know, which is right for a
document several versions of a program must read. `WithStrict` makes them
an error, which is right for a file a person maintains: there the unknown
key is nearly always a typo in a known one, and skipping it means the
setting the author wrote never takes effect.

It also catches a document written in another language's conventions.
`//` is legal plain scalar text in YAML, so a file in the style of
commented JSON parses cleanly and means something nobody intended:

```yaml
{
  // service name
  "name": "app",
  "port": 8080
}
```

That document is valid. Its first key is the string
`// service name "name"`, and a plain decode fills `port` and leaves
`name` empty without a word. `WithStrict` reports it, with the line.

### WithTimeLayout

Takes a Go reference-time layout, or the name of a standard library
constant: `RFC3339`, `RFC3339Nano`, `RFC1123`, `RFC1123Z`, `RFC822`,
`RFC822Z`, `RFC850`, `ANSIC`, `UnixDate`, `Kitchen`, `Stamp`,
`DateTime`, `DateOnly`, `TimeOnly`.

### WithParser and WithEncoder

The way in for a type that cannot implement the text interfaces, usually
because it is not yours to change. A registered conversion wins over
everything this package would otherwise do with that type - it would not
be a way in otherwise - and applies to the type, to pointers to it and to
elements of slices and arrays of it.

```go
err := yaml.Unmarshal(data, &cfg, yaml.WithParser(func(s string) (Money, error) {
    return ParseMoney(s)
}))

out, err := yaml.Marshal(cfg, yaml.WithEncoder(func(m Money) (string, error) {
    return m.String(), nil
}))
```

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
- `,required` refuses a document that does not set the key.
- Any other tag option is refused rather than ignored.
- Key lookup is exact first, then case-insensitive.
- Keys the struct does not know are ignored, unless the document was read
  with `WithStrict`.

Two more tags travel with the field:

| Tag | Meaning |
|---|---|
| `def:"..."` | the value to use when the key is absent |
| `layout:"..."` | the layout for a `time.Time` field |

### Presence, defaults and required

These three settle in one order, and it is worth stating because the
answers follow from it:

1. merge keys (`<<`) are expanded;
2. presence is decided on the merged mapping;
3. an absent key takes its `def`;
4. a `required` key that is still absent is an error.

Presence is decided **after** the merge, so a key a merge supplied counts
as present - the decoder, a strict decode and `required` all work from
the same mapping, which is the only way they can agree on what it means
for a key to be there.

An explicit `null` is **not** absence:

```yaml
port: null      # with def:"8080" -> 0, the default does not apply
                # with ,required  -> satisfied, the key is there
```

Writing `null` is a decision. A default that overruled it would be
overruling the person who wrote the file, and `required` asks whether the
author addressed the setting, not whether the value is non-empty.

A field cannot carry both `,required` and `def`: it would have to be in
the document and have a value for when it is not. That contradiction is
reported when the struct is first used, not quietly resolved.

`WithRequiredAll` requires every **leaf**. A nested struct is filled by
its own keys, so it is not itself required, and an optional section that
is absent stays absent; the leaves of a section that is present must be
there.

## Types

Beyond the basic kinds, `[]byte` and nested structs, maps, pointers,
slices and arrays:

| Type | Read from | Written as |
|---|---|---|
| `time.Duration` | `"30s"`, `"1h30m"` | `30s` |
| `time.Time` | text in the field's layout | text in that layout |
| `url.URL` | text, via `url.Parse` | the URL's text |

A duration is read from a **string**. A bare number is refused:

```
yaml: line 4: a duration must be written as a string like "30s"
(a bare number would mean nanoseconds)
```

Reading `timeout: 30` as thirty nanoseconds would be correct and useless.
A timeout a thousand million times shorter than intended does not fail
where anybody traces it back to the configuration file.

A `url.URL` is parsed, not validated. A relative reference such as
`/callback` is a URL and stays one; this package does not insist on a
scheme and a host, because callers legitimately have input that has
neither.

Timestamps are not resolved implicitly: `at: 2026-09-01` is a string
under the core schema, and becomes a `time.Time` only because the field
says so. A field of type `any` receives the string, so a document decoded
into a struct and the same document decoded into `any` never disagree
about what a value is.

## Decoding a type yourself

A type that implements `Unmarshaler` receives its own node and decides
what to make of it:

```go
func (l *Level) UnmarshalYAML(decode func(any) error) error {
    var s string
    if err := decode(&s); err == nil {
        return l.parseName(s)
    }
    var n int
    if err := decode(&n); err != nil {
        return err
    }
    return l.parseNumber(n)
}
```

`decode` may be called more than once, and each call decodes the same
node again - which is how a type that accepts either a string or a number
is written. Every call counts against the decoder's limits, so a type
cannot loop its way past them.

The node itself is not passed. A type therefore cannot see whether its
scalar was quoted, aliased or explicitly tagged. That is deliberate: the
alternative fixes the shape of this package's parse tree in its public
API, where it could never be changed again.

`Marshaler` is the other direction, returning the value to write in
place of the receiver:

```go
func (l Level) MarshalYAML() (any, error) { return l.String(), nil }
```

A `MarshalYAML` that returns its own receiver is a loop. It is reported
as an error rather than run until the stack ends.

Order of precedence, in both directions: a registered parser or encoder,
then these interfaces, then the types in the table above, then
`encoding.TextUnmarshaler` / `encoding.TextMarshaler`, then the plain
kinds.

## Expansion

`WithExpand` replaces `${NAME}` and `$NAME` in scalar values with the
matching environment variable. It is off by default.

```yaml
host: ${DB_HOST}
url:  https://$DB_HOST/api
```

**Only values, never keys.** A key that depends on the environment makes
a document unreadable and breaks the duplicate check.

**Only plain and double-quoted scalars.** Single-quoted and block scalars
are YAML's literal forms and stay literal, which is how a value that must
keep its `$` is written:

```yaml
literal: '${AMOUNT}'
script: |
  echo ${AMOUNT}
```

**A name must read as a name**: a letter or underscore, then letters,
digits or underscores. Anything else keeps its `$`. This is why a price,
a shell one-liner and a password survive:

```yaml
price:    "cost: $100"   # unchanged
awk:      "$1 == x"      # unchanged
password: pa$$word       # unchanged, a doubled $ is never a reference
```

There is no escape for `$`, and deliberately so: a second escaping layer
on top of YAML's own would be a puzzle. The literal forms above are the
answer.

**Inside a flow collection the braced form must be quoted.** That is
YAML, not this package: `{` and `}` are flow indicators, so a plain
scalar there cannot contain `${...}` at all.

```yaml
urls: [https://${HOST}/a]     # error: expected ',' or ']'
urls: ["https://${HOST}/a"]   # fine
urls: [https://$HOST/a]       # fine
```

**Substitution happens after the parser has fixed the boundaries of a
scalar and before its type is worked out.** Both halves matter. A
variable holding a colon or a newline changes one value and never the
shape of the document, so a configuration file cannot be restructured by
its environment. And `port: ${PORT}` still arrives as a number, because
the text is resolved after it is substituted - which also means that
decoding into `any` gives whatever the substituted text spells.

**An unset name expands to nothing**, which is quiet: the key is present,
so a `,required` field is satisfied by a value that is not there.

```yaml
database_url: ${DATABASE_URL}   # unset -> "" , and required is satisfied
```

`WithExpandStrict` refuses it instead, naming the variable and the line.
It turns expansion on by itself.

`Marshal` never folds a value back into `${NAME}`. A round trip through
`WithExpand` is asymmetric by design.

A `def` default is expanded on the same terms as a value from the file.

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

A `*TypeError` may also be one of the package's sentinel errors, so
`errors.Is` and `errors.As` both answer without having to choose between
what went wrong and where:

```go
var (
    ErrNilObject     // Unmarshal or Marshal was given nil
    ErrNotPointer    // Unmarshal needs a non-nil pointer
    ErrNotStruct     // a struct was required
    ErrInvalidObject // the value cannot be encoded
    ErrRequired      // a required key is not set
)

if errors.Is(err, yaml.ErrRequired) { ... }
```

The names match the ones the `.env` side of this toolkit uses, so the
same check reads the same way whichever format a program loads its
configuration from.

## Not supported

Refused with a descriptive error, rather than half-implemented:

- several documents in one input;
- `%YAML` and `%TAG` directives;
- custom and application tags, and `!!binary`;
- complex (non-scalar) mapping keys, including the `?` form;
- comment and formatting preservation.
