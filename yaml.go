package yaml

import (
	"fmt"
	"reflect"
)

// Unmarshal parses the YAML document in data and stores the result in
// the value pointed to by v, which must be a non-nil pointer.
//
// The mapping follows encoding/json: a mapping fills a struct by field
// tag (`yaml:"name"`) or by lower-cased field name, a sequence fills a
// slice or array, and a scalar fills the matching basic type. A type
// that implements encoding.TextUnmarshaler receives the scalar's text.
// Decoding into an interface value yields map[string]any, []any, string,
// bool, int64, float64 or nil.
//
// Keys the target does not know are ignored, as in encoding/json.
//
// An empty document leaves v untouched.
func Unmarshal(data []byte, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("yaml: Unmarshal needs a non-nil pointer, got %T", v)
	}
	root, err := parse(data)
	if err != nil {
		return err
	}
	if root == nil {
		return nil
	}
	d := &decoder{}
	return d.unmarshal(root, rv.Elem())
}

// Marshal returns the YAML encoding of v.
//
// Collections are written as indented blocks, two spaces to a level; an
// empty mapping or sequence is written as {} or []. Map keys are sorted,
// so the same value always produces the same bytes. Struct fields follow
// declaration order, honour `yaml:"name"` and `yaml:"name,omitempty"`,
// and are skipped entirely by `yaml:"-"`. A type that implements
// encoding.TextMarshaler is written as the text it produces.
//
// Strings are quoted whenever writing them bare would read back as
// something else - a number, a boolean, or a different string.
func Marshal(v any) ([]byte, error) {
	e := &encoder{}
	if err := e.writeNode(reflect.ValueOf(v), 0); err != nil {
		return nil, err
	}
	return []byte(e.buf.String()), nil
}
