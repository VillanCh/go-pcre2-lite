package pcre2lite

import (
	"errors"
	"fmt"
)

// Sentinel errors returned by matching and lifecycle operations. They are
// comparable with errors.Is.
var (
	// ErrNoMatch is exported for callers that prefer an error to a nil result.
	// The Find* / Match* methods themselves report "no match" by returning a
	// nil *Match or false without an error, mirroring the standard library.
	ErrNoMatch = errors.New("pcre2lite: no match")

	// ErrMatchLimit indicates the backtracking match limit was exceeded. This
	// usually signals catastrophic backtracking in the pattern or input.
	ErrMatchLimit = errors.New("pcre2lite: match limit exceeded")

	// ErrDepthLimit indicates the backtracking depth (recursion) limit was hit.
	ErrDepthLimit = errors.New("pcre2lite: depth limit exceeded")

	// ErrBadUTF indicates invalid UTF-8 in the subject (UTF mode) or a start
	// offset that is not on a character boundary.
	ErrBadUTF = errors.New("pcre2lite: invalid UTF-8 in subject")

	// ErrClosed is returned when matching against a closed Regexp.
	ErrClosed = errors.New("pcre2lite: Regexp is closed")

	// ErrShortBuffer indicates the supplied capture buffer was too small.
	ErrShortBuffer = errors.New("pcre2lite: capture buffer too small")

	// ErrPartial indicates a partial match (only possible with partial options).
	ErrPartial = errors.New("pcre2lite: partial match")

	// ErrNoMemory indicates PCRE2 could not allocate memory for the operation.
	ErrNoMemory = errors.New("pcre2lite: out of memory")

	// ErrInternal wraps any other PCRE2 execution error.
	ErrInternal = errors.New("pcre2lite: internal error")
)

// CompileError describes a pattern compilation failure, including the byte
// offset within the pattern where PCRE2 detected the problem.
type CompileError struct {
	Pattern string
	Code    int
	Offset  int
	Message string
}

func (e *CompileError) Error() string {
	return fmt.Sprintf("pcre2lite: compile error in %q at offset %d: %s (code %d)",
		e.Pattern, e.Offset, e.Message, e.Code)
}

// MatchError wraps an unexpected PCRE2 execution error with its native code and
// message. Use errors.Is with the sentinels above for the common categories.
type MatchError struct {
	Code    int
	Message string
	kind    error
}

func (e *MatchError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("pcre2lite: match error: %s (code %d)", e.Message, e.Code)
	}
	return fmt.Sprintf("pcre2lite: match error (code %d)", e.Code)
}

func (e *MatchError) Unwrap() error { return e.kind }
