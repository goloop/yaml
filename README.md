[![deps.dev](https://img.shields.io/badge/deps.dev-insights-4c8dbc)](https://deps.dev/go/github.com%2Fgoloop%2Fyaml) [![Go Reference](https://pkg.go.dev/badge/github.com/goloop/yaml.svg)](https://pkg.go.dev/github.com/goloop/yaml) [![License](https://img.shields.io/badge/license-MIT-brightgreen?style=flat)](https://github.com/goloop/yaml/blob/master/LICENSE) [![Stay with Ukraine](https://img.shields.io/static/v1?label=Stay%20with&message=Ukraine%20♥&color=ffD700&labelColor=0057B8&style=flat)](https://u24.gov.ua/)

# yaml

`yaml` reads and writes the de-facto YAML configuration format, with the
API of `encoding/json`: `Marshal`, `Unmarshal`, struct tags,
`encoding.TextMarshaler` and `encoding.TextUnmarshaler`.

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

## Features

- The `encoding/json` mapping: struct tags, `omitempty`, `-`, embedded
  structs, case-insensitive fallback, unknown keys ignored.
- Block mappings and sequences, flow collections (`[a, b]`, `{k: v}`),
  every scalar style, literal (`|`) and folded (`>`) block scalars with
  chomping.
- Anchors (`&a`), aliases (`*a`) and `<<` merge keys.
- Core-schema tags `!!str`, `!!int`, `!!float`, `!!bool`, `!!null` to
  force an interpretation.
- Errors carry the line number, because a config file that fails to load
  is read by a person looking for the mistake.
- Deterministic output: the same value always encodes to the same bytes,
  so a generated file does not churn in version control.
- Safe on untrusted input: alias cycles are unrepresentable, alias
  expansion is metered, nesting is capped.
- Zero third-party dependencies.

## Installation

```shell
go get github.com/goloop/yaml
```

## Two things worth knowing up front

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

## Out of scope

These are refused with a descriptive error rather than half-supported:
several documents in one input, `%YAML` and `%TAG` directives, custom and
application tags, complex (non-scalar) mapping keys, and binary data.

## Documentation

Full reference: **[DOC.md](DOC.md)**. Ukrainian: **[DOC.UK.md](DOC.UK.md)**.

## License

MIT, see [LICENSE](LICENSE).
