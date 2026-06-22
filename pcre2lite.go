// Package pcre2lite is a high-performance, embeddable regular expression
// runtime for Go. It compiles a trimmed PCRE2 8-bit interpreter from source via
// cgo, with JIT permanently disabled, and exposes a small byte-oriented API.
//
// It is intended as a faster, lower-allocation replacement for the execution
// path of github.com/dlclark/regexp2 in scenarios that need lookaround,
// lookbehind, backreferences or named captures that the standard library
// regexp (RE2) cannot provide.
//
// This root package re-exports the low-level API. For a drop-in regexp2
// replacement, use the sibling package .../go-pcre2-lite/regexp2.
//
// CGO_ENABLED=1 is required. All offsets are UTF-8 byte offsets.
package pcre2lite

import lib "github.com/VillanCh/go-pcre2-lite/internal/pcre2lite"

// Core types (aliases to the cgo implementation).
type (
	// Regexp is a compiled pattern, safe for concurrent matching.
	Regexp = lib.Regexp
	// CompileOptions controls compilation.
	CompileOptions = lib.CompileOptions
	// Span is a half-open byte range [Start, End).
	Span = lib.Span
	// Match holds the byte spans of all groups for a single match.
	Match = lib.Match
	// MatchOption is a per-call match flag.
	MatchOption = lib.MatchOption
	// CompileError describes a pattern compilation failure.
	CompileError = lib.CompileError
	// MatchError wraps an unexpected PCRE2 execution error.
	MatchError = lib.MatchError
)

// SpanUnset marks a non-participating capture group.
const SpanUnset = lib.SpanUnset

// Per-call match options.
const (
	MatchAnchored        = lib.MatchAnchored
	MatchNotBOL          = lib.MatchNotBOL
	MatchNotEOL          = lib.MatchNotEOL
	MatchNotEmpty        = lib.MatchNotEmpty
	MatchNotEmptyAtStart = lib.MatchNotEmptyAtStart
	MatchEndAnchored     = lib.MatchEndAnchored
)

// Sentinel errors.
var (
	ErrNoMatch     = lib.ErrNoMatch
	ErrMatchLimit  = lib.ErrMatchLimit
	ErrDepthLimit  = lib.ErrDepthLimit
	ErrBadUTF      = lib.ErrBadUTF
	ErrClosed      = lib.ErrClosed
	ErrShortBuffer = lib.ErrShortBuffer
	ErrPartial     = lib.ErrPartial
	ErrNoMemory    = lib.ErrNoMemory
	ErrInternal    = lib.ErrInternal
)

// Compile compiles pattern with the given options.
func Compile(pattern string, opts CompileOptions) (*Regexp, error) {
	return lib.Compile(pattern, opts)
}

// MustCompile is like Compile but panics on error.
func MustCompile(pattern string, opts CompileOptions) *Regexp {
	return lib.MustCompile(pattern, opts)
}

// Version returns the embedded PCRE2 version string (JIT disabled).
func Version() string {
	return lib.Version()
}
