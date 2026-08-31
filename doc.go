// Package yaml implements encoding and decoding of the de-facto
// configuration subset of YAML. The mapping between YAML and Go values
// mirrors the encoding/json package: Marshal and Unmarshal, struct field
// tags, encoding.TextMarshaler and encoding.TextUnmarshaler support.
//
//	type Config struct {
//		Name  string   `yaml:"name"`
//		Port  int      `yaml:"port"`
//		Tags  []string `yaml:"tags,omitempty"`
//	}
//
//	var c Config
//	err := yaml.Unmarshal(data, &c)
//
// UnmarshalStrict is the same, except that a key the target does not
// know is an error. For a file a person edits by hand that is usually
// what you want: an unknown key is nearly always a typo in a known one.
//
// Errors are *SyntaxError when the document is malformed and *TypeError
// when it does not fit the target. Both carry the line number.
//
// # Supported YAML
//
// The package covers the YAML that configuration and manifest files are
// actually made of:
//
//   - block mappings and sequences, nested by indentation;
//   - flow collections: [a, b] and {k: v};
//   - plain, single-quoted and double-quoted scalars, including
//     multi-line folding;
//   - literal (|) and folded (>) block scalars with chomping
//     indicators;
//   - comments, blank lines, an optional leading "---" and trailing
//     "...";
//   - anchors (&a) and aliases (*a), including "<<" merge keys;
//   - the core schema tags !!str, !!int, !!float, !!bool, !!null to
//     force a scalar interpretation.
//
// Scalars resolve by the YAML 1.2 core schema: null, true/false,
// integers (decimal, 0x hex, 0o octal), floats (including .inf and
// .nan) - everything else is a string. The YAML 1.1 booleans yes/no/on/off
// resolve as strings, but are accepted when decoding into a bool field.
//
// Deliberately out of scope: multiple documents in one input, %YAML and
// %TAG directives, custom and application tags, complex (non-scalar)
// mapping keys. Inputs that use them fail with a descriptive error
// rather than decoding to something unexpected.
//
// # Safety
//
// The decoder is meant to be safe on untrusted input: an alias may only
// reference an anchor that is already complete, which makes reference
// cycles unrepresentable; alias expansion is metered, so a small
// document cannot expand into a huge object graph; nesting depth is
// capped.
package yaml
