package yaml

import (
	"os"
	"reflect"
	"time"
)

// Option configures a Marshal or Unmarshal call. An option sets a
// call-level default that a per-field tag can override, so precedence
// runs field tag, then option, then the built-in default.
type Option func(*settings)

// settings is the resolved configuration of one call.
type settings struct {
	strict       bool // an unknown key is an error
	requireAll   bool // every leaf field is required
	expand       bool // replace ${VAR} and $VAR in scalar text
	expandStrict bool // an undefined variable is an error
	timeLayout   string
	fileMode     os.FileMode
	indent       int
	parsers      map[reflect.Type]func(string) (reflect.Value, error)
	encoders     map[reflect.Type]func(reflect.Value) (string, error)
}

// WithStrict makes a mapping key the target does not know an error
// rather than something to skip.
//
// Use it for a file a person maintains by hand. Ignoring unknown keys is
// right for a document that several versions of a program must read; it
// is wrong for a configuration file, where the unknown key is almost
// always a typo in a known one, and skipping it means the setting the
// author wrote simply never takes effect.
func WithStrict() Option {
	return func(s *settings) { s.strict = true }
}

// WithRequiredAll makes every leaf field required, as if each carried
// the ",required" flag: decoding fails when a field is absent from the
// document and has no def default.
//
// A nested struct is not itself required - it is filled by its own
// keys - so an optional section stays optional and only the fields of a
// section that is present must be there.
func WithRequiredAll() Option {
	return func(s *settings) { s.requireAll = true }
}

// WithExpand replaces ${NAME} and $NAME in scalar values with the
// matching environment variable, leaving a name that is not set empty.
//
// Only plain and double-quoted scalars are expanded. Single-quoted and
// block scalars are the literal forms and stay literal, which is also
// the only way to write a "$" that must survive. Keys are never
// expanded.
//
// An unset variable becoming empty text is quiet by design, and quiet is
// not always what a configuration wants: the key is present, so a
// ",required" field is satisfied by a value that is not there. Use
// WithExpandStrict when that matters.
func WithExpand() Option {
	return func(s *settings) { s.expand = true }
}

// WithExpandStrict is WithExpand, except that a reference to a variable
// that is not set is an error naming the variable and the line.
//
// It turns expansion on by itself: asking for the strict form of
// something that is off would have no meaning, and making the caller
// pass both options would be a trap worth nobody's time.
func WithExpandStrict() Option {
	return func(s *settings) { s.expand, s.expandStrict = true, true }
}

// WithTimeLayout sets the default layout for time.Time fields. It takes
// a Go reference-time layout or the name of a standard time constant
// (for example "DateOnly" or "RFC1123"). A per-field layout tag still
// wins; the built-in default is RFC3339.
func WithTimeLayout(layout string) Option {
	return func(s *settings) { s.timeLayout = layout }
}

// WithFileMode sets the permission bits MarshalFile creates a file with.
// The built-in default is 0o644; use 0o600 for a file that holds
// secrets.
func WithFileMode(mode os.FileMode) Option {
	return func(s *settings) { s.fileMode = mode }
}

// WithIndent sets how far one level of nesting moves right. The
// built-in default is two spaces, which is what YAML is conventionally
// written with. Zero or less is refused at encode time: a document
// indented by nothing is not a document.
func WithIndent(spaces int) Option {
	return func(s *settings) { s.indent = spaces }
}

// WithParser registers a decoder for fields of type T, and for elements
// of slices, arrays and pointers of T. It is the way in for a type that
// cannot implement encoding.TextUnmarshaler, and it wins over every
// built-in handling of that type - it would not be a way in otherwise.
// Pair it with WithEncoder to round-trip.
//
//	yaml.Unmarshal(data, &cfg, yaml.WithParser(func(s string) (Money, error) {
//		return ParseMoney(s)
//	}))
func WithParser[T any](parse func(string) (T, error)) Option {
	rt := reflect.TypeOf((*T)(nil)).Elem()
	return func(s *settings) {
		if s.parsers == nil {
			s.parsers = make(map[reflect.Type]func(string) (reflect.Value, error))
		}
		s.parsers[rt] = func(v string) (reflect.Value, error) {
			out, err := parse(v)
			return reflect.ValueOf(out), err
		}
	}
}

// WithEncoder registers an encoder for fields of type T, the counterpart
// of WithParser. It wins over every built-in handling of that type.
//
//	yaml.Marshal(cfg, yaml.WithEncoder(func(m Money) (string, error) {
//		return m.String(), nil
//	}))
func WithEncoder[T any](encode func(T) (string, error)) Option {
	rt := reflect.TypeOf((*T)(nil)).Elem()
	return func(s *settings) {
		if s.encoders == nil {
			s.encoders = make(map[reflect.Type]func(reflect.Value) (string, error))
		}
		s.encoders[rt] = func(rv reflect.Value) (string, error) {
			return encode(rv.Interface().(T))
		}
	}
}

// newSettings resolves the options into the settings one call runs with.
func newSettings(opts ...Option) settings {
	s := settings{fileMode: 0o644, indent: indentStep}
	for _, opt := range opts {
		opt(&s)
	}
	return s
}

// resolveLayout turns a layout name into a layout. The names are the
// standard library's own constants, so a config can say "DateOnly"
// instead of spelling the reference time out; anything else is taken as
// a literal layout. An empty value means RFC3339.
func resolveLayout(name string) string {
	switch name {
	case "", "RFC3339":
		return time.RFC3339
	case "RFC3339Nano":
		return time.RFC3339Nano
	case "RFC1123":
		return time.RFC1123
	case "RFC1123Z":
		return time.RFC1123Z
	case "RFC822":
		return time.RFC822
	case "RFC822Z":
		return time.RFC822Z
	case "RFC850":
		return time.RFC850
	case "ANSIC":
		return time.ANSIC
	case "UnixDate":
		return time.UnixDate
	case "Kitchen":
		return time.Kitchen
	case "Stamp":
		return time.Stamp
	case "DateTime":
		return time.DateTime
	case "DateOnly":
		return time.DateOnly
	case "TimeOnly":
		return time.TimeOnly
	default:
		return name
	}
}

// layoutFor picks the layout for a field: its own tag first, then the
// call's option, then RFC3339.
func (s settings) layoutFor(tag string) string {
	if tag != "" {
		return resolveLayout(tag)
	}
	return resolveLayout(s.timeLayout)
}
