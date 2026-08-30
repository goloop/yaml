package yaml

import "strings"

// flowReader walks a flow collection. A flow collection may span several
// lines, so the reader carries its own position and the block parser
// advances only once, after the whole collection has been read.
type flowReader struct {
	p  *parser
	li int // line index
	ci int // byte index within that line
}

func (f *flowReader) eof() bool { return f.li >= len(f.p.src) }

func (f *flowReader) line() int { return f.li + 1 }

// cur returns the byte at the reader's position. It is valid only after
// skip has left that position on content.
func (f *flowReader) cur() byte { return f.p.src[f.li][f.ci] }

// rest returns what is left of the current line.
func (f *flowReader) rest() string {
	if f.eof() {
		return ""
	}
	l := f.p.src[f.li]
	if f.ci >= len(l) {
		return ""
	}
	return l[f.ci:]
}

// skip advances past spaces, tabs, comments and line breaks. Inside a
// flow collection a line break is only separation, so the reader may
// cross lines freely.
func (f *flowReader) skip() {
	for !f.eof() {
		l := f.p.src[f.li]
		if f.ci >= len(l) {
			f.li++
			f.ci = 0
			continue
		}
		switch l[f.ci] {
		case ' ', '\t':
			f.ci++
		case '#':
			f.li++
			f.ci = 0
		default:
			return
		}
	}
}

// token reads a name up to the first character that cannot belong to
// one: whitespace or a flow indicator.
func (f *flowReader) token() string {
	l := f.p.src[f.li]
	start := f.ci
	for f.ci < len(l) && !isFlowBreak(l[f.ci]) {
		f.ci++
	}
	return l[start:f.ci]
}

func isFlowBreak(c byte) bool {
	switch c {
	case ' ', '\t', ',', '[', ']', '{', '}':
		return true
	}
	return false
}

// isFlowSep reports whether c ends a plain scalar when it follows a
// colon, i.e. whether that colon separates a key from its value.
func isFlowSep(c byte) bool {
	switch c {
	case ' ', '\t', ',', '[', ']', '{', '}':
		return true
	}
	return false
}

// parseFlowLine parses a flow collection that starts at column col of
// the current line and leaves the parser on the line after it.
func (p *parser) parseFlowLine(col int) (*node, error) {
	f := &flowReader{p: p, li: p.li, ci: col}
	n, err := p.parseFlowNode(f)
	if err != nil {
		return nil, err
	}
	// Only spaces and a comment may follow on the closing line.
	if !f.eof() && !isCommentOnly(f.rest()) {
		return nil, syntaxErr(f.line(), "unexpected content after flow collection")
	}
	p.li = f.li + 1
	if p.li > len(p.src) {
		p.li = len(p.src)
	}
	return n, nil
}

// parseFlowNode parses one node inside a flow collection.
func (p *parser) parseFlowNode(f *flowReader) (*node, error) {
	if err := p.enter(f.line()); err != nil {
		return nil, err
	}
	defer p.leave()

	var anchor, tag string
	for {
		f.skip()
		if f.eof() {
			return nil, syntaxErr(f.line(), "unexpected end of input in flow collection")
		}
		switch f.cur() {
		case '&':
			if anchor != "" {
				return nil, syntaxErr(f.line(), "duplicate anchor property")
			}
			line := f.line()
			f.ci++
			anchor = f.token()
			if anchor == "" {
				return nil, syntaxErr(line, "empty anchor name")
			}
			continue
		case '!':
			if tag != "" {
				return nil, syntaxErr(f.line(), "duplicate tag property")
			}
			line := f.line()
			tag = f.token()
			if !isSupportedTag(tag) {
				return nil, syntaxErr(line, "unsupported tag %q", tag)
			}
			continue
		}
		break
	}

	line := f.line()
	var n *node
	var err error
	switch c := f.cur(); {
	case c == '[':
		n, err = p.parseFlowSeq(f)
	case c == '{':
		n, err = p.parseFlowMap(f)
	case c == '*':
		var after string
		n, after, err = p.alias(f.rest(), line)
		if err == nil {
			f.ci += len(f.rest()) - len(after)
		}
	case c == '"' || c == '\'':
		var val, after string
		val, after, err = cutQuoted(f.rest(), line)
		if err == nil {
			f.ci += len(f.rest()) - len(after)
			style := styleSingle
			if c == '"' {
				style = styleDouble
			}
			n = &node{kind: scalarNode, style: style, value: val, line: line}
		}
	default:
		var val string
		val, err = f.plain()
		if err == nil {
			n = &node{kind: scalarNode, style: stylePlain, value: val, line: line}
		}
	}
	if err != nil {
		return nil, err
	}
	return p.finish(n, anchor, tag, line)
}

// parseFlowSeq parses "[a, b, c]".
func (p *parser) parseFlowSeq(f *flowReader) (*node, error) {
	n := &node{kind: seqNode, line: f.line()}
	f.ci++ // consume '['
	for {
		f.skip()
		if f.eof() {
			return nil, syntaxErr(n.line, "unterminated flow sequence")
		}
		if f.cur() == ']' {
			f.ci++
			return n, nil
		}
		item, err := p.parseFlowNode(f)
		if err != nil {
			return nil, err
		}

		// A "key: value" pair may stand as a sequence entry, and means a
		// mapping of that one pair: [a, b: 1] is [a, {b: 1}]. Config
		// files written by hand lean on this.
		f.skip()
		if !f.eof() && f.cur() == ':' {
			if item.kind != scalarNode {
				return nil, syntaxErr(f.line(), "mapping keys must be scalars")
			}
			key := item
			f.ci++
			f.skip()
			val := &node{kind: scalarNode, style: stylePlain, line: key.line}
			if !f.eof() {
				if c := f.cur(); c != ',' && c != ']' {
					if val, err = p.parseFlowNode(f); err != nil {
						return nil, err
					}
				}
			}
			item = &node{
				kind: mapNode, line: key.line,
				keys: []*node{key}, vals: []*node{val},
			}
		}
		n.items = append(n.items, item)

		f.skip()
		if f.eof() {
			return nil, syntaxErr(n.line, "unterminated flow sequence")
		}
		switch f.cur() {
		case ',':
			f.ci++
		case ']':
			f.ci++
			return n, nil
		default:
			return nil, syntaxErr(f.line(), "expected ',' or ']' in flow sequence")
		}
	}
}

// parseFlowMap parses "{k: v, k2: v2}".
func (p *parser) parseFlowMap(f *flowReader) (*node, error) {
	n := &node{kind: mapNode, line: f.line()}
	seen := map[string]int{}
	f.ci++ // consume '{'
	for {
		f.skip()
		if f.eof() {
			return nil, syntaxErr(n.line, "unterminated flow mapping")
		}
		if f.cur() == '}' {
			f.ci++
			return n, nil
		}

		kline := f.line()
		key, err := p.parseFlowNode(f)
		if err != nil {
			return nil, err
		}
		if key.kind != scalarNode {
			return nil, syntaxErr(kline, "mapping keys must be scalars")
		}

		// The value is optional: "{a, b}" is a mapping of two null
		// entries, which is how YAML writes a set.
		var val *node
		f.skip()
		if !f.eof() && f.cur() == ':' {
			f.ci++
			f.skip()
			if f.eof() {
				return nil, syntaxErr(n.line, "unterminated flow mapping")
			}
			if c := f.cur(); c != ',' && c != '}' {
				val, err = p.parseFlowNode(f)
				if err != nil {
					return nil, err
				}
			}
		}
		if val == nil {
			val = &node{kind: scalarNode, style: stylePlain, line: kline}
		}
		if prev, dup := seen[key.value]; dup {
			return nil, syntaxErr(kline,
				"mapping key %q already defined at line %d", key.value, prev)
		}
		seen[key.value] = kline
		n.keys = append(n.keys, key)
		n.vals = append(n.vals, val)

		f.skip()
		if f.eof() {
			return nil, syntaxErr(n.line, "unterminated flow mapping")
		}
		switch f.cur() {
		case ',':
			f.ci++
		case '}':
			f.ci++
			return n, nil
		default:
			return nil, syntaxErr(f.line(), "expected ',' or '}' in flow mapping")
		}
	}
}

// plain reads a plain scalar in flow context. It ends at a flow
// indicator or at a colon that separates a key from its value; a line
// break folds to a space and a blank line to a newline, as in block
// context.
func (f *flowReader) plain() (string, error) {
	line := f.line()
	var parts []string
	blanks := 0

	// Running off the end is not reported here: the enclosing collection
	// knows whether it was a sequence or a mapping, and says so.
	for !f.eof() {
		l := f.p.src[f.li]
		start := f.ci
		done := false
		for f.ci < len(l) {
			c := l[f.ci]
			if c == ',' || c == '[' || c == ']' || c == '{' || c == '}' {
				done = true
				break
			}
			// A colon ends the scalar only when it separates: "a: b"
			// splits, "http://x" does not.
			if c == ':' && (f.ci+1 == len(l) || isFlowSep(l[f.ci+1])) {
				done = true
				break
			}
			if c == '#' && f.ci > 0 && (l[f.ci-1] == ' ' || l[f.ci-1] == '\t') {
				done = true
				break
			}
			f.ci++
		}
		if frag := strings.TrimRight(l[start:f.ci], " \t"); frag != "" {
			if len(parts) == 0 {
				parts = append(parts, frag)
			} else {
				parts = append(parts, foldSep(blanks)+frag)
			}
			blanks = 0
		}
		if done {
			break
		}

		// The line ran out inside the collection: fold and continue.
		f.li++
		f.ci = 0
		for !f.eof() && strings.TrimSpace(f.p.src[f.li]) == "" {
			blanks++
			f.li++
		}
		if f.eof() {
			break
		}
		l = f.p.src[f.li]
		i := 0
		for i < len(l) && (l[i] == ' ' || l[i] == '\t') {
			i++
		}
		f.ci = i
	}

	if len(parts) == 0 {
		return "", syntaxErr(line, "expected a value in flow collection")
	}
	return strings.Join(parts, ""), nil
}
