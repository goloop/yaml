package yaml

import (
	"os"
	"strings"
)

// Expansion replaces ${NAME} and $NAME in scalar text with the value of
// the environment variable NAME.
//
// The name must look like an identifier: a letter or underscore, then
// letters, digits or underscores. Everything else keeps its "$" and is
// left alone. That rule is the whole point of not reusing os.Expand,
// which implements the shell's language instead: there "$1" is a
// positional parameter, so a price written "cost: $100" quietly becomes
// "cost: 00" and an awk snippet loses its first field. A configuration
// file has no positional parameters, but it does have prices, regular
// expressions and shell one-liners, and losing one of those without a
// word is the worst thing a config loader can do.
//
// There is deliberately no escape for a literal "$". A second escaping
// layer on top of YAML's own would be a puzzle rather than a feature:
// the literal forms are the ones YAML already has, single quotes and
// block scalars, and those are the ones this package refuses to expand.
// A doubled "$" is left alone as well, so a password like "pa$$word"
// survives without anybody having to know a rule.

// isNameByte reports whether c may appear in a variable name, and
// whether it may appear first.
func isNameByte(c byte, first bool) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c == '_':
		return true
	case c >= '0' && c <= '9':
		return !first
	}
	return false
}

// scanName reads a variable name at the start of s and returns it with
// the number of bytes it took. An empty name means s does not start
// with one.
func scanName(s string) (string, int) {
	i := 0
	for i < len(s) && isNameByte(s[i], i == 0) {
		i++
	}
	return s[:i], i
}

// expand replaces variable references in s. A reference to a variable
// that is not set becomes empty text, or an error when strict.
//
// The line is carried only so a failure can name the place: a config
// file that will not load is read by a person looking for the spot.
func expand(s string, line int, strict bool) (string, error) {
	if !strings.Contains(s, "$") {
		return s, nil
	}

	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); {
		c := s[i]
		if c != '$' {
			b.WriteByte(c)
			i++
			continue
		}

		// A doubled "$" is never a reference. This is not an escape -
		// nothing is removed - it is what keeps "pa$$word" the password
		// it is instead of a "$" followed by whatever $word expands to,
		// which is usually nothing at all.
		if i+1 < len(s) && s[i+1] == '$' {
			b.WriteString("$$")
			i += 2
			continue
		}

		rest := s[i+1:]

		// The braced form is always a reference: a "${" that does not
		// spell one is a typo, and letting it through as text is how a
		// setting silently never takes effect.
		if strings.HasPrefix(rest, "{") {
			end := strings.IndexByte(rest, '}')
			if end < 0 {
				return "", syntaxErr(line,
					"unterminated variable reference in %q", s)
			}
			name := rest[1:end]
			if got, n := scanName(name); n != len(name) || name == "" {
				_ = got
				return "", syntaxErr(line,
					"%q is not a variable name", "${"+name+"}")
			}
			val, err := lookupVar(name, line, strict)
			if err != nil {
				return "", err
			}
			b.WriteString(val)
			i += 1 + end + 1
			continue
		}

		// The bare form is a reference only when it reads as one, which
		// is what keeps "$100" and "$5" the text they plainly are.
		name, n := scanName(rest)
		if n == 0 {
			b.WriteByte('$')
			i++
			continue
		}
		val, err := lookupVar(name, line, strict)
		if err != nil {
			return "", err
		}
		b.WriteString(val)
		i += 1 + n
	}

	return b.String(), nil
}

func lookupVar(name string, line int, strict bool) (string, error) {
	val, ok := os.LookupEnv(name)
	if !ok && strict {
		return "", typeErr(line, "undefined variable %q", name)
	}
	return val, nil
}

// expandable reports whether a scalar's style allows expansion. Single
// quotes and block scalars are the literal forms, and they stay literal
// - the same rule the .env side of this toolkit follows for single
// quotes and backticks.
func (n *node) expandable() bool {
	return n.style == stylePlain || n.style == styleDouble
}

// expandTree walks the parsed document and expands every scalar value
// that may be expanded. Keys are never expanded: a key that depends on
// the environment makes a document unreadable and breaks the duplicate
// check that already ran at parse time.
//
// The walk runs once per node. Aliases make a node reachable from
// several parents, and expanding one twice would let a value that
// itself contains "$" be substituted a second time.
func expandTree(n *node, strict bool) error {
	seen := make(map[*node]bool)
	var walk func(*node) error
	walk = func(n *node) error {
		if n == nil || seen[n] {
			return nil
		}
		seen[n] = true

		switch n.kind {
		case scalarNode:
			if !n.expandable() {
				return nil
			}
			v, err := expand(n.value, n.line, strict)
			if err != nil {
				return err
			}
			n.value = v
		case seqNode:
			for _, it := range n.items {
				if err := walk(it); err != nil {
					return err
				}
			}
		case mapNode:
			for _, v := range n.vals {
				if err := walk(v); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(n)
}
