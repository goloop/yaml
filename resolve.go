package yaml

import (
	"math"
	"strconv"
	"strings"
)

// scalarKind is what the core schema makes of a scalar's text.
type scalarKind uint8

const (
	nullScalar scalarKind = iota
	boolScalar
	intScalar
	floatScalar
	stringScalar
)

func (k scalarKind) String() string {
	switch k {
	case nullScalar:
		return "null"
	case boolScalar:
		return "bool"
	case intScalar:
		return "int"
	case floatScalar:
		return "float"
	}
	return "string"
}

// resolve applies the YAML 1.2 core schema to a scalar node and returns
// both the kind and the Go value it stands for. Quoted and block scalars
// are always strings; an explicit tag overrides everything.
func (n *node) resolve() (scalarKind, any, error) {
	if n.tag != "" {
		return n.resolveTagged()
	}
	if n.style != stylePlain {
		return stringScalar, n.value, nil
	}
	if digits, ok := ambiguousOctal(n.value); ok {
		return 0, nil, syntaxErr(n.line,
			"%q is ambiguous: a leading zero meant octal in YAML 1.1 and "+
				"decimal in 1.2; write 0o%s for octal, %s for decimal, "+
				"or quote it for a string",
			n.value, digits, strings.TrimLeft(digits, "0"))
	}
	k, v := resolvePlain(n.value)
	return k, v, nil
}

// ambiguousOctal reports an integer written with a leading zero, such as
// 0644. YAML 1.1 read those as octal and YAML 1.2 reads them as decimal,
// so the two answers differ by a factor that nobody notices until the
// file permissions turn out wrong. Rather than pick a side quietly, the
// parser refuses the spelling and asks for an unambiguous one.
func ambiguousOctal(s string) (digits string, ok bool) {
	body := s
	if body != "" && (body[0] == '+' || body[0] == '-') {
		body = body[1:]
	}
	if len(body) < 2 || body[0] != '0' {
		return "", false
	}
	rest := body[1:]
	if !onlyDigits(rest, 10) {
		return "", false
	}
	// Nothing but zeros is zero whichever base it is read in.
	if strings.Trim(rest, "0") == "" {
		return "", false
	}
	return rest, true
}

// isNull reports whether the node stands for an empty value.
func (n *node) isNull() bool {
	if n == nil {
		return true
	}
	if n.kind != scalarNode {
		return false
	}
	switch n.tag {
	case "!!null":
		return isNullText(n.value)
	case "":
		return n.style == stylePlain && isNullText(n.value)
	}
	return false
}

func isNullText(s string) bool {
	switch s {
	case "", "~", "null", "Null", "NULL":
		return true
	}
	return false
}

// resolvePlain classifies an unquoted scalar. Anything that is not a
// null, a bool or a number is a string - which is the whole of the core
// schema, and the reason yes/no/on/off stay strings here.
func resolvePlain(s string) (scalarKind, any) {
	switch s {
	case "", "~", "null", "Null", "NULL":
		return nullScalar, nil
	case "true", "True", "TRUE":
		return boolScalar, true
	case "false", "False", "FALSE":
		return boolScalar, false
	}
	if intShape(s) {
		if v, err := parseInt(s); err == nil {
			return intScalar, v
		}
		// The core schema still calls an oversized integer a number, so
		// keep it numeric rather than silently turning it into a string.
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			return floatScalar, v
		}
	}
	if v, ok := parseFloat(s); ok {
		return floatScalar, v
	}
	return stringScalar, s
}

// resolveTagged applies an explicit "!!" tag. A tag that the value does
// not fit is an error rather than a silent fallback: the author asked
// for a type, so a mismatch is a mistake worth reporting.
func (n *node) resolveTagged() (scalarKind, any, error) {
	switch n.tag {
	case "!!str":
		return stringScalar, n.value, nil
	case "!!null":
		if !isNullText(n.value) {
			return 0, nil, syntaxErr(n.line, "!!null value is not empty")
		}
		return nullScalar, nil, nil
	case "!!bool":
		if b, ok := looksBool(n.value); ok {
			return boolScalar, b, nil
		}
		return 0, nil, syntaxErr(n.line, "%q is not a bool", n.value)
	case "!!int":
		if intShape(n.value) {
			if v, err := parseInt(n.value); err == nil {
				return intScalar, v, nil
			}
		}
		return 0, nil, syntaxErr(n.line, "%q is not an int", n.value)
	case "!!float":
		if v, ok := parseFloat(n.value); ok {
			return floatScalar, v, nil
		}
		if intShape(n.value) {
			if v, err := parseInt(n.value); err == nil {
				return floatScalar, float64(v), nil
			}
		}
		return 0, nil, syntaxErr(n.line, "%q is not a float", n.value)
	}
	return 0, nil, syntaxErr(n.line, "tag %q cannot apply to a scalar", n.tag)
}

// looksBool accepts the YAML 1.1 spellings as well. They do not resolve
// as booleans on their own (the core schema calls them strings), but
// when a bool is explicitly asked for, refusing "yes" would be pedantry.
func looksBool(s string) (bool, bool) {
	switch strings.ToLower(s) {
	case "true", "yes", "on", "y":
		return true, true
	case "false", "no", "off", "n":
		return false, true
	}
	return false, false
}

// intShape reports whether s has the form of a core-schema integer:
// an optional sign, then decimal digits, 0x hex or 0o octal.
//
// Note that a leading zero carries no meaning here: 0644 is six hundred
// forty-four, not an octal. Octal has to be written 0o644.
func intShape(s string) bool {
	body := s
	if body != "" && (body[0] == '+' || body[0] == '-') {
		body = body[1:]
	}
	base := 10
	switch {
	case strings.HasPrefix(body, "0x"), strings.HasPrefix(body, "0X"):
		base, body = 16, body[2:]
	case strings.HasPrefix(body, "0o"), strings.HasPrefix(body, "0O"):
		base, body = 8, body[2:]
	}
	if body == "" {
		return false
	}
	return onlyDigits(body, base)
}

func parseInt(s string) (int64, error) {
	sign := ""
	body := s
	if body != "" && (body[0] == '+' || body[0] == '-') {
		if body[0] == '-' {
			sign = "-"
		}
		body = body[1:]
	}
	base := 10
	switch {
	case strings.HasPrefix(body, "0x"), strings.HasPrefix(body, "0X"):
		base, body = 16, body[2:]
	case strings.HasPrefix(body, "0o"), strings.HasPrefix(body, "0O"):
		base, body = 8, body[2:]
	}
	return strconv.ParseInt(sign+body, base, 64)
}

func onlyDigits(s string, base int) bool {
	for i := 0; i < len(s); i++ {
		var d int
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'f':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = int(c-'A') + 10
		default:
			return false
		}
		if d >= base {
			return false
		}
	}
	return true
}

// parseFloat accepts the core-schema float forms plus the infinities and
// the not-a-number spelling.
func parseFloat(s string) (float64, bool) {
	switch s {
	case ".inf", ".Inf", ".INF", "+.inf", "+.Inf", "+.INF":
		return math.Inf(1), true
	case "-.inf", "-.Inf", "-.INF":
		return math.Inf(-1), true
	case ".nan", ".NaN", ".NAN":
		return math.NaN(), true
	}
	if !floatShape(s) {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// floatShape reports the core-schema float form: an optional sign, then
// digits carrying either a decimal point or an exponent. Requiring one
// of those two keeps plain integers out, so they stay integers.
func floatShape(s string) bool {
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	digits, dot := 0, false
	for i < len(s) {
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			digits++
		case c == '.':
			if dot {
				return false
			}
			dot = true
		case c == 'e' || c == 'E':
			if digits == 0 {
				return false
			}
			return expShape(s[i+1:])
		default:
			return false
		}
		i++
	}
	return digits > 0 && dot
}

func expShape(s string) bool {
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	if i == len(s) {
		return false
	}
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
