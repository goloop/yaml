package yaml

import (
	"encoding"
	"errors"
	"net/url"
	"reflect"
	"time"
)

// Unmarshaler is implemented by a type that decodes itself. The decode
// function it receives fills the value passed to it from the node the
// type was found at, so a type can look at the document as a string, a
// number or a whole structure and decide for itself.
//
//	func (l *Level) UnmarshalYAML(decode func(any) error) error {
//		var s string
//		if err := decode(&s); err != nil {
//			return err
//		}
//		return l.parse(s)
//	}
//
// The function may be called more than once, and each call decodes the
// same node again - which is how a type that accepts either a string or
// a number is written. Every call counts against the decoder's work
// budget, so a type cannot loop its way out of the limits that make
// decoding safe on untrusted input.
//
// The node itself is deliberately not passed. A type therefore cannot
// see whether its scalar was quoted, aliased or explicitly tagged; that
// is the price of not fixing the shape of this package's parse tree in
// its public API, where it could never be changed again.
type Unmarshaler interface {
	UnmarshalYAML(decode func(any) error) error
}

// Marshaler is implemented by a type that stands in for something else
// when encoded. It returns the value to write in its place.
//
//	func (l Level) MarshalYAML() (any, error) {
//		return l.String(), nil
//	}
//
// Returning the receiver itself is a loop, and the encoder reports it as
// an error rather than running out of stack.
type Marshaler interface {
	MarshalYAML() (any, error)
}

var (
	durationType        = reflect.TypeOf(time.Duration(0))
	timeType            = reflect.TypeOf(time.Time{})
	urlType             = reflect.TypeOf(url.URL{})
	textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
	textMarshalerType   = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	unmarshalerType     = reflect.TypeOf((*Unmarshaler)(nil)).Elem()
	marshalerType       = reflect.TypeOf((*Marshaler)(nil)).Elem()
)

// isSpecialType reports whether a type is one this package builds from a
// scalar itself, rather than field by field or through the text
// interfaces. These are the types a configuration file is full of and
// the standard library does not spell in YAML terms.
func isSpecialType(t reflect.Type) bool {
	return t == durationType || t == timeType || t == urlType
}

// baseType strips pointers, which is the type a registered parser or
// encoder is keyed by.
func baseType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// special decodes the scalar into one of the types this package knows by
// name. ok is false when v is not such a type.
func (d *decoder) special(n *node, v reflect.Value, layout string) (bool, error) {
	switch v.Type() {
	case durationType:
		kind, _, err := n.resolve()
		if err != nil {
			return true, err
		}
		// A bare number would be nanoseconds, and "timeout: 30" in a
		// configuration file never means thirty nanoseconds. Reading it
		// that way would be correct and useless: a timeout a thousand
		// million times shorter than intended fails in a way nobody
		// traces back to the config.
		if kind == intScalar || kind == floatScalar {
			return true, typeErr(n.line,
				"a duration must be written as a string like \"30s\" "+
					"(a bare number would mean nanoseconds)")
		}
		dur, err := time.ParseDuration(n.value)
		if err != nil {
			return true, typeErr(n.line, "%q is not a duration", n.value)
		}
		v.SetInt(int64(dur))
		return true, nil

	case timeType:
		l := d.s.layoutFor(layout)
		t, err := time.Parse(l, n.value)
		if err != nil {
			return true, typeErr(n.line,
				"%q is not a time in layout %q", n.value, l)
		}
		v.Set(reflect.ValueOf(t))
		return true, nil

	case urlType:
		// This is url.Parse, not URL validation: a relative reference
		// like "/callback" is a URL and stays one. A package that
		// insisted on a scheme and a host would be refusing input its
		// callers legitimately have.
		u, err := url.Parse(n.value)
		if err != nil {
			return true, typeErr(n.line, "%q is not a URL: %s", n.value, err)
		}
		v.Set(reflect.ValueOf(*u))
		return true, nil
	}
	return false, nil
}

// parser returns the parser registered for v's type, if any.
func (d *decoder) parser(t reflect.Type) (func(string) (reflect.Value, error), bool) {
	if len(d.s.parsers) == 0 {
		return nil, false
	}
	p, ok := d.s.parsers[baseType(t)]
	return p, ok
}

// applyParser fills v from the scalar using a parser the caller
// registered. A registered parser is the way in for a type that cannot
// implement the text interfaces, so it comes before everything this
// package would otherwise do with that type.
func (d *decoder) applyParser(n *node, v reflect.Value,
	p func(string) (reflect.Value, error)) error {
	if n.kind != scalarNode {
		return d.typeErr(n, v.Type(), "a collection")
	}
	_, out := indirect(v, n.isNull(), false)
	if !out.IsValid() {
		return nil
	}
	if n.isNull() {
		out.SetZero()
		return nil
	}
	got, err := p(n.value)
	if err != nil {
		return &TypeError{Line: n.line, Msg: err.Error()}
	}
	if !got.Type().AssignableTo(out.Type()) {
		return d.typeErr(n, out.Type(), got.Type().String())
	}
	out.Set(got)
	return nil
}

// indirectUnmarshaler walks down pointers, allocating as it goes, and
// reports an Unmarshaler if one turns up. It mirrors indirect, which
// does the same for the text interface.
func indirectUnmarshaler(v reflect.Value, isNull bool) (Unmarshaler, reflect.Value) {
	for {
		if v.Kind() == reflect.Interface && !v.IsNil() {
			e := v.Elem()
			if e.Kind() == reflect.Pointer && !e.IsNil() && e.CanSet() {
				v = e
				continue
			}
			break
		}
		if v.Kind() != reflect.Pointer {
			break
		}
		if isNull && v.CanSet() {
			return nil, v
		}
		if v.IsNil() {
			if !v.CanSet() {
				break
			}
			v.Set(reflect.New(v.Type().Elem()))
		}
		if v.Type().NumMethod() > 0 && v.CanInterface() {
			if u, ok := v.Interface().(Unmarshaler); ok {
				return u, reflect.Value{}
			}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Pointer && v.CanAddr() {
		if pv := v.Addr(); pv.Type().NumMethod() > 0 && pv.CanInterface() {
			if u, ok := pv.Interface().(Unmarshaler); ok {
				return u, reflect.Value{}
			}
		}
	}
	return nil, v
}

// hook hands the node to a type that decodes itself. The decode function
// it gets back decodes the same node again on every call, and every call
// is metered like any other, so a type cannot spend more of the budget
// than the document paid for.
func (d *decoder) hook(n *node, u Unmarshaler, layout string) error {
	decode := func(target any) error {
		if target == nil {
			return ErrNilObject
		}
		rv := reflect.ValueOf(target)
		if rv.Kind() != reflect.Pointer || rv.IsNil() {
			return ErrNotPointer
		}
		return d.unmarshalField(n, rv.Elem(), layout)
	}
	if err := u.UnmarshalYAML(decode); err != nil {
		// The type reports what it could not accept; the decoder knows
		// where it was, and losing the line would send the reader back
		// to searching the file by hand.
		var te *TypeError
		var se *SyntaxError
		if errors.As(err, &te) || errors.As(err, &se) {
			return err
		}
		return &TypeError{Line: n.line, Msg: err.Error()}
	}
	return nil
}
