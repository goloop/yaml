package yaml

import "fmt"

// A SyntaxError reports a document this package will not read, and the
// line it gave up on. The line is the point of it: a configuration file
// that fails to load is read by a person looking for the mistake, and an
// editor showing them the spot needs the number, not a sentence to parse
// back out of the message.
//
//	var se *yaml.SyntaxError
//	if errors.As(err, &se) {
//		editor.Highlight(se.Line)
//	}
type SyntaxError struct {
	Line int    // 1-based line of the input
	Msg  string // what was wrong, without the "yaml: line N:" prefix
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("yaml: line %d: %s", e.Line, e.Msg)
}

// A TypeError reports a value the document holds but the target cannot
// take: a mapping where a number was asked for, a number too large for
// its field, a key a strict decode does not recognise.
type TypeError struct {
	Line int    // 1-based line of the offending value
	Msg  string // what could not be done, without the prefix
}

func (e *TypeError) Error() string {
	return fmt.Sprintf("yaml: line %d: %s", e.Line, e.Msg)
}

func syntaxErr(line int, format string, args ...any) error {
	return &SyntaxError{Line: line, Msg: fmt.Sprintf(format, args...)}
}

func typeErr(line int, format string, args ...any) error {
	return &TypeError{Line: line, Msg: fmt.Sprintf(format, args...)}
}
