package regexp2

import (
	"sort"
	"strconv"
	"unicode/utf8"
	"unsafe"

	lib "github.com/VillanCh/go-pcre2-lite/internal/pcre2lite"
)

// Capture is a single captured substring. Index and Length are rune indices
// into the original input, matching github.com/dlclark/regexp2.
type Capture struct {
	text   []rune
	Index  int
	Length int
}

// String returns the captured text.
func (c *Capture) String() string {
	return string(c.text[c.Index : c.Index+c.Length])
}

// Runes returns the captured text as a rune slice.
func (c *Capture) Runes() []rune {
	return c.text[c.Index : c.Index+c.Length]
}

// Group is a matched (or unmatched) capture group. The embedded Capture is the
// last capture of the group.
type Group struct {
	Capture

	Name     string
	Captures []Capture
}

// Match is a single regex result. The embedded Group is group 0 (the whole
// match). Groups other than 0 are constructed lazily on first access, matching
// the allocation profile of regexp2.
type Match struct {
	Group

	regex       *Regexp
	otherGroups []Group
	populated   bool
	lm          *lib.Match

	// iteration state shared with FindNextMatch
	state   *matchState
	endByte int // byte offset (in state.subject) where this match ended
}

// matchState holds the per-input data shared by a match and its successors via
// FindNextMatch: the decoded runes (for Capture text), the UTF-8 bytes fed to
// the engine, and (for non-ASCII input) the byte offset of every rune start.
//
// It also carries a batched iterator used to accelerate the common sequential
// scan (FindStringMatch followed by repeated FindNextMatch). The iterator is
// only used for runs of non-empty matches, where the engine's batched forward
// scan reproduces regexp2's match sequence exactly; any empty match or
// non-sequential access falls back to the proven single-find path.
type matchState struct {
	text       []rune
	subject    []byte
	runeStarts []int // nil when ASCII (byte offset == rune index)
	ascii      bool

	iter       *lib.Iter
	iterActive bool
	iterNext   int // byte offset the next sequential findCore call must request
}

func newMatchStateString(re *Regexp, s string) *matchState {
	text := []rune(s)
	// Pure ASCII fast path: byte offsets equal rune indices, so no rune-start
	// table is needed. This must be a real ASCII check, not len(text)==len(s):
	// a string with invalid UTF-8 bytes also maps each bad byte to a single
	// RuneError, which would spoof the length test and skip normalisation.
	if isASCII(s) {
		return &matchState{text: text, subject: stringBytesNoCopy(s), ascii: true}
	}
	var subject []byte
	if string(text) == s {
		// Valid UTF-8 (non-ASCII): feed the original bytes to the engine.
		subject = stringBytesNoCopy(s)
	} else {
		// Invalid UTF-8: normalise to the RuneError-substituted encoding so the
		// engine never sees a malformed subject.
		subject = []byte(string(text))
	}
	return newMatchState(text, subject)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func newMatchStateRunes(re *Regexp, r []rune) *matchState {
	subject := []byte(string(r))
	if len(r) == len(subject) {
		return &matchState{text: r, subject: subject, ascii: true}
	}
	return newMatchState(r, subject)
}

func newMatchState(text []rune, subject []byte) *matchState {
	runeStarts := make([]int, len(text)+1)
	bp := 0
	for i, r := range text {
		runeStarts[i] = bp
		bp += runeLen(r)
	}
	runeStarts[len(text)] = bp
	return &matchState{text: text, subject: subject, runeStarts: runeStarts}
}

// byteToRune maps a rune-aligned byte offset in subject to a rune index.
func (ms *matchState) byteToRune(b int) int {
	if b <= 0 {
		return 0
	}
	if ms.ascii {
		return b
	}
	if b >= len(ms.subject) {
		return len(ms.text)
	}
	return sort.Search(len(ms.runeStarts), func(i int) bool {
		return ms.runeStarts[i] >= b
	})
}

// alignByteStart validates that the public byte offset startAt (into the
// original string s) lands on a rune boundary, and returns the corresponding
// byte offset inside ms.subject (which may differ from s when s contained
// invalid UTF-8).
func (ms *matchState) alignByteStart(s string, startAt int) (int, bool) {
	if startAt == 0 {
		return 0, true
	}
	if startAt == len(s) {
		return len(ms.subject), true
	}
	if ms.ascii {
		// Every byte boundary is a rune boundary and subject == s.
		return startAt, true
	}
	runeIdx := 0
	for bi := range s {
		if bi == startAt {
			return ms.runeStarts[runeIdx], true
		}
		if bi > startAt {
			return 0, false
		}
		runeIdx++
	}
	return 0, false
}

func (ms *matchState) buildMatch(re *Regexp, lm *lib.Match) *Match {
	m := &Match{regex: re, state: ms, lm: lm}
	m.Group = ms.makeGroup(re, 0, lm.Groups[0])
	m.endByte = lm.Groups[0].End
	return m
}

// armIter marks the iterator as ready to serve a sequential scan beginning at
// fromByte. Creation of the (relatively large) iterator buffer is deferred to
// the first iterServe call, so callers that take only the first match -- e.g.
// FindStringMatch with no FindNextMatch -- never pay the allocation.
func (ms *matchState) armIter(re *Regexp, fromByte int) {
	ms.iter = nil
	ms.iterActive = true
	ms.iterNext = fromByte
}

// iterServe returns the next match from the batched iterator, creating it lazily
// on first use. It keeps the iterator armed only while matches are non-empty; on
// an empty match (or exhaustion/error) it disarms so the next step falls back to
// the single-find path, which is byte-for-byte identical to regexp2 across empty
// matches.
func (ms *matchState) iterServe(re *Regexp) (*Match, error) {
	if ms.iter == nil {
		ms.iter = re.re.NewIter(ms.subject, ms.iterNext, 0)
	}
	lm := ms.iter.Next()
	if err := ms.iter.Err(); err != nil {
		ms.iterActive = false
		return nil, err
	}
	if lm == nil {
		ms.iterActive = false
		return nil, nil
	}
	g0 := lm.Groups[0]
	m := ms.buildMatch(re, lm)
	if g0.End > g0.Start {
		ms.iterNext = g0.End // next sequential call: plain search from here
	} else {
		ms.iterActive = false // empty match: fall back for the advance step
	}
	return m, nil
}

// populate builds the groups other than group 0 on first access. This mirrors
// regexp2, which keeps the per-group object construction off the hot path for
// callers that only need the whole match.
func (m *Match) populate() {
	if m.populated {
		return
	}
	m.populated = true
	gc := m.regex.re.CaptureCount()
	if gc <= 1 {
		return
	}
	ms := m.state
	lm := m.lm
	m.otherGroups = make([]Group, gc-1)
	for i := 1; i < gc; i++ {
		var span lib.Span
		if i < len(lm.Groups) {
			span = lm.Groups[i]
		} else {
			span = lib.Span{Start: lib.SpanUnset, End: lib.SpanUnset}
		}
		m.otherGroups[i-1] = ms.makeGroup(m.regex, i, span)
	}
}

func (ms *matchState) makeGroup(re *Regexp, i int, span lib.Span) Group {
	g := Group{}
	g.text = ms.text
	if name, ok := re.re.NumberedGroupName(i); ok {
		g.Name = name
	} else {
		g.Name = strconv.Itoa(i)
	}
	if span.IsUnset() {
		g.Captures = []Capture{}
		return g
	}
	ri := ms.byteToRune(span.Start)
	rend := ms.byteToRune(span.End)
	g.Index = ri
	g.Length = rend - ri
	g.Captures = []Capture{{text: ms.text, Index: ri, Length: rend - ri}}
	return g
}

// GroupCount returns the number of groups, including group 0.
func (m *Match) GroupCount() int {
	return m.regex.re.CaptureCount()
}

// GroupByName returns the group with the given name, or nil.
func (m *Match) GroupByName(name string) *Group {
	num := m.regex.GroupNumberFromName(name)
	if num < 0 {
		return nil
	}
	return m.GroupByNumber(num)
}

// GroupByNumber returns the group with the given number, or nil.
func (m *Match) GroupByNumber(num int) *Group {
	if num == 0 {
		return &m.Group
	}
	m.populate()
	if num < 0 || num > len(m.otherGroups) {
		return nil
	}
	return &m.otherGroups[num-1]
}

// Groups returns all groups, starting with group 0.
func (m *Match) Groups() []Group {
	m.populate()
	g := make([]Group, len(m.otherGroups)+1)
	g[0] = m.Group
	copy(g[1:], m.otherGroups)
	return g
}

func stringBytesNoCopy(s string) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

func runeLen(r rune) int {
	switch {
	case r < 0:
		return 3 // utf8.RuneError encoding length
	case r < 0x80:
		return 1
	case r < 0x800:
		return 2
	case r < 0x10000:
		return 3
	default:
		return 4
	}
}
