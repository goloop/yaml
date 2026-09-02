package yaml

import (
	"bytes"
	"io"
	"os"
	"reflect"
)

// Unmarshal parses the YAML document in data and stores the result in
// the value pointed to by v, which must be a non-nil pointer.
//
// The mapping follows encoding/json: a mapping fills a struct by field
// tag (`yaml:"name"`) or by lower-cased field name, a sequence fills a
// slice or array, and a scalar fills the matching basic type. A type
// that implements Unmarshaler decodes itself; one that implements
// encoding.TextUnmarshaler receives the scalar's text. Decoding into an
// interface value yields map[string]any, []any, string, bool, int64,
// float64 or nil.
//
// Keys the target does not know are ignored, as in encoding/json, unless
// WithStrict says otherwise. An empty document leaves v untouched.
//
// Errors are *SyntaxError when the document is malformed and *TypeError
// when it does not fit v; both carry the line number.
func Unmarshal(data []byte, v any, opts ...Option) error {
	return unmarshal(data, v, newSettings(opts...))
}

// UnmarshalFile reads the named file and decodes it into v. It is
// Unmarshal with the reading done, which is the shape a program that
// loads a configuration file actually wants.
func UnmarshalFile(filename string, v any, opts ...Option) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	return unmarshal(data, v, newSettings(opts...))
}

// UnmarshalReader reads r to the end and decodes it into v.
func UnmarshalReader(r io.Reader, v any, opts ...Option) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return unmarshal(data, v, newSettings(opts...))
}

// UnmarshalString decodes the document in s into v.
func UnmarshalString(s string, v any, opts ...Option) error {
	return unmarshal([]byte(s), v, newSettings(opts...))
}

func unmarshal(data []byte, v any, s settings) error {
	if v == nil {
		return ErrNilObject
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return ErrNotPointer
	}

	root, err := parse(data)
	if err != nil {
		return err
	}
	if root == nil {
		return nil
	}

	// Expansion runs on the parsed tree, not on the bytes: a variable
	// whose value holds a colon or a newline would otherwise change the
	// shape of the document rather than one value in it.
	if s.expand {
		if err := expandTree(root, s.expandStrict); err != nil {
			return err
		}
	}

	d := &decoder{s: s}
	return d.unmarshal(root, rv.Elem())
}

// Marshal returns the YAML encoding of v.
//
// Collections are written as indented blocks, two spaces to a level
// unless WithIndent says otherwise; an empty mapping or sequence is
// written as {} or []. Map keys are sorted, so the same value always
// produces the same bytes. Struct fields follow declaration order,
// honour `yaml:"name"` and `yaml:"name,omitempty"`, and are skipped
// entirely by `yaml:"-"`. A type that implements Marshaler stands in for
// itself; one that implements encoding.TextMarshaler is written as the
// text it produces.
//
// Strings are quoted whenever writing them bare would read back as
// something else - a number, a boolean, or a different string.
func Marshal(v any, opts ...Option) ([]byte, error) {
	s := newSettings(opts...)
	if s.indent < 1 {
		return nil, ErrInvalidObject
	}
	e := &encoder{s: s}
	if err := e.writeNode(reflect.ValueOf(v), 0, ""); err != nil {
		return nil, err
	}
	return []byte(e.buf.String()), nil
}

// MarshalFile writes the YAML encoding of v to the named file, with the
// permission bits WithFileMode sets and 0o644 by default.
func MarshalFile(filename string, v any, opts ...Option) error {
	s := newSettings(opts...)
	b, err := Marshal(v, opts...)
	if err != nil {
		return err
	}
	return os.WriteFile(filename, b, s.fileMode)
}

// MarshalWriter writes the YAML encoding of v to w.
func MarshalWriter(w io.Writer, v any, opts ...Option) error {
	b, err := Marshal(v, opts...)
	if err != nil {
		return err
	}
	// io.Copy reports a short write as io.ErrShortWrite, unlike a bare
	// w.Write whose n < len(b) result could be silently dropped.
	_, err = io.Copy(w, bytes.NewReader(b))
	return err
}

// MarshalString returns the YAML encoding of v as a string.
func MarshalString(v any, opts ...Option) (string, error) {
	b, err := Marshal(v, opts...)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
