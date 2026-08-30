package yaml

import (
	"encoding"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// decoder carries the limits that make decoding safe on untrusted input.
// Aliases are shared pointers, so a small document can describe a very
// large tree; ops counts what is actually visited and stops it.
type decoder struct {
	ops   int
	depth int
}

func (d *decoder) step() error {
	d.ops++
	if d.ops > maxDecodeOps {
		return fmt.Errorf("yaml: document expands to more than %d values", maxDecodeOps)
	}
	return nil
}

func (d *decoder) typeErr(n *node, t reflect.Type, what string) error {
	return fmt.Errorf("yaml: line %d: cannot decode %s into %s", n.line, what, t)
}

// unmarshal writes the node into v, which must be settable.
func (d *decoder) unmarshal(n *node, v reflect.Value) error {
	if err := d.step(); err != nil {
		return err
	}
	d.depth++
	defer func() { d.depth-- }()
	if d.depth > maxDepth {
		return fmt.Errorf("yaml: line %d: nesting is too deep", n.line)
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
		return d.scalar(n, out)
	case seqNode:
		return d.sequence(n, out)
	case mapNode:
		return d.mapping(n, out)
	}
	return nil
}

// text hands a scalar to a TextUnmarshaler. Non-scalars never reach
// here: a type that parses text has nothing to do with a mapping.
func (d *decoder) text(n *node, u encoding.TextUnmarshaler) error {
	if err := u.UnmarshalText([]byte(n.value)); err != nil {
		return fmt.Errorf("yaml: line %d: %w", n.line, err)
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

func (d *decoder) scalar(n *node, v reflect.Value) error {
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
			return fmt.Errorf("yaml: line %d: %d overflows %s", n.line, i, v.Type())
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
			return fmt.Errorf("yaml: line %d: %d is negative, %s is not", n.line, i, v.Type())
		}
		if v.OverflowUint(uint64(i)) {
			return fmt.Errorf("yaml: line %d: %d overflows %s", n.line, i, v.Type())
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

func (d *decoder) sequence(n *node, v reflect.Value) error {
	switch v.Kind() {
	case reflect.Interface:
		if v.NumMethod() != 0 {
			return d.typeErr(n, v.Type(), "a sequence")
		}
		out := make([]any, len(n.items))
		for i, item := range n.items {
			if err := d.unmarshal(item, reflect.ValueOf(&out[i]).Elem()); err != nil {
				return err
			}
		}
		v.Set(reflect.ValueOf(out))
		return nil

	case reflect.Slice:
		out := reflect.MakeSlice(v.Type(), len(n.items), len(n.items))
		for i, item := range n.items {
			if err := d.unmarshal(item, out.Index(i)); err != nil {
				return err
			}
		}
		v.Set(out)
		return nil

	case reflect.Array:
		if len(n.items) > v.Len() {
			return fmt.Errorf("yaml: line %d: sequence of %d does not fit %s",
				n.line, len(n.items), v.Type())
		}
		for i, item := range n.items {
			if err := d.unmarshal(item, v.Index(i)); err != nil {
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

func (d *decoder) mapping(n *node, v reflect.Value) error {
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
			if err := d.unmarshal(vals[i], ev); err != nil {
				return err
			}
			v.SetMapIndex(kv, ev)
		}
		return nil

	case reflect.Struct:
		fields := cachedFields(v.Type())
		for i, k := range keys {
			f := fields.lookup(k.value)
			if f == nil {
				// Unknown keys are ignored, as in encoding/json: a
				// config file may carry keys this build does not know.
				continue
			}
			fv, err := fieldByIndex(v, f.index)
			if err != nil {
				return err
			}
			if err := d.unmarshal(vals[i], fv); err != nil {
				return err
			}
		}
		return nil
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
}

type fields struct {
	list  []field
	byKey map[string]int
}

// lookup finds the field for a YAML key: exactly first, then ignoring
// case, the way encoding/json resolves names.
func (f *fields) lookup(name string) *field {
	if i, ok := f.byKey[name]; ok {
		return &f.list[i]
	}
	for i := range f.list {
		if strings.EqualFold(f.list[i].name, name) {
			return &f.list[i]
		}
	}
	return nil
}

var fieldCache sync.Map // reflect.Type -> *fields

func cachedFields(t reflect.Type) *fields {
	if f, ok := fieldCache.Load(t); ok {
		return f.(*fields)
	}
	f := typeFields(t)
	fieldCache.Store(t, f)
	return f
}

// typeFields flattens a struct into the keys it answers to. An embedded
// struct with no name of its own contributes its fields directly, and a
// shallower field wins over a deeper one of the same name.
func typeFields(t reflect.Type) *fields {
	out := &fields{byKey: map[string]int{}}
	var walk func(t reflect.Type, index []int)

	walk = func(t reflect.Type, index []int) {
		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			tag := sf.Tag.Get("yaml")
			if tag == "-" {
				continue
			}
			name, opts := splitTag(tag)

			if sf.Anonymous && name == "" {
				ft := sf.Type
				if ft.Kind() == reflect.Pointer {
					ft = ft.Elem()
				}
				if ft.Kind() == reflect.Struct {
					walk(ft, append(append([]int{}, index...), i))
					continue
				}
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
			if _, dup := out.byKey[name]; dup {
				continue
			}
			out.byKey[name] = len(out.list)
			out.list = append(out.list, field{
				name:      name,
				index:     append(append([]int{}, index...), i),
				omitEmpty: opts.has("omitempty"),
			})
		}
	}

	walk(t, nil)
	return out
}

type tagOptions string

func splitTag(tag string) (string, tagOptions) {
	name, opts, _ := strings.Cut(tag, ",")
	return name, tagOptions(opts)
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
