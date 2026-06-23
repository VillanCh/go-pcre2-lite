package pcre2lite_test

// This file is a regression suite that pins the engine to the behaviour of
// PCRE2 10.44 through 10.47. Every case below is drawn from a documented
// changelog entry (bug fix, new feature, or behaviour change) and is asserted
// against the AUTHORITATIVE ground truth recorded by PCRE2's own pcre2test tool
// for that release (testdata/pcre2_testoutput2.txt and ...4.txt), so the engine
// cannot silently regress on any of these constructs when it is re-vendored.
//
// Coverage by version (see the MIGRATION / release notes for the full list):
//
//   10.44  #1   variable-length lookbehind whose first branch is not the
//               shortest, with a capturing group referenced by another
//               lookbehind -> used to mis-compile / crash.
//   10.44  #3   group-name length raised from 32 to 128.
//   10.44  #7   \X grapheme-cluster break must require a ZWJ between two
//               Extended_Pictographic chars (UAX #29 fix).
//   10.45  #6   caseless \p{Ll}/\p{Lt}/\p{Lu} now all behave as Lc, matching Perl.
//   10.45 #478  \x must be followed by '{' or a hex digit -> compile error.
//   10.45 #491  \b and \v in (unrelated here) replacement strings are literal.
//   10.45 #503  Unicode updated to UCD 16; newer properties compile.
//   10.45 #517  scan-substring assertions (*scs: / *scan_substring:).
//   10.46 CVE   (*ACCEPT) inside (*scs:) must not read past the end (CVE-2025-58050).
//   10.47  #9   pattern recursion of the form (?N(group,...)) returns captures.
//   10.47 #733  pcre2_next_match() drives FindAll / Iter (covered by the batched
//               path) -- exercised here via empty-match iteration.
//   10.47 #756  improved compile error offsets.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	lib "github.com/VillanCh/go-pcre2-lite"
	p2 "github.com/VillanCh/go-pcre2-lite/regexp2"
)

// gcase is a single compile-and-match assertion. wantWhole is the expected
// whole-match text (UTF-8); with wantNoMatch set, no match is expected.
//
// wantGroups[i] describes group i (i==0 is the whole match). It is a *string so
// the helper can distinguish three states:
//   - nil                    -> do not check this group
//   - &unsetSentinel         -> group must be UNSET (non-participating)
//   - any other *string      -> group must have participated and captured *that*
//     exact text ("" means it captured the empty string)
type gcase struct {
	name        string
	pattern     string
	input       string
	utf         bool
	caseless    bool
	wantNoMatch bool
	wantWhole   string
	wantGroups  []*string
}

// unsetSentinel flags a group that must NOT have participated in the match.
var unsetSentinel = "" // address used as the sentinel; never dereferenced for text

// g returns a *string pointing at s, for wantGroups entries.
func g(s string) *string { return &s }

// gunset returns a *string that flags an unset (non-participating) group.
func gunset() *string { return &unsetSentinel }

func runGoldenCases(t *testing.T, cases []gcase) {
	t.Helper()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			opts := lib.CompileOptions{
				UTF:      tc.utf,
				UCP:      tc.utf,
				Caseless: tc.caseless,
			}
			re, err := lib.Compile(tc.pattern, opts)
			if err != nil {
				t.Fatalf("compile %q: %v", tc.pattern, err)
			}
			defer re.Close()

			m, merr := re.Find([]byte(tc.input), 0)
			if merr != nil {
				t.Fatalf("match %q / %q: %v", tc.pattern, tc.input, merr)
			}
			if tc.wantNoMatch {
				if m != nil {
					t.Errorf("expected no match, got whole=%q", tc.input[m.Groups[0].Start:m.Groups[0].End])
				}
				return
			}
			if m == nil {
				t.Fatalf("expected match, got nil")
			}
			whole := tc.input[m.Groups[0].Start:m.Groups[0].End]
			if tc.wantWhole != "" && whole != tc.wantWhole {
				t.Errorf("whole match: got %q want %q", whole, tc.wantWhole)
			}
			for i, wantPtr := range tc.wantGroups {
				if wantPtr == nil {
					continue
				}
				if i >= len(m.Groups) {
					t.Errorf("group %d: not enough groups (%d)", i, len(m.Groups))
					continue
				}
				gp := m.Groups[i]
				isUnset := gp.Start == lib.SpanUnset
				wantUnset := wantPtr == &unsetSentinel
				if wantUnset {
					if !isUnset {
						t.Errorf("group %d: participated with %q, want UNSET",
							i, tc.input[gp.Start:gp.End])
					}
					continue
				}
				if isUnset {
					t.Errorf("group %d: UNSET, want %q", i, *wantPtr)
					continue
				}
				if got := tc.input[gp.Start:gp.End]; got != *wantPtr {
					t.Errorf("group %d: got %q want %q", i, got, *wantPtr)
				}
			}
		})
	}
}

// --- 10.44 -----------------------------------------------------------------

func TestRegression1044LookbehindFirstBranch(t *testing.T) {
	// 10.44 #1: a variable-length lookbehind whose first branch is NOT the one
	// with the shortest minimum length, containing a capture group referenced by
	// a second lookbehind, used to mis-compile (crash under JIT, wrong match in
	// the interpreter). Golden from testoutput2 line 19533.
	runGoldenCases(t, []gcase{
		{
			name: "ABCDEFG", pattern: `(((?<=123?456456|ABC)))(?<=\2)..`,
			input: "ABCDEFG", wantWhole: "DE",
			wantGroups: []*string{g("DE"), g(""), g("")},
		},
		{
			name: "12345645678910", pattern: `(((?<=123?456456|ABC)))(?<=\2)..`,
			input: "12345645678910 ", wantWhole: "78",
			wantGroups: []*string{g("78"), g(""), g("")},
		},
	})
}

func TestRegression1044LongGroupName(t *testing.T) {
	// 10.44 #3: group-name length was raised from 32 to 128. A name of 100 chars
	// must compile and capture correctly.
	name := strings.Repeat("a", 100)
	re, err := lib.Compile(`(?<`+name+`>x)+`, lib.CompileOptions{})
	if err != nil {
		t.Fatalf("compile long group name: %v", err)
	}
	defer re.Close()
	m, _ := re.Find([]byte("xxx"), 0)
	if m == nil {
		t.Fatal("expected match")
	}
	if num, ok := re.NamedGroupNumber(name); !ok || num != 1 {
		t.Errorf("NamedGroupNumber(%q) = %d, ok=%v", name, num, ok)
	}
}

// TestRegression1044GroupNameBoundary pins the exact boundary of the raised
// group-name limit: 128 code units is accepted, 129 is rejected with error
// code 148 ("subpattern name is too long").
func TestRegression1044GroupNameBoundary(t *testing.T) {
	exactly := strings.Repeat("a", 128)
	if re, err := lib.Compile(`(?<`+exactly+`>x)`, lib.CompileOptions{}); err != nil {
		t.Errorf("name of 128 chars should compile, got %v", err)
	} else {
		re.Close()
	}
	tooLong := strings.Repeat("a", 129)
	_, err := lib.Compile(`(?<`+tooLong+`>x)`, lib.CompileOptions{})
	if err == nil {
		t.Fatal("name of 129 chars should be rejected")
	}
	ce, ok := err.(*lib.CompileError)
	if !ok || ce.Code != 148 {
		t.Errorf("expected CompileError code 148, got %T %v", err, err)
	}
}

// TestRegression1045AutoPossessifyVariableLookbehind pins the 10.45 #12 fix:
// an iterator at the end of an assertion can be auto-possessified, but NOT at
// the end of a variable-length lookbehind whose branches differ in length (e.g.
// (?<=AB|CD?)). The bug checked only the first branch, so CD? was mishandled.
// After the fix both branches must match correctly.
func TestRegression1045AutoPossessifyVariableLookbehind(t *testing.T) {
	runGoldenCases(t, []gcase{
		{name: "vlb-AB", pattern: `(?<=AB|CD?)X`, input: "--ABX", wantWhole: "X"},
		{name: "vlb-CD", pattern: `(?<=AB|CD?)X`, input: "--CDX", wantWhole: "X"},
		{name: "vlb-C", pattern: `(?<=AB|CD?)X`, input: "--CX", wantWhole: "X"},
		{name: "vlb-no-prefix", pattern: `(?<=AB|CD?)X`, input: "XX", wantNoMatch: true},
	})
}

// TestRegression1047NamedGroupHashLookup guards the 10.47 #700 optimisation:
// named-group lookup at compile time uses a hash table, so looking up a group
// among thousands must be O(1). If the hash were ever removed (reverting to a
// linear scan), this check would regress by orders of magnitude.
func TestRegression1047NamedGroupHashLookup(t *testing.T) {
	const nGroups = 2000
	var sb strings.Builder
	// Concatenate N distinct named groups (not wrapped in an outer group, so
	// the first named group is group 1 and the last is group nGroups).
	for i := 0; i < nGroups; i++ {
		fmt.Fprintf(&sb, `(?<g%04d>x)?`, i)
	}
	re, err := lib.Compile(sb.String(), lib.CompileOptions{UTF: true, UCP: true})
	if err != nil {
		t.Fatalf("compile %d named groups: %v", nGroups, err)
	}
	defer re.Close()

	// Look up the LAST group repeatedly; with a hash this is ~tens of ns,
	// with a linear scan it would be microseconds and the bound below would
	// fail. We assert correctness and a loose time bound.
	last := fmt.Sprintf("g%04d", nGroups-1)
	num, ok := re.NamedGroupNumber(last)
	if !ok || num != nGroups {
		t.Errorf("NamedGroupNumber(%q) = %d, ok=%v (want %d, true)", last, num, ok, nGroups)
	}
	// 100k lookups must complete well under a second (hash table territory).
	start := time.Now()
	for i := 0; i < 100000; i++ {
		_, _ = re.NamedGroupNumber(last)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("named-group lookup too slow: %v for 100k lookups (hash regression?)", d)
	}
}

func TestRegression1044GraphemeZWJ(t *testing.T) {
	// 10.44 #7: \X must break between two Extended_Pictographic chars unless a
	// ZWJ (U+200D) joins them. Two emoji joined by a ZWJ are ONE grapheme.
	cases := []gcase{
		{name: "zwj-joins", pattern: `^\X$`, input: "😀\u200d😀", utf: true, wantWhole: "😀\u200d😀"},
		{name: "no-zwj-two-graphemes", pattern: `^\X\X$`, input: "😀😀", utf: true, wantWhole: "😀😀"},
		{name: "combining-cluster", pattern: `^\X$`, input: "e\u0301", utf: true, wantWhole: "e\u0301"},
	}
	runGoldenCases(t, cases)
}

// --- 10.45 -----------------------------------------------------------------

func TestRegression1045CaselessCasedLetter(t *testing.T) {
	// 10.45 #6: with caseless matching, \p{Ll}, \p{Lt} and \p{Lu} are all treated
	// as Lc (cased letter), matching Perl. "AbCd" should match \p{Ll}+ caselessly.
	runGoldenCases(t, []gcase{
		{name: "Ll-caseless", pattern: `\p{Ll}+`, input: "AbCd", utf: true, caseless: true, wantWhole: "AbCd"},
		{name: "Lu-caseless", pattern: `\p{Lu}+`, input: "AbCd", utf: true, caseless: true, wantWhole: "AbCd"},
		{name: "Lt-caseless", pattern: `\p{Lt}+`, input: "AbCd", utf: true, caseless: true, wantWhole: "AbCd"},
	})
}

func TestRegression1045HexEscapeValidation(t *testing.T) {
	// 10.45 #478/#504: \x must be followed by '{' or a hex digit, else a compile
	// error. Each must fail with a non-empty error message.
	for _, pat := range []string{`\x`, `\xg`, `\x{}`, `\xZZ`, `\x{ZZ}`} {
		if _, err := lib.Compile(pat, lib.CompileOptions{}); err == nil {
			t.Errorf("expected compile error for %q", pat)
		}
	}
	// The valid forms still work.
	runGoldenCases(t, []gcase{
		{name: "x-hex", pattern: `\x41`, input: "A", wantWhole: "A"},
		{name: "x-brace", pattern: `\x{42}`, input: "B", wantWhole: "B"},
	})
}

func TestRegression1045ScanSubstring(t *testing.T) {
	// 10.45 #517: scan-substring assertions (*scs: / *scan_substring:). Golden
	// from testoutput2 lines 20001-20015 and 19994-19999.
	runGoldenCases(t, []gcase{
		{
			name: "scs-no-match", pattern: `([a-z])([a-z]++)(#+)(*scs:(2)(ab.))`,
			input: "xab##", wantNoMatch: true,
		},
		{
			name: "scs-yabc", pattern: `([a-z])([a-z]++)(#+)(*scs:(2)(ab.))`,
			input: "yabc###", wantWhole: "yabc###",
			wantGroups: []*string{g("yabc###"), g("y"), g("abc"), g("###"), g("abc")},
		},
		{
			name: "scs-zababc", pattern: `([a-z])([a-z]++)(#+)(*scs:(2)(ab.))`,
			input: "zababc####", wantWhole: "zababc####",
			wantGroups: []*string{g("zababc####"), g("z"), g("ababc"), g("####"), g("aba")},
		},
		{
			name: "scan-named-match", pattern: `(?<XX>[a-z]++)##(*scan_substring:('XX').*(..)$)\2`,
			input: "##abcd##abcd##cd##", wantWhole: "abcd##cd",
			wantGroups: []*string{g("abcd##cd"), g("abcd"), g("cd")},
		},
		{
			name: "scan-named-no-match", pattern: `(?<XX>[a-z]++)##(*scan_substring:('XX').*(..)$)\2`,
			input: "##abcd##abcd##abcd##", wantNoMatch: true,
		},
	})
}

func TestRegression1045UCD16(t *testing.T) {
	// 10.45 #503: Unicode updated to UCD 16. Newer binary properties like
	// "Emoji" and "Extended_Pictographic" must compile and match.
	runGoldenCases(t, []gcase{
		{name: "emoji", pattern: `\p{Emoji}`, input: "😀", utf: true, wantWhole: "😀"},
		{name: "extpict", pattern: `\p{Extended_Pictographic}`, input: "😀", utf: true, wantWhole: "😀"},
	})
}

// --- 10.46 -----------------------------------------------------------------

func TestRegression1046AcceptInScanSubstring(t *testing.T) {
	// CVE-2025-58050: (*ACCEPT) inside (*scs:) used to read past the end of the
	// subject. The pattern must match correctly and, crucially, must not crash or
	// return garbage. Golden from testoutput2 lines 20535-20545 and 6905-6910.
	runGoldenCases(t, []gcase{
		{
			name: "accept-scs-bbb", pattern: `(a)(*scs:(1)a(*ACCEPT))bbb`,
			input: "abbb", wantWhole: "abbb",
			wantGroups: []*string{g("abbb"), g("a")},
		},
		{
			name: "accept-scs-backref", pattern: `(a)(b+)(*scs:(1)a(*ACCEPT))(\2)`,
			input: "abbb", wantWhole: "abb",
			wantGroups: []*string{g("abb"), g("a"), g("b"), g("b")},
		},
		{
			name: "accept-scs-lookbehind-ab", pattern: `()()()(?<=ab(*ACCEPT)(*scs:(1,2,3))cd|efg)xyz`,
			input: "abxyz", wantWhole: "xyz",
			wantGroups: []*string{g("xyz"), g(""), g(""), g("")},
		},
		{
			name: "accept-scs-lookbehind-efg", pattern: `()()()(?<=ab(*ACCEPT)(*scs:(1,2,3))cd|efg)xyz`,
			input: "efgxyz", wantWhole: "xyz",
			wantGroups: []*string{g("xyz"), g(""), g(""), g("")},
		},
	})
}

// --- 10.47 -----------------------------------------------------------------

func TestRegression1047SubroutineCaptures(t *testing.T) {
	// 10.47 #9: pattern recursion of the form (?N(group,...)) acts as a subroutine
	// call that additionally returns the listed capturing groups to the caller.
	// Golden from testoutput2 lines 23081-23090.
	runGoldenCases(t, []gcase{
		{
			name:      "subroutine-caps-match",
			pattern:   `(?1(2))#(?1(3))#(?1(4))#(?(DEFINE)((.)\2(.)\3(.)\4))`,
			input:     "aabbcc#ddeeff#gghhii!aabbcc#ddeeff#gghhii#",
			wantWhole: "aabbcc#ddeeff#gghhii#",
			// g1 is unset (the DEFINE group never captured); g2/3/4 hold the last
			// capture returned by each subroutine call.
			wantGroups: []*string{g("aabbcc#ddeeff#gghhii#"), gunset(), g("a"), g("e"), g("i")},
		},
		{
			name:    "subroutine-caps-nomatch",
			pattern: `(?1(2))#(?1(3))#(?1(4))#(?(DEFINE)((.)\2(.)\3(.)\4))`,
			input:   "aabbcc#ddeeff#gghhii", wantNoMatch: true,
		},
	})
}

func TestRegression1047NextMatchIteration(t *testing.T) {
	// 10.47 #733: pcre2_next_match() drives the batched FindAll / Iter path. The
	// tricky cases are empty matches and patterns that can match both empty and
	// non-empty strings at adjacent positions. The iteration must terminate and
	// must not double-count or skip matches.
	re := lib.MustCompile(`a*`, lib.CompileOptions{UTF: true, UCP: true})
	defer re.Close()

	all, err := re.FindAll([]byte("aXaXa"), -1)
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	// PCRE2 global-match semantics over "aXaXa": empty matches between the a's
	// and at the boundaries are returned exactly once each.
	got := make([]string, len(all))
	for i, m := range all {
		got[i] = string(m.Group(0))
	}
	want := []string{"a", "", "a", "", "a", ""}
	if len(got) != len(want) {
		t.Fatalf("match count: got %d (%v) want %d (%v)", len(got), got, len(want), want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("match #%d: got %q want %q", i, got[i], want[i])
		}
	}
}

// TestRegression1047NextMatchKeptStart exercises the rare \K-in-lookaround case
// that pcre2_next_match() has dedicated handling for: when \K pushes the match
// start backwards so that ovector[0] != start_offset, the iterator must still
// make forward progress and not loop forever or skip matches.
func TestRegression1047NextMatchKeptStart(t *testing.T) {
	t.Run("K-resets-start", func(t *testing.T) {
		// a\Kb consumes "ab" but reports the match as just "b" (start = after a).
		// Over "ababab" the global scan must find exactly the three b's.
		re := lib.MustCompile(`a\Kb`, lib.CompileOptions{UTF: true, UCP: true})
		defer re.Close()
		all, err := re.FindAll([]byte("ababab"), -1)
		if err != nil {
			t.Fatalf("FindAll: %v", err)
		}
		if len(all) != 3 {
			t.Fatalf("expected 3 matches, got %d", len(all))
		}
		for i, m := range all {
			if s := string(m.Group(0)); s != "b" {
				t.Errorf("match #%d: got %q want %q", i, s, "b")
			}
		}
	})

	t.Run("K-empty-with-capture", func(t *testing.T) {
		// (?=(\d))\K is a zero-width match (empty whole match) that captures the
		// upcoming digit. pcre2_next_match advances by one code unit after each
		// empty match, yielding one match per digit.
		re := lib.MustCompile(`(?=(\d))\K`, lib.CompileOptions{UTF: true, UCP: true})
		defer re.Close()
		all, err := re.FindAll([]byte("a1b2"), -1)
		if err != nil {
			t.Fatalf("FindAll: %v", err)
		}
		if len(all) != 2 {
			t.Fatalf("expected 2 matches, got %d", len(all))
		}
		wantDigits := []string{"1", "2"}
		for i, m := range all {
			if g1 := m.Group(1); g1 == nil || string(g1) != wantDigits[i] {
				t.Errorf("match #%d group 1: got %v want %q", i, g1, wantDigits[i])
			}
		}
	})
}

func TestRegression1047ImprovedErrorOffsets(t *testing.T) {
	// 10.47 #756: improved error offsets and diagnostics. The offset now points
	// at the offending token, not always 0.
	cases := []struct {
		pattern    string
		wantSubstr string // a substring expected in the error message
	}{
		{`\xthing`, "after \\x"},         // 10.45 rule, but offset now precise
		{`\x{ZZ}`, "non-hex"},            // offset points at the bad char
		{`(?P<name`, "terminator"},       // unclosed name
		{`(foo)(*scs:1)`, "parenthesis"}, // malformed scan-substring
	}
	for _, tc := range cases {
		_, err := lib.Compile(tc.pattern, lib.CompileOptions{})
		if err == nil {
			t.Errorf("expected compile error for %q", tc.pattern)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantSubstr) {
			t.Errorf("error for %q: %q does not contain %q", tc.pattern, err.Error(), tc.wantSubstr)
		}
	}
}

// TestRegression1047CompatLayerFeatures exercises the same new behaviour through
// the regexp2 drop-in layer, which does not rewrite any of these constructs.
func TestRegression1047CompatLayerFeatures(t *testing.T) {
	t.Run("grapheme-ZWJ", func(t *testing.T) {
		// Option 0 still enables UTF+UCP for \X (compat layer default-on of UCP
		// for non-ECMAScript). ECMAScript would reject \X.
		re := p2.MustCompile(`^\X$`, 0)
		m, err := re.FindStringMatch("😀\u200d😀")
		if err != nil || m == nil {
			t.Fatalf("expected match, err=%v m=%v", err, m)
		}
		if m.String() != "😀\u200d😀" {
			t.Errorf("got %q", m.String())
		}
	})

	t.Run("subroutine-captures", func(t *testing.T) {
		re := p2.MustCompile(`(?1(2))#(?1(3))#(?1(4))#(?(DEFINE)((.)\2(.)\3(.)\4))`, 0)
		m, err := re.FindStringMatch("aabbcc#ddeeff#gghhii!aabbcc#ddeeff#gghhii#")
		if err != nil || m == nil {
			t.Fatalf("expected match, err=%v m=%v", err, m)
		}
		if m.String() != "aabbcc#ddeeff#gghhii#" {
			t.Errorf("whole: got %q", m.String())
		}
		// g2/3/4 hold the returned captures.
		for n, want := range map[int]string{2: "a", 3: "e", 4: "i"} {
			g := m.GroupByNumber(n)
			if g == nil || g.String() != want {
				t.Errorf("group %d: got %v want %q", n, g, want)
			}
		}
	})

	t.Run("scan-substring", func(t *testing.T) {
		re := p2.MustCompile(`([a-z])([a-z]++)(#+)(*scs:(2)(ab.))`, 0)
		m, err := re.FindStringMatch("yabc###")
		if err != nil || m == nil {
			t.Fatalf("expected match, err=%v m=%v", err, m)
		}
		if m.String() != "yabc###" {
			t.Errorf("whole: got %q", m.String())
		}
		if g := m.GroupByNumber(4); g == nil || g.String() != "abc" {
			t.Errorf("group 4: got %v want %q", g, "abc")
		}
	})
}
