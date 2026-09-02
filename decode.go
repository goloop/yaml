package yaml

import (
	"encoding"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// decoder carries the limits that make decoding safe on untrusted input.
// Aliases are shared pointers, so a small document can describe a very
// large tree; ops counts what is actually visited and stops it.
type decoder struct {
	ops   int
	depth int
	s     settings
}

func (d *decoder) step() error {
	d.ops++
	if d.ops > maxDecodeOps {
		return fmt.Errorf("yaml: document expands to more than %d values", maxDecodeOps)
	}
	return nil
}

func (d *decoder) typeErr(n *node, t reflect.Type, what string) error {
	return typeErr(n.line, "cannot decode %s into %s", what, t)
}

// unmarshal writes the node into v, which must be settable.
func (d *decoder) unmarshal(n *node, v reflect.Value) error {
	return d.unmarshalField(n, v, "")
}

// unmarshalField is unmarshal with the layout a field's tag asked for.
// The layout travels into the elements of a sequence, because a list of
// times is one field with one format, but not into the fields of a
// nested struct, which carry tags of their own.
//
// The order here is the contract: a parser the caller registered wins
// over everything, then a type that decodes itself, then the types this
// package knows by name, then the text interface, then the plain kinds.
// A registered parser is the way in for a type that can do none of the
// above, so anything overtaking it would close the only door.
func (d *decoder) unmarshalField(n *node, v reflect.Value, layout string) error {
	if err := d.step(); err != nil {
		return err
	}
	d.depth++
	defer func() { d.depth-- }()
	if d.depth > maxDepth {
		return typeErr(n.line, "nesting is too deep")
	}

	if p, ok := d.parser(v.Type()); ok {
		return d.applyParser(n, v, p)
	}
	if hu, _ := indirectUnmarshaler(v, n.isNull()); hu != nil {
		return d.hook(n, hu, layout)
	}

	// time.Time implements the text interface, and taking that path
	// would pin it to RFC3339 and quietly ignore the layout the field
	// asked for. The types this package names are settled here instead.
	if n.kind == scalarNode && isSpecialType(baseType(v.Type())) {
		_, out := indirect(v, n.isNull(), false)
		if !out.IsValid() {
			return nil
		}
		if n.isNull() {
			out.SetZero()
			return nil
		}
		if ok, err := d.special(n, out, layout); ok {
			return err
		}
	}

	u, out := indirect(v, n.isNull(), n.kind == scalarNode)
	if u != nil {
		return d.text(n, u)
	}
	if !out.IsValid() {
		return nil
	}
	if n.isNull() {
		out.SetZero()
		return nil
	}

	switch n.kind {
	case scalarNode:
		return d.scalar(n, out, layout)
	case seqNode:
		return d.sequence(n, out, layout)
	case mapNode:
		return d.mapping(n, out, layout)
	}
	return nil
}

// text hands a scalar to a TextUnmarshaler. Non-scalars never reach
// here: a type that parses text has nothing to do with a mapping.
func (d *decoder) text(n *node, u encoding.TextUnmarshaler) error {
	if err := u.UnmarshalText([]byte(n.value)); err != nil {
		return &TypeError{Line: n.line, Msg: err.Error()}
	}
	return nil
}

// indirect walks down pointers and interfaces, allocating as it goes,
// and reports a TextUnmarshaler if one turns up on the way. A null node
// stops the walk at the first settable pointer, so it can be nilled
// rather than pointed at a zero value.
func indirect(v reflect.Value, isNull, allowText bool) (encoding.TextUnmarshaler, reflect.Value) {
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
		if allowText && v.Type().NumMethod() > 0 && v.CanInterface() {
			if u, ok := v.Interface().(encoding.TextUnmarshaler); ok {
				return u, reflect.Value{}
			}
		}
		v = v.Elem()
	}
	if allowText && v.Kind() != reflect.Pointer && v.CanAddr() {
		if pv := v.Addr(); pv.Type().NumMethod() > 0 && pv.CanInterface() {
			if u, ok := pv.Interface().(encoding.TextUnmarshaler); ok {
				return u, reflect.Value{}
			}
		}
	}
	return nil, v
}

// isTextTarget reports whether v declares that it is built from text.
func isTextTarget(v reflect.Value) bool {
	if v.CanAddr() {
		if pv := v.Addr(); pv.Type().NumMethod() > 0 && pv.CanInterface() {
			if _, ok := pv.Interface().(encoding.TextUnmarshaler); ok {
				return true
			}
		}
	}
	return false
}

func (d *decoder) scalar(n *node, v reflect.Value, layout string) error {
	// The types this package builds from a scalar itself are checked
	// before the kinds, because time.Duration is an int64 and would
	// otherwise be filled with a raw count of nanoseconds.
	if ok, err := d.special(n, v, layout); ok {
		return err
	}

	kind, val, err := n.resolve()
	if err != nil {
		return err
	}

	switch v.Kind() {
	case reflect.Interface:
		if v.NumMethod() != 0 {
			return d.typeErr(n, v.Type(), kind.String())
		}
		if val == nil {
			v.SetZero()
			return nil
		}
		v.Set(reflect.ValueOf(val))
		return nil

	case reflect.String:
		// Every scalar can become a string: in YAML the type is inferred
		// from spelling, not declared, so a field asking for a string
		// wants the text as written - which is what keeps an unquoted
		// postcode like 01234 usable.
		v.SetString(n.value)
		return nil

	case reflect.Bool:
		if kind == boolScalar {
			v.SetBool(val.(bool))
			return nil
		}
		// yes/no/on/off resolve as strings under the core schema, but a
		// bool field says plainly what was meant.
		if n.style == stylePlain {
			if b, ok := looksBool(n.value); ok {
				v.SetBool(b)
				return nil
			}
		}
		return d.typeErr(n, v.Type(), kind.String())

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, ok := val.(int64)
		if !ok {
			return d.typeErr(n, v.Type(), kind.String())
		}
		if v.OverflowInt(i) {
			return typeErr(n.line, "%d overflows %s", i, v.Type())
		}
		v.SetInt(i)
		return nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64, reflect.Uintptr:
		i, ok := val.(int64)
		if !ok {
			return d.typeErr(n, v.Type(), kind.String())
		}
		if i < 0 {
			return typeErr(n.line, "%d is negative, %s is not", i, v.Type())
		}
		if v.OverflowUint(uint64(i)) {
			return typeErr(n.line, "%d overflows %s", i, v.Type())
		}
		v.SetUint(uint64(i))
		return nil

	case reflect.Float32, reflect.Float64:
		switch f := val.(type) {
		case float64:
			v.SetFloat(f)
			return nil
		case int64:
			v.SetFloat(float64(f))
			return nil
		}
		return d.typeErr(n, v.Type(), kind.String())

	case reflect.Slice:
		// []byte takes the scalar's text, as encoding/json does.
		if v.Type().Elem().Kind() == reflect.Uint8 {
			v.SetBytes([]byte(n.value))
			return nil
		}
	}
	return d.typeErr(n, v.Type(), kind.String())
}

func (d *decoder) sequence(n *node, v reflect.Value, layout string) error {
	switch v.Kind() {
	case reflect.Interface:
		if v.NumMethod() != 0 {
			return d.typeErr(n, v.Type(), "a sequence")
		}
		out := make([]any, len(n.items))
		for i, item := range n.items {
			if err := d.unmarshalField(item,
				reflect.ValueOf(&out[i]).Elem(), layout); err != nil {
				return err
			}
		}
		v.Set(reflect.ValueOf(out))
		return nil

	case reflect.Slice:
		out := reflect.MakeSlice(v.Type(), len(n.items), len(n.items))
		for i, item := range n.items {
			if err := d.unmarshalField(item, out.Index(i), layout); err != nil {
				return err
			}
		}
		v.Set(out)
		return nil

	case reflect.Array:
		if len(n.items) > v.Len() {
			return typeErr(n.line, "sequence of %d does not fit %s",
				len(n.items), v.Type())
		}
		for i, item := range n.items {
			if err := d.unmarshalField(item, v.Index(i), layout); err != nil {
				return err
			}
		}
		for i := len(n.items); i < v.Len(); i++ {
			v.Index(i).SetZero()
		}
		return nil
	}
	return d.typeErr(n, v.Type(), "a sequence")
}

func (d *decoder) mapping(n *node, v reflect.Value, layout string) error {
	keys, vals, err := d.merged(n)
	if err != nil {
		return err
	}

	switch v.Kind() {
	case reflect.Interface:
		if v.NumMethod() != 0 {
			return d.typeErr(n, v.Type(), "a mapping")
		}
		out := make(map[string]any, len(keys))
		for i, k := range keys {
			var val any
			if err := d.unmarshal(vals[i], reflect.ValueOf(&val).Elem()); err != nil {
				return err
			}
			out[k.value] = val
		}
		v.Set(reflect.ValueOf(out))
		return nil

	case reflect.Map:
		if v.IsNil() {
			v.Set(reflect.MakeMap(v.Type()))
		}
		kt, et := v.Type().Key(), v.Type().Elem()
		for i, k := range keys {
			kv := reflect.New(kt).Elem()
			if err := d.unmarshal(k, kv); err != nil {
				return err
			}
			ev := reflect.New(et).Elem()
			if err := d.unmarshalField(vals[i], ev, layout); err != nil {
				return err
			}
			v.SetMapIndex(kv, ev)
		}
		return nil

	case reflect.Struct:
		// A type that parses itself from text has no business being
		// filled key by key: reaching here means the document holds a
		// mapping where the target wanted a scalar, and every key would
		// be silently dropped as "unknown".
		if isTextTarget(v) {
			return d.typeErr(n, v.Type(), "a mapping")
		}
		fields, err := cachedFields(v.Type())
		if err != nil {
			return err
		}
		// Presence is decided here, after merged() has expanded any
		// "<<" keys: the decoder, a strict decode and the required flag
		// must all be looking at the same mapping, or they would
		// disagree about what it means for a key to be there.
		filled := make([]bool, len(fields.list))
		for i, k := range keys {
			fi := fields.lookupIndex(k.value)
			if fi < 0 {
				if d.s.strict {
					return typeErr(k.line,
						"unknown key %q for %s", k.value, v.Type())
				}
				// Unknown keys are ignored, as in encoding/json: a
				// config file may carry keys this build does not know.
				continue
			}
			filled[fi] = true
			fv, err := fieldByIndex(v, fields.list[fi].index)
			if err != nil {
				return err
			}
			if err := d.unmarshalField(vals[i], fv,
				fields.list[fi].layout); err != nil {
				return err
			}
		}
		return d.absent(n, v, fields, filled)
	}
	return d.typeErr(n, v.Type(), "a mapping")
}

// merged returns the mapping's pairs with "<<" merge keys expanded.
// A key the mapping states itself always wins over a merged one, and an
// earlier entry of a merge sequence wins over a later one - so a merge
// fills gaps and never overwrites.
func (d *decoder) merged(n *node) ([]*node, []*node, error) {
	var merges []*node
	keys := make([]*node, 0, len(n.keys))
	vals := make([]*node, 0, len(n.vals))
	seen := make(map[string]bool, len(n.keys))

	for i, k := range n.keys {
		if err := d.step(); err != nil {
			return nil, nil, err
		}
		if k.kind != scalarNode {
			return nil, nil, syntaxErr(k.line, "mapping keys must be scalars")
		}
		// A null names nothing. Letting it through would decode to the
		// empty key, where it would collide with a real "" key and one
		// of the two values would vanish without a word.
		if k.isNull() {
			return nil, nil, syntaxErr(k.line,
				"a null mapping key has no name; quote it to use it as text")
		}
		if k.value == "<<" && k.style == stylePlain && k.tag == "" {
			merges = append(merges, n.vals[i])
			continue
		}
		if seen[k.value] {
			continue
		}
		seen[k.value] = true
		keys = append(keys, k)
		vals = append(vals, n.vals[i])
	}

	for _, m := range merges {
		sources, err := mergeSources(m)
		if err != nil {
			return nil, nil, err
		}
		for _, src := range sources {
			mk, mv, err := d.merged(src)
			if err != nil {
				return nil, nil, err
			}
			for i, k := range mk {
				if seen[k.value] {
					continue
				}
				seen[k.value] = true
				keys = append(keys, k)
				vals = append(vals, mv[i])
			}
		}
	}
	return keys, vals, nil
}

func mergeSources(m *node) ([]*node, error) {
	switch m.kind {
	case mapNode:
		return []*node{m}, nil
	case seqNode:
		out := make([]*node, 0, len(m.items))
		for _, it := range m.items {
			if it.kind != mapNode {
				return nil, syntaxErr(it.line,
					"a merge key takes a mapping or a sequence of mappings")
			}
			out = append(out, it)
		}
		return out, nil
	}
	return nil, syntaxErr(m.line,
		"a merge key takes a mapping or a sequence of mappings")
}

// fieldByIndex walks to a nested field, allocating the pointers to
// embedded structs that stand between v and it.
func fieldByIndex(v reflect.Value, index []int) (reflect.Value, error) {
	for i, x := range index {
		if i > 0 {
			if v.Kind() == reflect.Pointer {
				if v.IsNil() {
					if !v.CanSet() {
						return reflect.Value{}, fmt.Errorf(
							"yaml: cannot set embedded field through %s", v.Type())
					}
					v.Set(reflect.New(v.Type().Elem()))
				}
				v = v.Elem()
			}
		}
		v = v.Field(x)
	}
	return v, nil
}

// field describes one struct field the decoder and encoder can address.
type field struct {
	name      string
	index     []int
	omitEmpty bool
	required  bool   // the key must be in the document
	def       string // def tag: value used when the key is absent
	hasDef    bool   // the def tag was present, even if empty
	layout    string // layout tag: time.Time layout for this field
	typ       reflect.Type
	depth     int // embedding levels below the outer struct
}

type fields struct {
	list  []field
	byKey map[string]int
}

// lookup finds the field for a YAML key: exactly first, then ignoring
// case, the way encoding/json resolves names.
func (f *fields) lookup(name string) *field {
	if i := f.lookupIndex(name); i >= 0 {
		return &f.list[i]
	}
	return nil
}

// lookupIndex finds the field for a YAML key: exactly first, then
// ignoring case, the way encoding/json resolves names. It returns -1
// when the target has no such field.
func (f *fields) lookupIndex(name string) int {
	if i, ok := f.byKey[name]; ok {
		return i
	}
	for i := range f.list {
		if strings.EqualFold(f.list[i].name, name) {
			return i
		}
	}
	return -1
}

type cachedType struct {
	fields *fields
	err    error
}

var fieldCache sync.Map // reflect.Type -> *cachedType

func cachedFields(t reflect.Type) (*fields, error) {
	if c, ok := fieldCache.Load(t); ok {
		e := c.(*cachedType)
		return e.fields, e.err
	}
	f, err := typeFields(t)
	fieldCache.Store(t, &cachedType{fields: f, err: err})
	return f, err
}

// typeFields flattens a struct into the keys it answers to. An embedded
// struct with no name of its own contributes its fields directly.
//
// Depth decides collisions: a field the struct declares itself always
// wins over one of the same name reached through an embedded struct, no
// matter which is met first. That is what encoding/json does, and a
// struct that behaves differently under the two encoders would be a trap
// rather than a feature.
func typeFields(t reflect.Type) (*fields, error) {
	var found []field
	var walk func(t reflect.Type, index []int, depth int) error

	walk = func(t reflect.Type, index []int, depth int) error {
		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			tag := sf.Tag.Get("yaml")
			if tag == "-" {
				continue
			}
			name, opts := splitTag(tag)

			embedded := false
			var et reflect.Type
			if sf.Anonymous && name == "" {
				et = sf.Type
				if et.Kind() == reflect.Pointer {
					et = et.Elem()
				}
				embedded = et.Kind() == reflect.Struct
			}
			if err := opts.check(t, sf.Name, embedded); err != nil {
				return err
			}
			if embedded {
				if err := walk(et, append(append([]int{}, index...), i),
					depth+1); err != nil {
					return err
				}
				continue
			}
			if !sf.IsExported() {
				continue
			}
			if name == "" {
				// Lower case by default: YAML keys are conventionally
				// lower case, and a struct without tags should still
				// read a normal config file.
				name = strings.ToLower(sf.Name)
			}
			def, hasDef := sf.Tag.Lookup("def")
			required := opts.has("required")
			if required && hasDef {
				return fmt.Errorf(
					"yaml: %s.%s: \"required\" and a def default contradict "+
						"each other: a field cannot both have to be in the "+
						"document and have a value for when it is not",
					t.Name(), sf.Name)
			}

			found = append(found, field{
				name:      name,
				index:     append(append([]int{}, index...), i),
				omitEmpty: opts.has("omitempty"),
				required:  required,
				def:       def,
				hasDef:    hasDef,
				layout:    sf.Tag.Get("layout"),
				typ:       sf.Type,
				depth:     depth,
			})
		}
		return nil
	}

	if err := walk(t, nil, 0); err != nil {
		return nil, err
	}

	// Shallowest wins, and among equals the one declared first. Sorting
	// is stable so declaration order survives for everything else, which
	// is the order Marshal writes fields in.
	sort.SliceStable(found, func(i, j int) bool {
		return found[i].depth < found[j].depth
	})

	out := &fields{byKey: make(map[string]int, len(found))}
	for _, f := range found {
		if _, dup := out.byKey[f.name]; dup {
			continue
		}
		out.byKey[f.name] = len(out.list)
		out.list = append(out.list, f)
	}
	// Writing follows declaration order, not the depth order used to
	// resolve collisions.
	sort.SliceStable(out.list, func(i, j int) bool {
		return lessIndex(out.list[i].index, out.list[j].index)
	})
	for i := range out.list {
		out.byKey[out.list[i].name] = i
	}
	return out, nil
}

// lessIndex orders fields the way they are laid out in the struct, so an
// embedded struct's fields appear where the embedding is declared.
func lessIndex(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

type tagOptions string

func splitTag(tag string) (string, tagOptions) {
	name, opts, _ := strings.Cut(tag, ",")
	return name, tagOptions(opts)
}

// check refuses options this package does not implement. Silently
// ignoring one is how a tag comes to drop half a config file without a
// word: the author wrote an instruction and nothing carried it out.
func (o tagOptions) check(t reflect.Type, fieldName string, embedded bool) error {
	rest := string(o)
	for rest != "" {
		var opt string
		opt, rest, _ = strings.Cut(rest, ",")
		switch opt {
		case "", "omitempty", "required":
		case "inline":
			// On an embedded struct the option asks for the flattening
			// this package already does, so it costs nothing to honour
			// and lets a struct written for either encoder work here.
			// On a named field it asks for a catch-all that is not
			// implemented, and ignoring it would drop every key that
			// was meant to land in it.
			if embedded {
				continue
			}
			return fmt.Errorf(
				"yaml: %s.%s: \"inline\" is only supported on an embedded "+
					"struct, not on a named field", t.Name(), fieldName)
		default:
			return fmt.Errorf(
				"yaml: %s.%s: unsupported tag option %q "+
					"(this package supports \"omitempty\", \"required\" "+
					"and \"-\")",
				t.Name(), fieldName, opt)
		}
	}
	return nil
}

func (o tagOptions) has(want string) bool {
	rest := string(o)
	for rest != "" {
		var opt string
		opt, rest, _ = strings.Cut(rest, ",")
		if opt == want {
			return true
		}
	}
	return false
}
