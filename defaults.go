package yaml

import "reflect"

// absent settles the fields the document never mentioned. A def default
// fills one, a required flag refuses one, and anything else is left at
// whatever the caller had there.
//
// It runs after the mapping has been walked, so presence means presence
// in the merged mapping - the same view the decoder and a strict decode
// work from.
func (d *decoder) absent(n *node, v reflect.Value, fs *fields, filled []bool) error {
	for i := range fs.list {
		if filled[i] {
			continue
		}
		f := &fs.list[i]

		if f.hasDef {
			fv, err := fieldByIndex(v, f.index)
			if err != nil {
				return err
			}
			if err := d.applyDef(n, f, fv); err != nil {
				return err
			}
			continue
		}

		// A section is filled by its own keys, so requiring one would
		// mean requiring the whole subtree; only leaves are required.
		// An explicit ",required" still means what it says.
		if f.required || (d.s.requireAll && !isSection(f.typ)) {
			return sentinelErr(n.line, ErrRequired,
				"%s: key %q is required and the document does not set it",
				v.Type(), f.name)
		}
	}
	return nil
}

// applyDef decodes a def tag into the field as if the document had
// carried it as a plain scalar. Going through the same path is the
// point: a default is written by the same person, in the same language,
// and gets the same expansion and the same type resolution as a value
// that was in the file.
func (d *decoder) applyDef(n *node, f *field, fv reflect.Value) error {
	text := f.def
	if d.s.expand {
		var err error
		if text, err = expand(text, n.line, d.s.expandStrict); err != nil {
			return err
		}
	}

	syn := &node{
		kind:  scalarNode,
		style: stylePlain,
		value: text,
		line:  n.line,
	}
	if err := d.unmarshalField(syn, fv, f.layout); err != nil {
		return typeErr(n.line, "def for key %q: %s", f.name, unwrapMsg(err))
	}
	return nil
}

// isSection reports whether a field is filled key by key rather than
// from one scalar. Pointers are followed, and a type that builds itself
// from text is a leaf however many fields it happens to have.
func isSection(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return false
	}
	if isSpecialType(t) {
		return false
	}
	return !reflect.PointerTo(t).Implements(textUnmarshalerType) &&
		!reflect.PointerTo(t).Implements(unmarshalerType)
}

// unwrapMsg is the message of an error without the "yaml: line N: "
// prefix, so a wrapping error does not print the position twice.
func unwrapMsg(err error) string {
	switch e := err.(type) {
	case *TypeError:
		return e.Msg
	case *SyntaxError:
		return e.Msg
	}
	return err.Error()
}
