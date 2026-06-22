package pcre2lite

// SpanUnset is the Start/End value used for a capture group that did not
// participate in the match.
const SpanUnset = -1

// Span is a half-open byte range [Start, End) within the subject. Offsets are
// byte offsets, never rune indices.
type Span struct {
	Start int
	End   int
}

// IsUnset reports whether the span refers to a non-participating group.
func (s Span) IsUnset() bool { return s.Start < 0 || s.End < 0 }

// Len returns the byte length of the span, or 0 if unset.
func (s Span) Len() int {
	if s.IsUnset() {
		return 0
	}
	return s.End - s.Start
}

// Match holds the byte spans of all groups for a single match. Group 0 is the
// whole match. Groups slices into Input; no captured text is copied.
type Match struct {
	Input  []byte
	Groups []Span
}

// Group returns the captured bytes of group i, or nil if the group is unset or
// out of range. The returned slice aliases Input; do not mutate it.
func (m *Match) Group(i int) []byte {
	if i < 0 || i >= len(m.Groups) {
		return nil
	}
	s := m.Groups[i]
	if s.IsUnset() {
		return nil
	}
	return m.Input[s.Start:s.End]
}

// GroupString returns the captured text of group i as a string.
func (m *Match) GroupString(i int) string {
	return string(m.Group(i))
}

// CompileOptions controls pattern compilation. The boolean fields map directly
// to PCRE2 compile options; MatchLimit and DepthLimit of 0 select the library
// defaults baked into the build.
type CompileOptions struct {
	UTF           bool // PCRE2_UTF: treat pattern and subject as UTF-8
	UCP           bool // PCRE2_UCP: Unicode properties for \d \w etc.
	Caseless      bool // PCRE2_CASELESS: case-insensitive
	Multiline     bool // PCRE2_MULTILINE: ^ and $ match at line breaks
	DotAll        bool // PCRE2_DOTALL: . matches newlines
	Extended      bool // PCRE2_EXTENDED: ignore whitespace and # comments
	Ungreedy      bool // PCRE2_UNGREEDY: invert greediness of quantifiers
	Anchored      bool // PCRE2_ANCHORED: anchor at the start position
	DollarEndOnly bool // PCRE2_DOLLAR_ENDONLY
	FirstLine     bool // PCRE2_FIRSTLINE
	NoAutoCapture bool // PCRE2_NO_AUTO_CAPTURE: (...) are non-capturing
	EndAnchored   bool // PCRE2_ENDANCHORED
	AllowEmpty    bool // PCRE2_ALLOW_EMPTY_CLASS
	DupNames      bool // PCRE2_DUPNAMES: allow duplicate group names
	NeverUCP      bool // PCRE2_NEVER_UCP

	MatchLimit uint32 // 0 = library default
	DepthLimit uint32 // 0 = library default
}

// MatchOption is a bitmask of per-call match options. The numeric values match
// the C P2L_MOPT_* flags; this is asserted at build time in cgo_consts.go.
type MatchOption uint32

const (
	MatchAnchored        MatchOption = 1 << 0
	MatchNotBOL          MatchOption = 1 << 1
	MatchNotEOL          MatchOption = 1 << 2
	MatchNotEmpty        MatchOption = 1 << 3
	MatchNotEmptyAtStart MatchOption = 1 << 4
	MatchEndAnchored     MatchOption = 1 << 5
)
