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
// Each function has File, Reader and String forms, so loading a
// configuration file is one call: UnmarshalFile, UnmarshalReader,
// UnmarshalString, and MarshalFile, MarshalWriter, MarshalString.
//
// Errors are *SyntaxError when the document is malformed and *TypeError
// when it does not fit the target. Both carry the line number, and a
// *TypeError may also be one of the package's sentinel errors, so
// errors.Is and errors.As both answer without having to choose.
//
// # Options
//
// An option sets a call-level default that a field tag can override,
// so precedence runs field tag, then option, then built-in default.
//
//   - WithStrict makes a key the target does not know an error. For a
//     file a person edits by hand that is usually what you want: an
//     unknown key is nearly always a typo in a known one, and skipping
//     it means the setting the author wrote never takes effect.
//   - WithRequiredAll makes every leaf field required.
//   - WithExpand replaces ${NAME} and $NAME in scalars with the
//     environment; WithExpandStrict does the same and refuses a name
//     that is not set.
//   - WithTimeLayout sets the default layout for time.Time.
//   - WithFileMode sets the permissions MarshalFile creates a file with.
//   - WithIndent sets how far one level of nesting moves right.
//   - WithParser and WithEncoder register a conversion for a type that
//     cannot implement the text interfaces.
//
// # Struct tags
//
//   - yaml: the key name; "-" ignores the field; ",omitempty" drops an
//     empty value when encoding, ",required" refuses a document that
//     does not set the key, and ",inline" flattens an embedded struct.
//   - def: the value to use when the key is absent. An explicit null is
//     not absence: writing null is a decision, and a default that
//     overruled it would be overruling the person who wrote the file.
//   - layout: the layout for a time.Time field.
//
// # Supported types
//
// All sized int/uint, float32/64, string, bool, []byte, time.Duration,
// time.Time, url.URL, any type implementing Unmarshaler or
// encoding.TextUnmarshaler, nested structs, pointers, maps and
// slices/arrays of these.
//
// A duration is read from a string, "30s" or "1h30m". A bare number is
// refused: it would mean nanoseconds, and "timeout: 30" in a
// configuration file never means thirty nanoseconds.
//
// A url.URL is read with url.Parse and written as the text it came
// from. That is parsing, not validation: a relative reference such as
// "/callback" is a URL and stays one.
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
// # Expansion
//
// WithExpand replaces ${NAME} and $NAME in plain and double-quoted
// scalars with the matching environment variable. Single-quoted and
// block scalars are the literal forms and stay literal, which is how a
// value that must keep its "$" is written. Keys are never expanded.
//
// A name must read as a name: a letter or underscore, then letters,
// digits or underscores. Anything else keeps its "$", so a price
// ("cost: $100"), a shell one-liner ("$1 == x") and a password
// ("pa$$word") survive intact.
//
// Substitution happens after the parser has fixed the boundaries of a
// scalar and before its type is worked out. Both halves matter: a
// variable holding a colon or a newline changes one value and never the
// shape of the document, while "port: ${PORT}" still arrives as a
// number. In a flow collection the braced form has to be quoted, since
// "{" and "}" are YAML's own indicators there; the bare form does not.
//
// An unset name expands to nothing, which is quiet: the key is present,
// so a ",required" field is satisfied by a value that is not there.
// WithExpandStrict refuses it instead, naming the variable and the line.
//
// # Safety
//
// The decoder is meant to be safe on untrusted input: an alias may only
// reference an anchor that is already complete, which makes reference
// cycles unrepresentable; alias expansion is metered, so a small
// document cannot expand into a huge object graph; nesting depth is
// capped.
package yaml
