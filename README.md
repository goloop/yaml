[![deps.dev](https://img.shields.io/badge/deps.dev-insights-4c8dbc)](https://deps.dev/go/github.com%2Fgoloop%2Fyaml) [![Go Reference](https://pkg.go.dev/badge/github.com/goloop/yaml.svg)](https://pkg.go.dev/github.com/goloop/yaml) [![License](https://img.shields.io/badge/license-MIT-brightgreen?style=flat)](https://github.com/goloop/yaml/blob/master/LICENSE) [![Stay with Ukraine](https://img.shields.io/static/v1?label=Stay%20with&message=Ukraine%20♥&color=ffD700&labelColor=0057B8&style=flat)](https://u24.gov.ua/)

# yaml

`yaml` reads and writes the de-facto YAML configuration format, with the
API of `encoding/json`: `Marshal`, `Unmarshal`, struct tags,
`encoding.TextMarshaler` and `encoding.TextUnmarshaler` - plus the things
a configuration file actually needs: defaults, required keys, durations,
times, URLs, and environment expansion when you ask for it.

It is deliberately not a complete YAML 1.2 implementation. It covers what
configuration files, manifests and fixtures are actually made of, and it
refuses the rest with a message naming the line, instead of accepting a
document and quietly deciding what it meant.

```go
type Config struct {
    Name  string   `yaml:"name"`
    Port  int      `yaml:"port"`
    Tags  []string `yaml:"tags,omitempty"`
}

var c Config
if err := yaml.Unmarshal(data, &c); err != nil {
    return err
}
```

Loading a file a person maintains, with the strictness that suits one:

```go
type Config struct {
    Host    string        `yaml:"host" def:"localhost"`
    Port    int           `yaml:"port" def:"8080"`
    Secret  string        `yaml:"secret,required"`
    Timeout time.Duration `yaml:"timeout" def:"30s"`
    Since   time.Time     `yaml:"since" layout:"DateOnly"`
    API     url.URL       `yaml:"api"`
}

var c Config
err := yaml.UnmarshalFile("config.yaml", &c,
    yaml.WithStrict(),       // an unknown key is a typo, not a feature
    yaml.WithExpandStrict(), // ${DB_HOST}, and say so if it is not set
)
```

## Features

- The `encoding/json` mapping: struct tags, `omitempty`, `-`, embedded
  structs, case-insensitive fallback, unknown keys ignored.
- Block mappings and sequences, flow collections (`[a, b]`, `{k: v}`),
  every scalar style, literal (`|`) and folded (`>`) block scalars with
  chomping.
- Anchors (`&a`), aliases (`*a`) and `<<` merge keys.
- Core-schema tags `!!str`, `!!int`, `!!float`, `!!bool`, `!!null` to
  force an interpretation.
- Errors carry the line number in a typed value (`*SyntaxError`,
  `*TypeError`), because a config file that fails to load is read by a
  person looking for the mistake, and sentinel errors for `errors.Is`.
- `WithStrict` turns an unknown key into an error, which is what a
  hand-edited file wants: the unknown key is nearly always a typo.
- `def` defaults and `,required` keys, decided after `<<` merges, so a
  missing setting is caught at load time rather than at the first request
  that needs it.
- `time.Duration` from `30s`, `time.Time` with a per-field `layout`, and
  `url.URL` as the text it was written as.
- `WithExpand` substitutes `${NAME}` and `$NAME` from the environment,
  without the shell's positional parameters - so `cost: $100` and
  `pa$$word` stay what they are.
- `File`, `Reader`, `Writer` and `String` forms of both directions.
- `WithParser` and `WithEncoder` for a type that is not yours to change,
  and `UnmarshalYAML` / `MarshalYAML` for one that is.
- Deterministic output: the same value always encodes to the same bytes,
  so a generated file does not churn in version control.
- Safe on untrusted input: alias cycles are unrepresentable, alias
  expansion is metered, nesting is capped.
- Zero third-party dependencies.

## Installation

```shell
go get github.com/goloop/yaml
```

## Differences from YAML 1.1 readers

The core schema is YAML 1.2, and the places where 1.1 disagreed are the
places configuration files go wrong. Each of these is a decision, not an
oversight:

| Written | Here | Under YAML 1.1 |
|---|---|---|
| `yes`, `no`, `on`, `off` | string (a `bool` field still takes them) | boolean |
| `0644` | refused as ambiguous | octal, 420 |
| `1_000` | string | 1000 |
| `2026-08-30` | string (give the field a type with `UnmarshalText`) | timestamp |
| `~` as a mapping key | refused; it names nothing | dropped silently |
| embedded struct, no tag | flattened, as in `encoding/json` | nested under its type name |
| embedded struct, `,inline` | flattened | flattened |

## Three things worth knowing up front

**`yes` and `no` are strings.** Scalars resolve by the YAML 1.2 core
schema, where the booleans are `true` and `false` and nothing else. A
`bool` field still accepts `yes`/`no`/`on`/`off`, because there the
target says what was meant; a field of type `any` gets the string.

**A leading zero is refused.** `0644` meant octal under YAML 1.1 and
means decimal under 1.2 - a difference of a factor nobody notices until
the file permissions turn out wrong. Rather than pick a side quietly,
the parser refuses the spelling and asks for `0o644`, `644`, or quotes:

```
yaml: line 1: "0644" is ambiguous: a leading zero meant octal in YAML 1.1
and decimal in 1.2; write 0o644 for octal, 644 for decimal, or quote it
for a string
```

**A duration is a string.** `timeout: 30s`, not `timeout: 30`. A bare
number would mean nanoseconds, which in a configuration file is never
what anyone meant, so it is refused with a message saying how to write
it.

## Out of scope

These are refused with a descriptive error rather than half-supported:
several documents in one input, `%YAML` and `%TAG` directives, custom and
application tags, complex (non-scalar) mapping keys, and binary data.

## Documentation

Full reference: **[DOC.md](DOC.md)**. Ukrainian: **[DOC.UK.md](DOC.UK.md)**.

## License

MIT, see [LICENSE](LICENSE).
