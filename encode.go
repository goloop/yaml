package yaml

import (
	"encoding"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// indentStep is how far one level of nesting moves right. Two spaces is
// what YAML is conventionally written with, and tabs are not legal
// indentation at all.
const indentStep = 2

type encoder struct {
	buf   strings.Builder
	depth int
}

func (e *encoder) pad(n int) {
	for i := 0; i < n; i++ {
		e.buf.WriteByte(' ')
	}
}

// writeNode writes v at the current output position, which the caller
// has already moved to column indent. Lines after the first indent
// themselves.
func (e *encoder) writeNode(v reflect.Value, indent int) error {
	e.depth++
	defer func() { e.depth-- }()
	if e.depth > maxDepth {
		return fmt.Errorf("yaml: value nests deeper than %d levels", maxDepth)
	}

	v = deref(v)

	if txt, ok, err := scalarText(v); err != nil {
		return err
	} else if ok {
		e.buf.WriteString(txt)
		e.buf.WriteByte('\n')
		return nil
	}

	switch v.Kind() {
	case reflect.Map:
		return e.writeMap(v, indent)
	case reflect.Struct:
		return e.writeStruct(v, indent)
	case reflect.Slice, reflect.Array:
		return e.writeSeq(v, indent)
	}
	return fmt.Errorf("yaml: cannot encode %s", v.Type())
}

// writeEntry writes what follows a "key:" or a "-": a scalar continues
// the line, a collection starts on the next one.
func (e *encoder) writeEntry(v reflect.Value, indent int) error {
	v = deref(v)

	if txt, ok, err := scalarText(v); err != nil {
		return err
	} else if ok {
		e.buf.WriteByte(' ')
		e.buf.WriteString(txt)
		e.buf.WriteByte('\n')
		return nil
	}

	if empty, flow := emptyCollection(v); empty {
		e.buf.WriteByte(' ')
		e.buf.WriteString(flow)
		e.buf.WriteByte('\n')
		return nil
	}

	e.buf.WriteByte('\n')
	e.pad(indent + indentStep)
	return e.writeNode(v, indent+indentStep)
}

func (e *encoder) writeMap(v reflect.Value, indent int) error {
	if v.Len() == 0 {
		e.buf.WriteString("{}\n")
		return nil
	}
	keys, err := sortedKeys(v)
	if err != nil {
		return err
	}
	for i, k := range keys {
		if i > 0 {
			e.pad(indent)
		}
		txt, err := keyText(k)
		if err != nil {
			return err
		}
		e.buf.WriteString(txt)
		e.buf.WriteByte(':')
		if err := e.writeEntry(v.MapIndex(k), indent); err != nil {
			return err
		}
	}
	return nil
}

func (e *encoder) writeStruct(v reflect.Value, indent int) error {
	fs, err := cachedFields(v.Type())
	if err != nil {
		return err
	}
	written := 0
	for i := range fs.list {
		f := &fs.list[i]
		fv, err := fieldByIndex(v, f.index)
		if err != nil {
			return err
		}
		if f.omitEmpty && isEmptyValue(fv) {
			continue
		}
		if written > 0 {
			e.pad(indent)
		}
		written++
		name, err := plainOrQuoted(f.name)
		if err != nil {
			return err
		}
		e.buf.WriteString(name)
		e.buf.WriteByte(':')
		if err := e.writeEntry(fv, indent); err != nil {
			return err
		}
	}
	if written == 0 {
		e.buf.WriteString("{}\n")
	}
	return nil
}

func (e *encoder) writeSeq(v reflect.Value, indent int) error {
	if v.Len() == 0 {
		e.buf.WriteString("[]\n")
		return nil
	}
	for i := 0; i < v.Len(); i++ {
		if i > 0 {
			e.pad(indent)
		}
		e.buf.WriteString("- ")
		if err := e.writeNode(v.Index(i), indent+indentStep); err != nil {
			return err
		}
	}
	return nil
}

// deref follows pointers and interfaces down to the value that carries
// the content. A nil anywhere along the way stops it, so the caller sees
// an invalid value and writes a null.
func deref(v reflect.Value) reflect.Value {
	for {
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() {
				return reflect.Value{}
			}
			// A pointer that marshals itself is the value.
			if isTextMarshaler(v) {
				return v
			}
			v = v.Elem()
		case reflect.Interface:
			if v.IsNil() {
				return reflect.Value{}
			}
			v = v.Elem()
		default:
			return v
		}
	}
}

func isTextMarshaler(v reflect.Value) bool {
	if !v.CanInterface() {
		return false
	}
	_, ok := v.Interface().(encoding.TextMarshaler)
	return ok
}

// scalarText renders v as a single-line scalar. ok is false when v is a
// collection and needs a block of its own.
func scalarText(v reflect.Value) (string, bool, error) {
	if !v.IsValid() {
		return "null", true, nil
	}
	if v.CanInterface() {
		if m, ok := v.Interface().(encoding.TextMarshaler); ok {
			b, err := m.MarshalText()
			if err != nil {
				return "", false, fmt.Errorf("yaml: %w", err)
			}
			s, err := plainOrQuoted(string(b))
			return s, true, err
		}
	}

	switch v.Kind() {
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), true, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), true, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(v.Uint(), 10), true, nil
	case reflect.Float32, reflect.Float64:
		return floatText(v.Float(), v.Type().Bits()), true, nil
	case reflect.String:
		s, err := plainOrQuoted(v.String())
		return s, true, err
	case reflect.Slice:
		if v.IsNil() {
			return "null", true, nil
		}
		if v.Type().Elem().Kind() == reflect.Uint8 {
			s, err := plainOrQuoted(string(v.Bytes()))
			return s, true, err
		}
	case reflect.Map:
		if v.IsNil() {
			return "null", true, nil
		}
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return "", false, fmt.Errorf("yaml: cannot encode %s", v.Type())
	}
	return "", false, nil
}

func floatText(f float64, bits int) string {
	switch {
	case math.IsInf(f, 1):
		return ".inf"
	case math.IsInf(f, -1):
		return "-.inf"
	case math.IsNaN(f):
		return ".nan"
	}
	s := strconv.FormatFloat(f, 'g', -1, bits)
	// A float that formats without a dot or an exponent would read back
	// as an integer, so give it one.
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

func emptyCollection(v reflect.Value) (bool, string) {
	if !v.IsValid() {
		return false, ""
	}
	switch v.Kind() {
	case reflect.Map:
		return v.Len() == 0, "{}"
	case reflect.Slice, reflect.Array:
		return v.Len() == 0, "[]"
	case reflect.Struct:
		fs, err := cachedFields(v.Type())
		if err != nil {
			// The error surfaces from writeStruct, which is reached
			// straight after this test.
			return false, ""
		}
		return len(fs.list) == 0, "{}"
	}
	return false, ""
}

// keyText renders a map key. Keys are scalars, so anything a scalar can
// be is allowed, but a collection key is not representable here.
func keyText(k reflect.Value) (string, error) {
	txt, ok, err := scalarText(deref(k))
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("yaml: cannot use %s as a mapping key", k.Type())
	}
	return txt, nil
}

// sortedKeys orders map keys so that the output of a given value is
// always byte for byte the same: numbers by value, everything else by
// its rendered text.
func sortedKeys(v reflect.Value) ([]reflect.Value, error) {
	keys := v.MapKeys()
	switch v.Type().Key().Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		sort.Slice(keys, func(i, j int) bool { return keys[i].Int() < keys[j].Int() })
		return keys, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64, reflect.Uintptr:
		sort.Slice(keys, func(i, j int) bool { return keys[i].Uint() < keys[j].Uint() })
		return keys, nil
	case reflect.String:
		sort.Slice(keys, func(i, j int) bool {
			return keys[i].String() < keys[j].String()
		})
		return keys, nil
	}
	texts := make(map[int]string, len(keys))
	for i, k := range keys {
		t, err := keyText(k)
		if err != nil {
			return nil, err
		}
		texts[i] = t
	}
	idx := make([]int, len(keys))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return texts[idx[a]] < texts[idx[b]] })
	out := make([]reflect.Value, len(keys))
	for i, j := range idx {
		out[i] = keys[j]
	}
	return out, nil
}

// plainOrQuoted returns s as a YAML scalar, quoting it whenever writing
// it bare would read back as something other than this exact string.
//
// A string that is not valid UTF-8 has no YAML spelling at all: the
// double-quoted escapes name Unicode code points, not bytes, so a stray
// byte cannot be written and read back. Encoding fails rather than
// substituting replacement characters and losing the data silently.
func plainOrQuoted(s string) (string, error) {
	if !utf8.ValidString(s) {
		return "", fmt.Errorf(
			"yaml: cannot encode %q: a YAML document must be Unicode text", s)
	}
	if needsQuotes(s) {
		return quote(s), nil
	}
	return s, nil
}

func needsQuotes(s string) bool {
	if s == "" {
		return true
	}
	// Anything the core schema would read as a non-string has to be
	// quoted, or a number written from a string field comes back a
	// number.
	if k, _ := resolvePlain(s); k != stringScalar {
		return true
	}
	// And so does anything the parser would refuse outright: the encoder
	// must never write a document its own reader rejects.
	if _, ambiguous := ambiguousOctal(s); ambiguous {
		return true
	}
	// These stay strings under the core schema, but enough readers treat
	// them as booleans that writing them bare invites disagreement.
	switch strings.ToLower(s) {
	case "yes", "no", "on", "off", "y", "n":
		return true
	}
	if strings.TrimSpace(s) != s {
		return true
	}
	for _, r := range s {
		if breaksLine(r) {
			return true
		}
	}
	// A line that reads as a document marker is not a scalar at all.
	if s == "---" || s == "..." ||
		strings.HasPrefix(s, "--- ") || strings.HasPrefix(s, "... ") {
		return true
	}
	// An indicator in first position changes what the line is.
	switch s[0] {
	case '[', ']', '{', '}', ',', '#', '&', '*', '!', '|', '>', '\'', '"',
		'%', '@', '`':
		return true
	case '-', '?', ':':
		if len(s) == 1 || s[1] == ' ' {
			return true
		}
	}
	// ": " separates a key from a value and " #" starts a comment,
	// wherever they appear.
	if strings.Contains(s, ": ") || strings.HasSuffix(s, ":") {
		return true
	}
	if strings.Contains(s, " #") {
		return true
	}
	return false
}

// breaksLine reports characters that a reader may treat as ending the
// line. The ASCII controls are obvious; U+0085, U+2028 and U+2029 are
// not, and a reader that honours them would see a one-line scalar as
// two, so they can never be written bare.
func breaksLine(r rune) bool {
	return r < 0x20 || r == 0x7f || r == '\u0085' || r == '\u2028' || r == '\u2029'
}

func quote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\u0085':
			b.WriteString(`\N`)
		case '\u2028':
			b.WriteString(`\L`)
		case '\u2029':
			b.WriteString(`\P`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\x%02x`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return v.IsNil()
	}
	return false
}
