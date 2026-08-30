package yaml

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Parsing limits. They exist so that a small hostile document cannot
// consume unbounded resources: depth guards the recursive parser and
// decoder, the op budget guards alias-driven expansion during decode.
const (
	maxDepth     = 512
	maxDecodeOps = 1_000_000
)

type nodeKind uint8

const (
	scalarNode nodeKind = iota
	mapNode
	seqNode
)

type scalarStyle uint8

const (
	stylePlain scalarStyle = iota
	styleSingle
	styleDouble
	styleLiteral
	styleFolded
)

// node is one parsed YAML node. Aliases resolve at parse time to the
// anchored *node, so a node may be shared between several parents; the
// decoder treats nodes as read-only.
type node struct {
	kind  nodeKind
	style scalarStyle
	tag   string // explicit "!!" tag or empty
	value string // scalar content
	keys  []*node
	vals  []*node
	items []*node
	line  int // 1-based source line where the node starts
}

type parser struct {
	src     []string
	li      int // current line index
	anchors map[string]*node
	depth   int
}

func syntaxErr(line int, format string, args ...any) error {
	return fmt.Errorf("yaml: line %d: %s", line, fmt.Sprintf(format, args...))
}

// parse turns a document into a node tree. A nil root with a nil error
// means the document is empty.
func parse(data []byte) (*node, error) {
	text := string(data)
	text = strings.TrimPrefix(text, "\ufeff")
	src := strings.Split(text, "\n")
	for i, l := range src {
		l = strings.TrimSuffix(l, "\r")
		// A YAML document is Unicode text. Bytes that are not valid
		// UTF-8 have no character to stand for, and nothing downstream
		// could write them back out, so they are refused here rather
		// than quietly turned into replacement characters later.
		if !utf8.ValidString(l) {
			return nil, syntaxErr(i+1, "invalid UTF-8: a YAML document must be Unicode text")
		}
		src[i] = l
	}
	// Splitting "a\n" yields a phantom empty element after the final
	// break. Most of the parser skips blank lines anyway, but a block
	// scalar counts them, so the document would grow a trailing line it
	// does not have.
	if n := len(src); n > 1 && src[n-1] == "" {
		src = src[:n-1]
	}
	p := &parser{src: src, anchors: map[string]*node{}}

	p.skipBlank()
	if p.eof() {
		return nil, nil
	}
	if strings.HasPrefix(strings.TrimLeft(p.cur(), " "), "%") {
		return nil, syntaxErr(p.line(), "directives are not supported")
	}
	// An optional "---" document start, possibly with the root node on
	// the same line.
	if rest, ok := cutDocStart(p.cur()); ok {
		if strings.TrimSpace(rest) == "" || isCommentOnly(rest) {
			p.li++
		} else {
			pad := len(p.cur()) - len(rest)
			p.src[p.li] = strings.Repeat(" ", pad) + rest
		}
	}

	root, err := p.parseBlockNode(0, -1)
	if err != nil {
		return nil, err
	}

	// Only blank lines, comments and one "..." may follow.
	p.skipBlank()
	if !p.eof() {
		t := strings.TrimSpace(p.cur())
		if t == "..." {
			p.li++
			p.skipBlank()
		}
	}
	if !p.eof() {
		t := strings.TrimSpace(p.cur())
		if t == "---" || strings.HasPrefix(t, "--- ") {
			return nil, syntaxErr(p.line(), "multiple documents are not supported")
		}
		return nil, syntaxErr(p.line(), "unexpected content after document end")
	}
	return root, nil
}

// cutDocStart reports whether the line opens a document ("---") and
// returns whatever follows the marker. The marker has to be followed by
// a break or by blank space: "---foo" is a plain scalar, not a marker.
func cutDocStart(l string) (rest string, ok bool) {
	t := strings.TrimLeft(l, " ")
	if !strings.HasPrefix(t, "---") {
		return "", false
	}
	switch t = t[3:]; {
	case t == "":
		return "", true
	case t[0] == ' ' || t[0] == '\t':
		return strings.TrimLeft(t, " \t"), true
	}
	return "", false
}

func (p *parser) eof() bool   { return p.li >= len(p.src) }
func (p *parser) cur() string { return p.src[p.li] }
func (p *parser) line() int   { return p.li + 1 }

// skipBlank advances past blank and comment-only lines.
func (p *parser) skipBlank() {
	for !p.eof() {
		t := strings.TrimSpace(p.cur())
		if t == "" || strings.HasPrefix(t, "#") {
			p.li++
			continue
		}
		return
	}
}

func isCommentOnly(s string) bool {
	t := strings.TrimSpace(s)
	return t == "" || strings.HasPrefix(t, "#")
}

// indentOf returns the indentation of a line in columns. Tabs are not
// valid YAML indentation.
func indentOf(l string, line int) (int, error) {
	for i := 0; i < len(l); i++ {
		switch l[i] {
		case ' ':
		case '\t':
			return 0, syntaxErr(line, "tab characters are not allowed in indentation")
		default:
			return i, nil
		}
	}
	return len(l), nil
}

func (p *parser) enter(line int) error {
	p.depth++
	if p.depth > maxDepth {
		return syntaxErr(line, "nesting is too deep")
	}
	return nil
}

func (p *parser) leave() { p.depth-- }

// isDocEnd reports a "---" or "..." marker line, which terminates any
// open block node. It trims the line itself, so callers may hand it the
// raw content.
func isDocEnd(t string) bool {
	t = strings.TrimRight(t, " \t")
	for _, marker := range [...]string{"---", "..."} {
		if t == marker {
			return true
		}
		if rest, cut := strings.CutPrefix(t, marker); cut &&
			(rest[0] == ' ' || rest[0] == '\t') {
			return true
		}
	}
	return false
}

// parseBlockNode parses the node that starts at the current line. The
// node's content must be indented at least minCol columns; base is the
// parent column, used as the folding baseline for plain and quoted
// scalars. Returns nil for an absent (null) node.
func (p *parser) parseBlockNode(minCol, base int) (*node, error) {
	if err := p.enter(p.line()); err != nil {
		return nil, err
	}
	defer p.leave()

	p.skipBlank()
	if p.eof() {
		return nil, nil
	}
	col, err := indentOf(p.cur(), p.line())
	if err != nil {
		return nil, err
	}
	if col < minCol {
		return nil, nil
	}
	content := p.cur()[col:]
	if isDocEnd(strings.TrimRight(content, " ")) {
		return nil, nil
	}

	// Node properties: anchor and/or tag, possibly alone on the line.
	anchor, tag, rest, err := cutProperties(content, p.line())
	if err != nil {
		return nil, err
	}
	if anchor != "" || tag != "" {
		if strings.TrimSpace(rest) == "" || isCommentOnly(rest) {
			p.li++
			n, err := p.parseBlockNode(col+1, col)
			if err != nil {
				return nil, err
			}
			return p.finish(n, anchor, tag, p.line())
		}
		pad := len(content) - len(rest) + col
		p.src[p.li] = strings.Repeat(" ", pad) + rest
		n, err := p.parseBlockNode(minCol, base)
		if err != nil {
			return nil, err
		}
		return p.finish(n, anchor, tag, p.line())
	}

	switch {
	case rest == "?" || strings.HasPrefix(rest, "? "):
		return nil, syntaxErr(p.line(), "explicit mapping keys are not supported")
	case rest == "-" || strings.HasPrefix(rest, "- "):
		return p.parseBlockSeq(col)
	case rest[0] == '*':
		n, after, err := p.alias(rest, p.line())
		if err != nil {
			return nil, err
		}
		if !isCommentOnly(after) {
			return nil, syntaxErr(p.line(), "unexpected content after alias")
		}
		p.li++
		return n, nil
	case rest[0] == '[' || rest[0] == '{':
		return p.parseFlowLine(col)
	case rest[0] == '|' || rest[0] == '>':
		return p.parseBlockScalar(col, base, rest)
	}

	// "key:" makes this a mapping. The check has to run before the
	// quoted-scalar case below, because a quoted key is indistinguishable
	// from a quoted scalar until the colon behind it is found.
	if _, _, _, isKey, err := splitKey(rest, p.line()); err != nil {
		return nil, err
	} else if isKey {
		return p.parseBlockMap(col)
	}
	if rest[0] == '"' || rest[0] == '\'' {
		return p.parseQuotedScalar(base, col)
	}
	return p.parseScalar(col, base)
}

// finish attaches properties to a parsed node and registers its anchor.
func (p *parser) finish(n *node, anchor, tag string, line int) (*node, error) {
	if n == nil {
		n = &node{kind: scalarNode, style: stylePlain, line: line}
	}
	if tag != "" {
		if n.tag != "" && n.tag != tag {
			return nil, syntaxErr(line, "conflicting tags")
		}
		n.tag = tag
	}
	if anchor != "" {
		p.anchors[anchor] = n
	}
	return n, nil
}

// cutProperties strips leading "&anchor" and "!!tag" tokens (in either
// order) from a value position.
func cutProperties(s string, line int) (anchor, tag, rest string, err error) {
	rest = s
	for {
		switch {
		case strings.HasPrefix(rest, "&"):
			if anchor != "" {
				return "", "", "", syntaxErr(line, "duplicate anchor property")
			}
			end := strings.IndexAny(rest, " \t")
			if end < 0 {
				end = len(rest)
			}
			anchor = rest[1:end]
			if anchor == "" {
				return "", "", "", syntaxErr(line, "empty anchor name")
			}
			if i := strings.IndexAny(anchor, ",[]{}"); i >= 0 {
				return "", "", "", syntaxErr(line, "invalid anchor name %q", anchor)
			}
			rest = strings.TrimLeft(rest[end:], " ")
		case strings.HasPrefix(rest, "!"):
			end := strings.IndexAny(rest, " \t")
			if end < 0 {
				end = len(rest)
			}
			t := rest[:end]
			if !isSupportedTag(t) {
				return "", "", "", syntaxErr(line, "unsupported tag %q", t)
			}
			if tag != "" {
				return "", "", "", syntaxErr(line, "duplicate tag property")
			}
			tag = t
			rest = strings.TrimLeft(rest[end:], " ")
		default:
			return anchor, tag, rest, nil
		}
	}
}

func isSupportedTag(t string) bool {
	switch t {
	case "!!str", "!!int", "!!float", "!!bool", "!!null", "!!map", "!!seq":
		return true
	}
	return false
}

// alias resolves "*name" against completed anchors. Forward and
// self-references are impossible by construction, which rules out
// cycles.
func (p *parser) alias(s string, line int) (*node, string, error) {
	end := strings.IndexAny(s, " \t,]}")
	if end < 0 {
		end = len(s)
	}
	name := s[1:end]
	if name == "" {
		return nil, "", syntaxErr(line, "empty alias name")
	}
	n, ok := p.anchors[name]
	if !ok {
		return nil, "", syntaxErr(line, "unknown anchor %q referenced by alias", name)
	}
	return n, s[end:], nil
}

// splitKey recognizes "key:" at the start of a mapping line. It
// returns the raw key text, its style, and the remainder after the
// colon. Keys are single-line scalars; a plain key ends at the first
// ":" followed by a space or the end of line.
func splitKey(s string, line int) (key string, style scalarStyle, rest string, ok bool, err error) {
	if s == "" {
		return "", 0, "", false, nil
	}
	if s[0] == '"' || s[0] == '\'' {
		raw, after, qerr := cutQuoted(s, line)
		if qerr != nil {
			return "", 0, "", false, nil // not a key; scalar path reports errors
		}
		after = strings.TrimLeft(after, " ")
		if after == ":" || strings.HasPrefix(after, ": ") || strings.HasPrefix(after, ":\t") {
			st := styleSingle
			if s[0] == '"' {
				st = styleDouble
			}
			return raw, st, strings.TrimPrefix(after[1:], " "), true, nil
		}
		return "", 0, "", false, nil
	}
	// Plain key: scan for ": " / ":" at end of line, stopping at a
	// comment.
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ':':
			if i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\t' {
				key = strings.TrimRight(s[:i], " \t")
				if key == "" {
					return "", 0, "", false, syntaxErr(line, "empty mapping key")
				}
				rest = strings.TrimLeft(s[i+1:], " ")
				return key, stylePlain, rest, true, nil
			}
		case '#':
			if i > 0 && (s[i-1] == ' ' || s[i-1] == '\t') {
				return "", 0, "", false, nil
			}
		}
	}
	return "", 0, "", false, nil
}

// cutQuoted reads a single-line quoted scalar at the start of s and
// returns its decoded value and the remainder.
func cutQuoted(s string, line int) (val, rest string, err error) {
	q := s[0]
	if q == '\'' {
		var b strings.Builder
		i := 1
		for i < len(s) {
			if s[i] == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					b.WriteByte('\'')
					i += 2
					continue
				}
				return b.String(), s[i+1:], nil
			}
			b.WriteByte(s[i])
			i++
		}
		return "", "", syntaxErr(line, "unterminated single-quoted scalar")
	}
	// Double-quoted.
	var b strings.Builder
	i := 1
	for i < len(s) {
		c := s[i]
		switch c {
		case '"':
			return b.String(), s[i+1:], nil
		case '\\':
			r, n, eerr := unescape(s[i:], line)
			if eerr != nil {
				return "", "", eerr
			}
			b.WriteString(r)
			i += n
		default:
			b.WriteByte(c)
			i++
		}
	}
	return "", "", syntaxErr(line, "unterminated double-quoted scalar")
}

// unescape decodes one backslash escape at the start of s and returns
// the replacement plus the number of input bytes consumed.
func unescape(s string, line int) (string, int, error) {
	if len(s) < 2 {
		return "", 0, syntaxErr(line, "truncated escape sequence")
	}
	switch s[1] {
	case '0':
		return "\x00", 2, nil
	case 'a':
		return "\a", 2, nil
	case 'b':
		return "\b", 2, nil
	case 't':
		return "\t", 2, nil
	case 'n':
		return "\n", 2, nil
	case 'v':
		return "\v", 2, nil
	case 'f':
		return "\f", 2, nil
	case 'r':
		return "\r", 2, nil
	case 'e':
		return "\x1b", 2, nil
	case ' ':
		return " ", 2, nil
	case '"':
		return "\"", 2, nil
	case '/':
		return "/", 2, nil
	case '\\':
		return "\\", 2, nil
	case 'N':
		return "\u0085", 2, nil // next line
	case '_':
		return "\u00a0", 2, nil // non-breaking space
	case 'L':
		return "\u2028", 2, nil // line separator
	case 'P':
		return "\u2029", 2, nil // paragraph separator
	case 'x', 'u', 'U':
		width := map[byte]int{'x': 2, 'u': 4, 'U': 8}[s[1]]
		if len(s) < 2+width {
			return "", 0, syntaxErr(line, "truncated \\%c escape", s[1])
		}
		var r rune
		for _, c := range []byte(s[2 : 2+width]) {
			var d byte
			switch {
			case c >= '0' && c <= '9':
				d = c - '0'
			case c >= 'a' && c <= 'f':
				d = c - 'a' + 10
			case c >= 'A' && c <= 'F':
				d = c - 'A' + 10
			default:
				return "", 0, syntaxErr(line, "invalid \\%c escape", s[1])
			}
			r = r<<4 | rune(d)
		}
		return string(r), 2 + width, nil
	}
	return "", 0, syntaxErr(line, "unknown escape \\%c", s[1])
}

// parseBlockMap parses a block mapping whose keys sit at column col.
func (p *parser) parseBlockMap(col int) (*node, error) {
	m := &node{kind: mapNode, line: p.line()}
	seen := map[string]int{}
	for {
		p.skipBlank()
		if p.eof() {
			return m, nil
		}
		c, err := indentOf(p.cur(), p.line())
		if err != nil {
			return nil, err
		}
		if c < col {
			return m, nil
		}
		content := p.cur()[c:]
		if isDocEnd(strings.TrimRight(content, " ")) {
			return m, nil
		}
		if c > col {
			return nil, syntaxErr(p.line(), "unexpected indentation")
		}
		key, kstyle, rest, ok, err := splitKey(content, p.line())
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, syntaxErr(p.line(), "did not find expected key")
		}
		if prev, dup := seen[key]; dup {
			return nil, syntaxErr(p.line(),
				"mapping key %q already defined at line %d", key, prev)
		}
		seen[key] = p.line()
		kn := &node{kind: scalarNode, style: kstyle, value: key, line: p.line()}

		vn, err := p.parseMapValue(col, rest)
		if err != nil {
			return nil, err
		}
		m.keys = append(m.keys, kn)
		m.vals = append(m.vals, vn)
	}
}

// parseMapValue parses what follows "key:" on the same line, or the
// indented block below it. keyCol is the key's column.
func (p *parser) parseMapValue(keyCol int, rest string) (*node, error) {
	if err := p.enter(p.line()); err != nil {
		return nil, err
	}
	defer p.leave()

	anchor, tag, rest, err := cutProperties(rest, p.line())
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(rest) == "" || isCommentOnly(rest) {
		// Value is the block below: a more-indented node, or a
		// sequence that may sit at the key's own column.
		p.li++
		p.skipBlank()
		var vn *node
		if !p.eof() {
			c, err := indentOf(p.cur(), p.line())
			if err != nil {
				return nil, err
			}
			content := p.cur()[c:]
			switch {
			case c > keyCol:
				vn, err = p.parseBlockNode(keyCol+1, keyCol)
			case c == keyCol && !isDocEnd(strings.TrimRight(content, " ")) &&
				(content == "-" || strings.HasPrefix(content, "- ")):
				vn, err = p.parseBlockSeq(keyCol)
			}
			if err != nil {
				return nil, err
			}
		}
		return p.finish(vn, anchor, tag, p.line())
	}

	var vn *node
	switch {
	case rest[0] == '*':
		n, after, aerr := p.alias(rest, p.line())
		if aerr != nil {
			return nil, aerr
		}
		if !isCommentOnly(after) {
			return nil, syntaxErr(p.line(), "unexpected content after alias")
		}
		p.li++
		vn = n
	case rest[0] == '|' || rest[0] == '>':
		col := len(p.cur()) - len(rest)
		n, berr := p.parseBlockScalar(col, keyCol, rest)
		if berr != nil {
			return nil, berr
		}
		vn = n
	case rest[0] == '[' || rest[0] == '{':
		col := len(p.cur()) - len(rest)
		n, ferr := p.parseFlowLine(col)
		if ferr != nil {
			return nil, ferr
		}
		vn = n
	case rest[0] == '"' || rest[0] == '\'':
		col := len(p.cur()) - len(rest)
		n, qerr := p.parseQuotedScalar(keyCol, col)
		if qerr != nil {
			return nil, qerr
		}
		vn = n
	case rest == "-" || strings.HasPrefix(rest, "- "):
		return nil, syntaxErr(p.line(),
			"block sequence entries are not allowed on the same line as a mapping key")
	default:
		if _, _, _, isKey, kerr := splitKey(rest, p.line()); kerr != nil {
			return nil, kerr
		} else if isKey {
			return nil, syntaxErr(p.line(),
				"mapping values are not allowed in this context")
		}
		col := len(p.cur()) - len(rest)
		n, serr := p.parseScalar(col, keyCol)
		if serr != nil {
			return nil, serr
		}
		vn = n
	}
	return p.finish(vn, anchor, tag, p.line())
}

// parseBlockSeq parses a block sequence whose dashes sit at column col.
func (p *parser) parseBlockSeq(col int) (*node, error) {
	s := &node{kind: seqNode, line: p.line()}
	for {
		p.skipBlank()
		if p.eof() {
			return s, nil
		}
		c, err := indentOf(p.cur(), p.line())
		if err != nil {
			return nil, err
		}
		if c < col {
			return s, nil
		}
		content := p.cur()[c:]
		if isDocEnd(strings.TrimRight(content, " ")) {
			return s, nil
		}
		if c > col {
			return nil, syntaxErr(p.line(), "unexpected indentation")
		}
		if content != "-" && !strings.HasPrefix(content, "- ") {
			return s, nil
		}
		rest := strings.TrimLeft(content[1:], " ")
		var item *node
		if strings.TrimSpace(rest) == "" || isCommentOnly(rest) {
			p.li++
			item, err = p.parseBlockNode(col+1, col)
		} else {
			// Re-anchor the remainder as a virtual line so compact
			// entries ("- key: v", "- - x") reuse the block parser.
			restCol := len(p.cur()) - len(rest)
			p.src[p.li] = strings.Repeat(" ", restCol) + rest
			item, err = p.parseBlockNode(restCol, col)
		}
		if err != nil {
			return nil, err
		}
		if item == nil {
			item = &node{kind: scalarNode, style: stylePlain, line: p.line()}
		}
		s.items = append(s.items, item)
	}
}

// parseScalar parses a plain scalar that starts at column col of the
// current line, folding continuation lines indented deeper than base.
func (p *parser) parseScalar(col, base int) (*node, error) {
	n := &node{kind: scalarNode, style: stylePlain, line: p.line()}
	frag := cutComment(p.cur()[col:])
	frag = strings.TrimRight(frag, " \t")
	if frag == "" {
		return nil, syntaxErr(p.line(), "expected scalar content")
	}
	parts := []string{frag}
	blanks := 0
	p.li++
	for !p.eof() {
		t := strings.TrimSpace(p.cur())
		if t == "" {
			blanks++
			p.li++
			continue
		}
		c, err := indentOf(p.cur(), p.line())
		if err != nil {
			return nil, err
		}
		if c <= base || strings.HasPrefix(t, "#") ||
			isDocEnd(strings.TrimRight(p.cur()[c:], " ")) {
			break
		}
		content := p.cur()[c:]
		if _, _, _, isKey, kerr := splitKey(content, p.line()); kerr != nil {
			return nil, kerr
		} else if isKey {
			break
		}
		if content == "-" || strings.HasPrefix(content, "- ") {
			break
		}
		frag = strings.TrimRight(cutComment(content), " \t")
		if frag == "" {
			break
		}
		parts = append(parts, foldSep(blanks)+frag)
		blanks = 0
		p.li++
	}
	n.value = strings.Join(parts, "")
	return n, nil
}

// foldSep is the separator produced by folding: adjacent lines join
// with a space, blank lines become hard newlines.
func foldSep(blanks int) string {
	if blanks == 0 {
		return " "
	}
	return strings.Repeat("\n", blanks)
}

// cutComment strips a trailing comment from plain-scalar content. A
// comment starts at "#" preceded by whitespace.
func cutComment(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '#' && i > 0 && (s[i-1] == ' ' || s[i-1] == '\t') {
			return s[:i]
		}
	}
	return s
}

// parseQuotedScalar parses a quoted scalar that may span lines. The
// parser stands on the opening quote's line and col is the column of the
// quote itself: it is passed in rather than searched for, because on a
// line like `"key": "value"` a search would find the key's quote.
func (p *parser) parseQuotedScalar(base, col int) (*node, error) {
	l := p.cur()
	start := col
	q := l[start]
	n := &node{kind: scalarNode, line: p.line()}
	if q == '"' {
		n.style = styleDouble
	} else {
		n.style = styleSingle
	}

	var b strings.Builder
	i := start + 1
	blanks := 0
	pending := false // a fold separator is owed before the next fragment
	flushSep := func() {
		if pending {
			b.WriteString(foldSep(blanks))
			blanks = 0
			pending = false
		}
	}
	for {
		if p.eof() {
			return nil, syntaxErr(n.line, "unterminated quoted scalar")
		}
		l = p.cur()
		for i < len(l) {
			c := l[i]
			if c == q {
				if q == '\'' && i+1 < len(l) && l[i+1] == '\'' {
					flushSep()
					b.WriteByte('\'')
					i += 2
					continue
				}
				// Closing quote: only spaces/comment may follow.
				after := strings.TrimLeft(l[i+1:], " \t")
				if !isCommentOnly(after) {
					return nil, syntaxErr(p.line(),
						"unexpected content after quoted scalar")
				}
				p.li++
				n.value = b.String()
				return n, nil
			}
			if q == '"' && c == '\\' {
				if i+1 == len(l) {
					// Escaped line break: continue with no fold space.
					i = len(l)
					pending = false
					blanks = 0
					goto nextLine
				}
				flushSep()
				r, w, err := unescape(l[i:], p.line())
				if err != nil {
					return nil, err
				}
				b.WriteString(r)
				i += w
				continue
			}
			flushSep()
			b.WriteByte(c)
			i++
		}
		pending = true
	nextLine:
		p.li++
		if p.eof() {
			return nil, syntaxErr(n.line, "unterminated quoted scalar")
		}
		l = p.cur()
		if strings.TrimSpace(l) == "" {
			blanks++
			i = len(l)
			continue
		}
		c, err := indentOf(l, p.line())
		if err != nil {
			return nil, err
		}
		if c <= base {
			return nil, syntaxErr(n.line, "unterminated quoted scalar")
		}
		// Continuation content starts after the indentation; trailing
		// spaces fold away.
		trimmed := strings.TrimRight(l, " \t")
		p.src[p.li] = trimmed
		i = c
	}
}

// parseBlockScalar parses a literal (|) or folded (>) block scalar.
// col is the column of the indicator, base the parent column; rest is
// the line content starting at the indicator.
func (p *parser) parseBlockScalar(col, base int, rest string) (*node, error) {
	n := &node{kind: scalarNode, line: p.line()}
	folded := rest[0] == '>'
	if folded {
		n.style = styleFolded
	} else {
		n.style = styleLiteral
	}

	// Header: chomping and explicit indentation in either order, then
	// only a comment.
	chomp := byte(0) // 0 clip, '-' strip, '+' keep
	explicit := 0
	h := rest[1:]
	for len(h) > 0 {
		switch {
		case h[0] == '-' || h[0] == '+':
			if chomp != 0 {
				return nil, syntaxErr(p.line(), "invalid block scalar header")
			}
			chomp = h[0]
			h = h[1:]
		case h[0] >= '1' && h[0] <= '9':
			if explicit != 0 {
				return nil, syntaxErr(p.line(), "invalid block scalar header")
			}
			explicit = int(h[0] - '0')
			h = h[1:]
		default:
			if !isCommentOnly(h) && strings.TrimSpace(h) != "" {
				return nil, syntaxErr(p.line(), "invalid block scalar header")
			}
			h = ""
		}
	}
	p.li++

	indent := -1
	if explicit > 0 {
		indent = base + explicit
		if base < 0 {
			indent = explicit
		}
	}

	var lines []string
	for !p.eof() {
		l := p.cur()
		if strings.TrimSpace(l) == "" {
			lines = append(lines, "")
			p.li++
			continue
		}
		c, err := indentOf(l, p.line())
		if err != nil {
			return nil, err
		}
		if indent < 0 {
			if c <= base {
				break
			}
			indent = c
		}
		if c < indent {
			if c <= base {
				break
			}
			return nil, syntaxErr(p.line(), "invalid indentation in block scalar")
		}
		lines = append(lines, l[indent:])
		p.li++
	}
	// Trailing blank lines beyond the content belong to chomping.
	content := assembleBlock(lines, folded)
	switch chomp {
	case '-':
		content = strings.TrimRight(content, "\n")
	case '+':
		// keep as is
	default:
		content = strings.TrimRight(content, "\n")
		if content != "" {
			content += "\n"
		}
	}
	n.value = content
	return n, nil
}

// assembleBlock joins block scalar lines: literally, or with folding.
func assembleBlock(lines []string, folded bool) string {
	if !folded {
		return strings.Join(lines, "\n") + suffixNL(lines)
	}

	var b strings.Builder
	first := true
	prevIndented := false
	blanks := 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			if !first {
				blanks++
			}
			continue
		}
		indented := l[0] == ' ' || l[0] == '\t'
		if first {
			first = false
		} else {
			b.WriteString(foldBreak(blanks, prevIndented, indented))
		}
		b.WriteString(l)
		prevIndented = indented
		blanks = 0
	}
	if first {
		return strings.Repeat("\n", blanks)
	}
	b.WriteString("\n")
	b.WriteString(strings.Repeat("\n", blanks))
	return b.String()
}

// foldBreak is what the line break between two content lines becomes,
// together with any blank lines that sat between them. A break between
// two plain lines folds to a space; a break next to a more-indented line
// is kept, because indented lines carry their own layout; and every
// blank line adds a break of its own.
func foldBreak(blanks int, prevIndented, indented bool) string {
	if prevIndented || indented {
		return strings.Repeat("\n", blanks+1)
	}
	if blanks == 0 {
		return " "
	}
	return strings.Repeat("\n", blanks)
}

func suffixNL(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return "\n"
}
