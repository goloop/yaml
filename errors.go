package yaml

import (
	"errors"
	"fmt"
)

// Sentinel errors this package returns. Test for them with errors.Is.
// The names match the ones the .env side of this toolkit uses, so the
// same check reads the same way whichever format a program loads its
// configuration from.
var (
	// ErrNilObject is returned when the value passed to Unmarshal or
	// Marshal is nil.
	ErrNilObject = errors.New("yaml: object is nil")

	// ErrNotPointer is returned when Unmarshal is not given a non-nil
	// pointer to decode into.
	ErrNotPointer = errors.New("yaml: object must be a non-nil pointer")

	// ErrNotStruct is returned when a struct was required and the value
	// is something else.
	ErrNotStruct = errors.New("yaml: object must be a pointer to a struct")

	// ErrInvalidObject is returned by Marshal when the value cannot be
	// encoded at all.
	ErrInvalidObject = errors.New("yaml: object cannot be encoded")

	// ErrRequired is returned when a field tagged as required has no key
	// in the document and no def default.
	ErrRequired = errors.New("yaml: required key is not set")
)

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
//
// It may also carry one of the sentinel errors above, so a caller can
// ask what went wrong and where it went wrong without choosing between
// the two:
//
//	if errors.Is(err, yaml.ErrRequired) { ... }
//
//	var te *yaml.TypeError
//	if errors.As(err, &te) { editor.Highlight(te.Line) }
type TypeError struct {
	Line int    // 1-based line of the offending value
	Msg  string // what could not be done, without the prefix
	err  error  // sentinel this error also is, if any
}

func (e *TypeError) Error() string {
	return fmt.Sprintf("yaml: line %d: %s", e.Line, e.Msg)
}

// Unwrap reports the sentinel this error also is, so errors.Is can find
// it. It is nil for an error that is only about this one value.
func (e *TypeError) Unwrap() error { return e.err }

func syntaxErr(line int, format string, args ...any) error {
	return &SyntaxError{Line: line, Msg: fmt.Sprintf(format, args...)}
}

func typeErr(line int, format string, args ...any) error {
	return &TypeError{Line: line, Msg: fmt.Sprintf(format, args...)}
}

// sentinelErr is typeErr for a failure that is also one of the package's
// sentinels.
func sentinelErr(line int, sentinel error, format string, args ...any) error {
	return &TypeError{
		Line: line,
		Msg:  fmt.Sprintf(format, args...),
		err:  sentinel,
	}
}
