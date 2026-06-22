// Package regexp2 is a drop-in replacement for github.com/dlclark/regexp2 that
// executes matches with the embedded PCRE2 8-bit interpreter (JIT permanently
// disabled) instead of the pure-Go backtracking engine.
//
// The exported API mirrors github.com/dlclark/regexp2 so that migrating is, in
// the common case, a matter of changing the import path. Match positions
// (Capture.Index / Capture.Length) are reported as rune indices, exactly like
// regexp2, even though the underlying engine works on UTF-8 bytes.
//
// This package does NOT claim 100% semantic parity with .NET or regexp2. See
// MIGRATION.md for the documented differences (group numbering with mixed
// named/unnamed groups, repeated-capture history, RightToLeft, timeouts, and a
// few syntax edge cases). Use the differential tests to validate your rules.
package regexp2

import (
	"errors"
	"math"
	"strconv"
	"time"
	"unicode/utf8"

	lib "github.com/VillanCh/go-pcre2-lite/internal/pcre2lite"
)

var (
	// DefaultMatchTimeout used when running regexp matches -- "forever".
	// Provided for API compatibility; this backend does not enforce a wall
	// clock timeout (see MatchTimeout below).
	DefaultMatchTimeout = time.Duration(math.MaxInt64)
	// DefaultUnmarshalOptions used when unmarshaling a regex from text.
	DefaultUnmarshalOptions = None
)

// RegexOptions impact the parsing and runtime behavior of a regex. The values
// match github.com/dlclark/regexp2 exactly.
type RegexOptions int32

const (
	None                    RegexOptions = 0x0
	IgnoreCase                           = 0x0001 // "i"
	Multiline                            = 0x0002 // "m"
	ExplicitCapture                      = 0x0004 // "n"
	Compiled                             = 0x0008 // "c"
	Singleline                           = 0x0010 // "s"
	IgnorePatternWhitespace              = 0x0020 // "x"
	RightToLeft                          = 0x0040 // "r"
	Debug                                = 0x0080 // "d"
	ECMAScript                           = 0x0100 // "e"
	RE2                                  = 0x0200 // RE2 compatibility mode
	Unicode                              = 0x0400 // "u"
)

// Regexp is a compiled regular expression. It is safe for concurrent use by
// multiple goroutines.
type Regexp struct {
	// MatchTimeout is accepted for API compatibility. This backend relies on
	// the PCRE2 match/depth limits rather than a wall-clock timeout, so a
	// finite value here does not abort a running match. See SetMatchLimits.
	MatchTimeout time.Duration

	pattern string
	options RegexOptions
	re      *lib.Regexp
}

// Compile parses a regular expression and returns a Regexp.
func Compile(expr string, opt RegexOptions) (*Regexp, error) {
	co := optionsToCompile(opt)
	re, err := lib.Compile(expr, co)
	if err != nil {
		return nil, err
	}
	return &Regexp{
		MatchTimeout: DefaultMatchTimeout,
		pattern:      expr,
		options:      opt,
		re:           re,
	}, nil
}

// MustCompile is like Compile but panics if the expression cannot be parsed.
func MustCompile(str string, opt RegexOptions) *Regexp {
	re, err := Compile(str, opt)
	if err != nil {
		panic(`regexp2: Compile(` + quote(str) + `): ` + err.Error())
	}
	return re
}

func optionsToCompile(opt RegexOptions) lib.CompileOptions {
	co := lib.CompileOptions{
		// Always UTF so that "." and counting behave per rune, matching the
		// rune-oriented semantics of regexp2.
		UTF: true,
	}
	if opt&IgnoreCase != 0 {
		co.Caseless = true
	}
	if opt&Multiline != 0 {
		co.Multiline = true
	}
	if opt&Singleline != 0 {
		// .NET "Singleline" == dot matches newline == PCRE2 DOTALL.
		co.DotAll = true
	}
	if opt&IgnorePatternWhitespace != 0 {
		co.Extended = true
	}
	if opt&ExplicitCapture != 0 {
		co.NoAutoCapture = true
	}
	// .NET classes (\d \w \s) are Unicode-aware by default; ECMAScript mode
	// restricts them to ASCII.
	if opt&ECMAScript == 0 {
		co.UCP = true
	}
	return co
}

// SetMatchLimits recompiles the backend limits for catastrophic-backtracking
// protection. matchLimit and depthLimit of 0 keep the build defaults. This is
// an extension beyond the regexp2 API.
func (re *Regexp) SetMatchLimits(matchLimit, depthLimit uint32) error {
	co := optionsToCompile(re.options)
	co.MatchLimit = matchLimit
	co.DepthLimit = depthLimit
	newRe, err := lib.Compile(re.pattern, co)
	if err != nil {
		return err
	}
	old := re.re
	re.re = newRe
	if old != nil {
		old.Close()
	}
	return nil
}

// Escape adds backslashes to any special characters in the input string.
func Escape(input string) string {
	return escapeImpl(input)
}

// Unescape removes any backslashes from previously-escaped special characters.
func Unescape(input string) (string, error) {
	return unescapeImpl(input)
}

// SetTimeoutCheckPeriod is a no-op kept for API compatibility. This backend has
// no timeout goroutine.
func SetTimeoutCheckPeriod(d time.Duration) {}

// StopTimeoutClock is a no-op kept for API compatibility.
func StopTimeoutClock() {}

// String returns the source text used to compile the regular expression.
func (re *Regexp) String() string {
	return re.pattern
}

func quote(s string) string {
	if strconv.CanBackquote(s) {
		return "`" + s + "`"
	}
	return strconv.Quote(s)
}

// RightToLeft reports whether the RightToLeft option was set. Note: this
// backend scans left-to-right; see MIGRATION.md.
func (re *Regexp) RightToLeft() bool {
	return re.options&RightToLeft != 0
}

// Debug reports whether the Debug option was set.
func (re *Regexp) Debug() bool {
	return re.options&Debug != 0
}

// MatchString returns true if the string matches the regex.
func (re *Regexp) MatchString(s string) (bool, error) {
	return re.re.Match(subjectBytes(s))
}

// MatchRunes returns true if the runes match the regex.
func (re *Regexp) MatchRunes(r []rune) (bool, error) {
	return re.re.Match([]byte(string(r)))
}

// FindStringMatch searches the input string for a Regexp match.
func (re *Regexp) FindStringMatch(s string) (*Match, error) {
	ms := newMatchStateString(re, s)
	return re.findCore(ms, 0, false)
}

// FindRunesMatch searches the input rune slice for a Regexp match.
func (re *Regexp) FindRunesMatch(r []rune) (*Match, error) {
	ms := newMatchStateRunes(re, r)
	return re.findCore(ms, 0, false)
}

// FindStringMatchStartingAt searches starting at the byte index startAt, which
// must align to the start of a rune (matching regexp2 semantics).
func (re *Regexp) FindStringMatchStartingAt(s string, startAt int) (*Match, error) {
	if startAt > len(s) {
		return nil, errors.New("startAt must be less than the length of the input string")
	}
	ms := newMatchStateString(re, s)
	if startAt < 0 {
		if re.RightToLeft() {
			return re.findCore(ms, len(ms.subject), false)
		}
		return re.findCore(ms, 0, false)
	}
	byteStart, ok := ms.alignByteStart(s, startAt)
	if !ok {
		return nil, errors.New("startAt must align to the start of a valid rune in the input string")
	}
	return re.findCore(ms, byteStart, false)
}

// FindRunesMatchStartingAt searches the rune slice starting at rune index startAt.
func (re *Regexp) FindRunesMatchStartingAt(r []rune, startAt int) (*Match, error) {
	ms := newMatchStateRunes(re, r)
	if startAt < 0 {
		startAt = 0
	}
	if startAt > len(r) {
		return nil, errors.New("startAt must be less than the length of the input")
	}
	return re.findCore(ms, ms.runeStarts[startAt], false)
}

// FindNextMatch returns the next match in the same input as the match parameter.
func (re *Regexp) FindNextMatch(m *Match) (*Match, error) {
	if m == nil {
		return nil, nil
	}
	ms := m.state
	startByte := m.endByte
	prevEmpty := m.Length == 0
	if prevEmpty {
		if m.endByte >= len(ms.subject) {
			return nil, nil
		}
	}
	return re.findCore(ms, startByte, prevEmpty)
}

// findCore runs one match against ms.subject at byte offset startByte. When
// prevEmpty is true the previous match was empty, so the start is advanced by
// one code unit before searching (mirroring regexp2's FindNextMatch).
//
// A batched iterator serves the common sequential scan (a plain search whose
// start equals where the previous non-empty match ended). Any deviation -- an
// empty previous match, a jump from FindStringMatchStartingAt, or iteration
// over an old Match -- falls back to a single Find and re-arms the iterator, so
// behaviour is identical to the unbatched path while the hot loop stays fast.
func (re *Regexp) findCore(ms *matchState, startByte int, prevEmpty bool) (*Match, error) {
	if !prevEmpty && ms.iterActive && startByte == ms.iterNext {
		return ms.iterServe(re)
	}
	ms.iterActive = false

	if prevEmpty {
		startByte++
		if re.re.UTF() {
			for startByte < len(ms.subject) && ms.subject[startByte]&0xC0 == 0x80 {
				startByte++
			}
		}
	}
	if startByte < 0 {
		startByte = 0
	}
	if startByte > len(ms.subject) {
		return nil, nil
	}
	lm, err := re.re.Find(ms.subject, startByte)
	if err != nil {
		return nil, err
	}
	if lm == nil {
		return nil, nil
	}
	m := ms.buildMatch(re, lm)
	// Arm the iterator for the next sequential call, but only after a non-empty
	// match (so the next call is a plain search from the match end).
	g0 := lm.Groups[0]
	if g0.End > g0.Start {
		ms.armIter(re, g0.End)
	}
	return m, nil
}

// GetGroupNames returns the set of names used for capture groups, with unnamed
// groups represented by their decimal number.
func (re *Regexp) GetGroupNames() []string {
	n := re.re.CaptureCount()
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = re.GroupNameFromNumber(i)
	}
	return out
}

// GetGroupNumbers returns the group numbers 0..n-1.
func (re *Regexp) GetGroupNumbers() []int {
	n := re.re.CaptureCount()
	out := make([]int, n)
	for i := 0; i < n; i++ {
		out[i] = i
	}
	return out
}

// GroupNameFromNumber returns the name for a group number, or its decimal
// representation for an unnamed group, or "" if out of range.
func (re *Regexp) GroupNameFromNumber(i int) string {
	if name, ok := re.re.NumberedGroupName(i); ok {
		return name
	}
	if i >= 0 && i < re.re.CaptureCount() {
		return strconv.Itoa(i)
	}
	return ""
}

// GroupNumberFromName returns the group number for a name, or -1 if unknown.
func (re *Regexp) GroupNumberFromName(name string) int {
	if n, ok := re.re.NamedGroupNumber(name); ok {
		return n
	}
	// Numeric name?
	if n, err := strconv.Atoi(name); err == nil {
		if n >= 0 && n < re.re.CaptureCount() {
			return n
		}
	}
	return -1
}

// MarshalText implements encoding.TextMarshaler.
func (re *Regexp) MarshalText() ([]byte, error) {
	return []byte(re.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler by compiling the value.
func (re *Regexp) UnmarshalText(text []byte) error {
	newRE, err := Compile(string(text), DefaultUnmarshalOptions)
	if err != nil {
		return err
	}
	*re = *newRE
	return nil
}

// subjectBytes returns valid UTF-8 bytes for s without copying when s is
// already valid UTF-8. Invalid sequences are normalized to U+FFFD, matching the
// rune view used by regexp2.
func subjectBytes(s string) []byte {
	if utf8.ValidString(s) {
		return stringBytesNoCopy(s)
	}
	return []byte(string([]rune(s)))
}
