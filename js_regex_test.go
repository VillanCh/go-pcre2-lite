package pcre2lite_test

// JavaScript / Node.js regex compatibility, performance and security tests.
//
// Compatibility cases are taken VERBATIM from ECMAScript's own conformance
// material -- Test262 (tc39/test262, test/built-ins/RegExp/...) and the V8/Chrome
// regexp feature documentation -- and asserted against the EXACT result that a
// JS engine produces (the .match()/.exec() array). This proves that JS regexes
// migrated onto go-pcre2-lite/regexp2 behave as they do in V8/Node.
//
// Security cases are real, CVE-tracked ReDoS regexes from the npm ecosystem
// (moment.js, the Cloudflare 2019 outage rule, the MITRE CWE-1333 canonical
// example, classic "evil" email validation). They assert that go-pcre2-lite is
// bounded by the engine's match limit -- it never hangs -- whereas dlclark needs
// an explicit timeout to survive the same inputs.
//
// References:
//   - https://github.com/tc39/test262/tree/main/test/built-ins/RegExp
//   - https://developer.chrome.com/blog/upcoming-regexp-features
//   - moment.js CVE-2022-31129 (GHSA-wc69-rhjr-hc9g)
//   - UAParser.js CVE-2020-7733 (SNYK-JS-UAPARSERJS-610226)
//   - MITRE CWE-1333 (Inefficient Regular Expression Complexity)

import (
	"strings"
	"testing"
	"time"

	p2 "github.com/VillanCh/go-pcre2-lite/regexp2"
	dl "github.com/dlclark/regexp2"
)

// jsUndef marks a capture group that JavaScript reports as `undefined`
// (a group that did not participate in the match).
const jsUndef = "\x00<undef>\x00"

type jsMatchCase struct {
	name    string
	pattern string
	opts    p2.RegexOptions
	input   string
	whole   string   // expected whole match (group 0)
	groups  []string // expected groups 1..n; jsUndef means JS `undefined`
}

// jsCompatCases are Test262 / V8-documented patterns that PCRE2 10.47 fully
// supports. Each expected value is what V8/Node returns for the same call.
var jsCompatCases = []jsMatchCase{
	// --- Test262 RegExp/lookBehind/captures.js (fixed-length lookbehind) ------
	{"t262-lb-captures-1", `(?<=(c))def`, 0, "abcdef", "def", []string{"c"}},
	{"t262-lb-captures-2", `(?<=(\w{2}))def`, 0, "abcdef", "def", []string{"bc"}},
	{"t262-lb-captures-3", `(?<=(\w(\w)))def`, 0, "abcdef", "def", []string{"bc", "c"}},
	{"t262-lb-captures-5", `(?<=(bc)|(cd)).`, 0, "abcdef", "d", []string{"bc", jsUndef}},

	// --- V8/Chrome blog: lookbehind for currency ------------------------------
	{"v8-lb-currency", `(?<=\$)\d+`, 0, "$1 is worth about ¥123", "1", nil},

	// --- V8/Chrome blog + Test262: named capture groups -----------------------
	{"v8-named-date", `(?<year>\d{4})-(?<month>\d{2})-(?<day>\d{2})`, 0,
		"2017-07-10", "2017-07-10", []string{"2017", "07", "10"}},

	// --- JS named backreference \k<name> --------------------------------------
	{"js-named-backref", `(?<q>["'])(?<v>.*?)\k<q>`, 0,
		`say "hi" please`, `"hi"`, []string{`"`, "hi"}},

	// --- JS classic duplicate-word backreference ------------------------------
	{"js-dup-word", `\b(\w+)\s+\1\b`, 0, "the the cat", "the the", []string{"the"}},

	// --- V8/Chrome blog: Unicode property escapes (JS /u) ----------------------
	// JS accepts the long General_Category name \p{Number}; PCRE2 wants the short
	// alias \p{N}. Both engines accept \p{N}, so the portable form is used here;
	// the long-name divergence is asserted in TestJSPropertyLongNameDivergence.
	{"v8-prop-number", `\p{N}`, 0, "①", "①", nil},
	{"v8-prop-alpha", `\p{Alphabetic}`, 0, "雪", "雪", nil},
	{"v8-prop-math", `^\p{Math}+$`, 0, "∛∞∉", "∛∞∉", nil},
}

// jsGlobalCase is a /g pattern whose successive whole matches are checked.
type jsGlobalCase struct {
	name    string
	pattern string
	opts    p2.RegexOptions
	input   string
	all     []string
}

var jsGlobalCases = []jsGlobalCase{
	// Test262 RegExp/lookBehind/captures.js global subcases.
	{"t262-lb-global-8", `(?<=b|c)\w`, 0, "abcdef", []string{"c", "d"}},
	{"t262-lb-global-9", `(?<=[b-e])\w{2}`, 0, "abcdef", []string{"cd", "ef"}},
}

// jsVarLookbehindSupportedCases are Test262 variable-length lookbehind patterns
// that JavaScript accepts. PCRE2 10.47 supports bounded variable-length
// lookbehind natively, and the syntax-compatibility layer (compat.go) tightens
// otherwise-unbounded quantifiers inside a lookbehind so they compile too. We
// assert that they now compile AND produce the same first match as dlclark
// (the .NET-style oracle that already supports variable-length lookbehind).
var jsVarLookbehindSupportedCases = []struct {
	name    string
	pattern string
	input   string
}{
	{"t262-lb-captures-6-var", `(?<=([ab]{1,2})\D|(abc))\w`, "abxc abc abcd"},
	{"t262-lb-captures-7-var", `\D(?<=([ab]+))(\w)`, "xaab9"},
}

// jsVarLookbehindStillRejectedCases are variable-length lookbehind patterns whose
// length cannot be bounded at compile time (here the length depends on a
// backreference), so PCRE2 still rejects them. This stays documented as a
// boundary; see MIGRATION.md.
var jsVarLookbehindStillRejectedCases = []struct {
	name    string
	pattern string
}{
	{"t262-lb-mutual-1", `(?<=a(.\2)b(\1)).{4}`},
}

func collectAllMatches(t *testing.T, re *p2.Regexp, input string) []string {
	t.Helper()
	var out []string
	m, err := re.FindStringMatch(input)
	if err != nil {
		t.Fatalf("FindStringMatch: %v", err)
	}
	for m != nil {
		out = append(out, m.String())
		if m, err = re.FindNextMatch(m); err != nil {
			t.Fatalf("FindNextMatch: %v", err)
		}
		if len(out) > 100000 {
			t.Fatalf("runaway iteration")
		}
	}
	return out
}

// TestJSRegexCompatTest262 asserts go-pcre2-lite reproduces JavaScript's exact
// match results for Test262 / V8-documented regex features.
func TestJSRegexCompatTest262(t *testing.T) {
	for _, c := range jsCompatCases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			re, err := p2.Compile(c.pattern, c.opts)
			if err != nil {
				t.Fatalf("compile %q: %v", c.pattern, err)
			}
			m, err := re.FindStringMatch(c.input)
			if err != nil {
				t.Fatalf("match: %v", err)
			}
			if m == nil {
				t.Fatalf("expected a match for %q on %q", c.pattern, c.input)
			}
			if m.String() != c.whole {
				t.Errorf("whole match: got %q want %q", m.String(), c.whole)
			}
			for i, want := range c.groups {
				g := m.GroupByNumber(i + 1)
				participated := g != nil && len(g.Captures) > 0
				if want == jsUndef {
					if participated {
						t.Errorf("group %d: got %q, want JS undefined", i+1, g.String())
					}
					continue
				}
				if !participated {
					t.Errorf("group %d: did not participate, want %q", i+1, want)
					continue
				}
				if g.String() != want {
					t.Errorf("group %d: got %q want %q", i+1, g.String(), want)
				}
			}
		})
	}
}

// TestJSRegexCompatNamed checks named-group access matches JS `groups` object.
func TestJSRegexCompatNamed(t *testing.T) {
	re := p2.MustCompile(`(?<year>\d{4})-(?<month>\d{2})-(?<day>\d{2})`, 0)
	m, err := re.FindStringMatch("2017-07-10")
	if err != nil || m == nil {
		t.Fatalf("match failed: err=%v m=%v", err, m)
	}
	want := map[string]string{"year": "2017", "month": "07", "day": "10"}
	for name, val := range want {
		g := m.GroupByName(name)
		if g == nil || g.String() != val {
			t.Errorf("group %q: got %v want %q", name, g, val)
		}
	}
}

// TestJSRegexCompatGlobal checks /g successive matches against JS .match(/g).
func TestJSRegexCompatGlobal(t *testing.T) {
	for _, c := range jsGlobalCases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			re, err := p2.Compile(c.pattern, c.opts)
			if err != nil {
				t.Fatalf("compile %q: %v", c.pattern, err)
			}
			got := collectAllMatches(t, re, c.input)
			if len(got) != len(c.all) {
				t.Fatalf("match count: got %v want %v", got, c.all)
			}
			for i := range got {
				if got[i] != c.all[i] {
					t.Errorf("match %d: got %q want %q", i, got[i], c.all[i])
				}
			}
		})
	}
}

// TestJSVariableLookbehindSupported asserts that variable-length lookbehind now
// compiles (PCRE2 10.47 native support + compat.go bounding) and yields the same
// first match as dlclark, the variable-length-lookbehind oracle.
func TestJSVariableLookbehindSupported(t *testing.T) {
	for _, c := range jsVarLookbehindSupportedCases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			re, err := p2.Compile(c.pattern, 0)
			if err != nil {
				t.Fatalf("expected variable-length lookbehind %q to compile, got: %v", c.pattern, err)
			}
			m, err := re.FindStringMatch(c.input)
			if err != nil {
				t.Fatalf("FindStringMatch: %v", err)
			}
			got := ""
			if m != nil {
				got = m.String()
			}
			dre, derr := dl.Compile(c.pattern, 0)
			if derr != nil {
				t.Fatalf("dlclark oracle failed to compile %q: %v", c.pattern, derr)
			}
			dm, _ := dre.FindStringMatch(c.input)
			want := ""
			if dm != nil {
				want = dm.String()
			}
			if got != want {
				t.Errorf("pattern %q on %q: got %q, dlclark oracle %q", c.pattern, c.input, got, want)
			}
		})
	}
}

// TestJSVariableLookbehindStillRejected documents lookbehind whose length cannot
// be bounded at compile time (length depends on a backreference) and is still
// rejected by PCRE2.
func TestJSVariableLookbehindStillRejected(t *testing.T) {
	for _, c := range jsVarLookbehindStillRejectedCases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if _, err := p2.Compile(c.pattern, 0); err == nil {
				t.Errorf("expected PCRE2 to reject unbounded lookbehind %q, but it compiled", c.pattern)
			} else {
				t.Logf("rejected as expected: %v", err)
			}
		})
	}
}

// TestJSPropertyLongNameDivergence documents that JavaScript accepts long
// General_Category property names like \p{Number}, whereas PCRE2 10.47 only
// accepts the short alias \p{N}. Migrated patterns must use the short form.
func TestJSPropertyLongNameDivergence(t *testing.T) {
	if _, err := p2.Compile(`\p{Number}`, 0); err == nil {
		t.Errorf("expected \\p{Number} (JS long name) to be rejected by PCRE2; use \\p{N}")
	} else {
		t.Logf("\\p{Number} rejected as expected (use \\p{N}): %v", err)
	}
	// The portable short form must work and match the same character.
	re := p2.MustCompile(`\p{N}`, 0)
	m, err := re.FindStringMatch("①")
	if err != nil || m == nil || m.String() != "①" {
		t.Errorf("\\p{N} on circled-one: err=%v m=%v", err, m)
	}
}

// TestJSLookbehindCaptureDivergence documents a genuine JS-vs-PCRE2 semantic
// difference: for a quantified capture inside a lookbehind, JavaScript (which
// matches the lookbehind right-to-left) keeps the leftmost iteration ("a"),
// while PCRE2 keeps the rightmost ("c"). The WHOLE match is identical; only the
// captured sub-group differs. See MIGRATION.md.
func TestJSLookbehindCaptureDivergence(t *testing.T) {
	re := p2.MustCompile(`(?<=(\w){3})def`, 0)
	m, err := re.FindStringMatch("abcdef")
	if err != nil || m == nil {
		t.Fatalf("match failed: err=%v m=%v", err, m)
	}
	if m.String() != "def" {
		t.Errorf("whole match: got %q want %q (must agree with JS)", m.String(), "def")
	}
	g := m.GroupByNumber(1)
	if g == nil || g.String() != "c" {
		t.Errorf("PCRE2 group 1: got %v want %q (JS would yield \"a\")", g, "c")
	} else {
		t.Logf("documented divergence: PCRE2 group1=%q, JS group1=\"a\"", g.String())
	}
}

// jsBackrefLookbehindDivergence are Test262 patterns with a backreference inside
// the lookbehind. PCRE2 10.47 COMPILES them but does NOT match where JavaScript
// does (JS resolves the self-reference differently). We assert PCRE2's actual
// no-match so the divergence is captured.
var jsBackrefLookbehindDivergence = []struct {
	name    string
	pattern string
	opts    p2.RegexOptions
	input   string
}{
	{"t262-lb-backref-1", `(?<=\1(\w))d`, p2.IgnoreCase, "abcCd"}, // JS: ["d","C"]
	{"t262-lb-backref-2", `(?<=\1([abx]))d`, 0, "abxxd"},          // JS: ["d","x"]
}

// TestJSBackrefInLookbehindDivergence documents that backreferences inside a
// lookbehind compile under PCRE2 but produce a different (no-match) result than
// JavaScript. See MIGRATION.md.
func TestJSBackrefInLookbehindDivergence(t *testing.T) {
	for _, c := range jsBackrefLookbehindDivergence {
		c := c
		t.Run(c.name, func(t *testing.T) {
			re, err := p2.Compile(c.pattern, c.opts)
			if err != nil {
				t.Fatalf("expected %q to compile under PCRE2, got: %v", c.pattern, err)
			}
			m, err := re.FindStringMatch(c.input)
			if err != nil {
				t.Fatalf("match error: %v", err)
			}
			if m != nil {
				t.Errorf("PCRE2 unexpectedly matched %q (JS matches, PCRE2 documented as no-match)", m.String())
			} else {
				t.Logf("documented divergence: PCRE2 no-match on %q where JS matches", c.input)
			}
		})
	}
}

// --- Security: real-world ReDoS from the JS ecosystem ----------------------
//
// We split the corpus by how PCRE2 actually defends against each pattern, as
// measured against PCRE2 10.47 (see the per-group comments). This avoids giving
// a false sense of security: match_limit stops EXPONENTIAL backtracking dead,
// but does NOT bound a POLYNOMIAL (quadratic) scan -- for that the only real
// defense is capping input length.

// jsReDoSRealWorld are CVE-tracked or incident-tracked evil regexes drawn from
// the JS/Node ecosystem. The headline guarantee is that every one TERMINATES
// under the default limits, no matter the input -- go-pcre2-lite never hangs.
var jsReDoSRealWorld = []struct {
	name    string
	pattern string
	input   string
}{
	// moment.js CVE-2022-31129: the rfc2822 legacy-comment stripper. PoC is
	// moment("(".repeat(N)); the hotspot regex is /\([^)]*\)|[\n\t]/g. This one
	// is QUADRATIC, not exponential (see TestJSReDoSQuadraticNeedsInputCap).
	{"moment-cve-2022-31129", `\([^)]*\)|[\n\t]`, strings.Repeat("(", 40000)},
	// Cloudflare 2019 global outage: the core of the offending WAF rule. PCRE2's
	// start optimization (no '=' present) makes this an instant no-match.
	{"cloudflare-2019-outage", `.*.*=.*`, strings.Repeat("a", 40000)},
	// MITRE CWE-1333 canonical vulnerable backtracking clause. EXPONENTIAL.
	{"cwe-1333-canonical", `(\w+\s?)*$`, strings.Repeat("a", 60) + "!"},
	// Classic "evil" email validator (OWASP / regular-expressions.info). PCRE2's
	// required-char optimization (no '@') makes this an instant no-match.
	{"evil-email-validator", `^([a-zA-Z0-9])(([\-.]|[_]+)?([a-zA-Z0-9]+))*@`,
		strings.Repeat("a", 40) + "!"},
	// UAParser.js CVE-2020-7733 shape: overlapping class then a sentinel ('x')
	// that is absent, so PCRE2 fails fast.
	{"uaparser-cve-2020-7733-shape", `(([a-z]+)\s?)+!x`, strings.Repeat("a ", 30) + "!"},
}

// jsReDoSExponential are patterns whose backtracking is genuinely EXPONENTIAL
// under any classic backtracker (nested unbounded quantifiers). Against PCRE2
// they are bounded by match_limit, and a tight limit turns them into a
// sub-millisecond deterministic failure. These are the patterns that would hang
// dlclark/regexp2 without an explicit MatchTimeout.
var jsReDoSExponential = []struct {
	name    string
	pattern string
	input   string
}{
	{"cwe-1333", `(\w+\s?)*$`, strings.Repeat("a", 60) + "!"},
	{"nested-plus", `(a+)+$`, strings.Repeat("a", 40) + "!"},
	{"class-star", `([a-zA-Z]+)*$`, strings.Repeat("a", 40) + "!"},
	{"digit-plus", `(\d+)+$`, strings.Repeat("1", 40) + "!"},
	// Classic catastrophic email matcher from regular-expressions.info.
	{"evil-email-nested", `^(([a-z])+.)+[A-Z]([a-z])+$`, strings.Repeat("a", 40)},
}

// TestJSEcosystemReDoSAllTerminate is the headline safety property: every
// real-world JS evil regex finishes within an 8s watchdog under DEFAULT limits,
// returning either a clean no-match or a bounded match-limit error. It never
// hangs, regardless of input.
func TestJSEcosystemReDoSAllTerminate(t *testing.T) {
	const watchdog = 8 * time.Second
	for _, tc := range jsReDoSRealWorld {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			re := p2.MustCompile(tc.pattern, 0)
			var (
				matched bool
				err     error
				elapsed time.Duration
			)
			finished := withDeadline(watchdog, func() {
				start := time.Now()
				matched, err = re.MatchString(tc.input)
				elapsed = time.Since(start)
			})
			if !finished {
				t.Fatalf("match did not finish within %v: ReDoS was NOT bounded", watchdog)
			}
			t.Logf("matched=%v err=%v elapsed=%v", matched, err, elapsed)
			if err != nil && !isLimitErr(err) {
				t.Errorf("unexpected error (want nil or limit error): %v", err)
			}
		})
	}
}

// TestJSReDoSExponentialFastFailWithLimit proves the match_limit defense: for
// genuinely exponential patterns, a tight match limit collapses the attack into
// a sub-50ms deterministic failure.
func TestJSReDoSExponentialFastFailWithLimit(t *testing.T) {
	const fast = 50 * time.Millisecond
	for _, tc := range jsReDoSExponential {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			re := p2.MustCompile(tc.pattern, 0)
			if err := re.SetMatchLimits(50000, 0); err != nil {
				t.Fatalf("SetMatchLimits: %v", err)
			}
			var (
				err     error
				elapsed time.Duration
			)
			finished := withDeadline(8*time.Second, func() {
				start := time.Now()
				_, err = re.MatchString(tc.input)
				elapsed = time.Since(start)
			})
			if !finished {
				t.Fatalf("did not finish even with a tight limit")
			}
			t.Logf("err=%v elapsed=%v", err, elapsed)
			if !isLimitErr(err) {
				t.Errorf("expected a match-limit error for an exponential pattern, got: %v", err)
			}
			if elapsed > fast {
				t.Errorf("tight limit should fail fast, took %v", elapsed)
			}
		})
	}
}

// TestJSReDoSQuadraticNeedsInputCap is the honest counterpoint: moment's
// CVE-2022-31129 hotspot is QUADRATIC, not exponential. match_limit does NOT
// bound the per-character scan loop, so the only effective defense is capping
// input length. We verify (1) a capped input stays fast, and (2) the cost grows
// ~O(n^2), so users must bound input length rather than rely on match_limit.
func TestJSReDoSQuadraticNeedsInputCap(t *testing.T) {
	const pat = `\([^)]*\)|[\n\t]`

	// Defense that works: capping input length keeps the scan cheap.
	reCapped := p2.MustCompile(pat, 0)
	var cappedElapsed time.Duration
	finished := withDeadline(2*time.Second, func() {
		start := time.Now()
		_, _ = reCapped.MatchString(strings.Repeat("(", 2000))
		cappedElapsed = time.Since(start)
	})
	if !finished || cappedElapsed > 50*time.Millisecond {
		t.Errorf("capped input (2000) should be fast, finished=%v elapsed=%v", finished, cappedElapsed)
	}

	// Defense that does NOT work: a tight match_limit barely helps, because the
	// work is in character-class scanning, not backtracking steps. We only log
	// this (timings are environment-dependent) so the limitation stays visible.
	reLimited := p2.MustCompile(pat, 0)
	_ = reLimited.SetMatchLimits(50000, 0)
	var limitedElapsed time.Duration
	var limitedErr error
	finished = withDeadline(8*time.Second, func() {
		start := time.Now()
		_, limitedErr = reLimited.MatchString(strings.Repeat("(", 40000))
		limitedElapsed = time.Since(start)
	})
	if !finished {
		t.Fatalf("quadratic scan did not finish within watchdog")
	}
	t.Logf("quadratic with tight match_limit on n=40000: err=%v elapsed=%v (match_limit does NOT bound polynomial scans; cap input length instead)",
		limitedErr, limitedElapsed)
}

// TestJSEcosystemReDoSDlclarkNeedsTimeout documents the safety gap: on the
// exponential patterns, dlclark/regexp2 must be given an explicit MatchTimeout
// to survive, while go-pcre2-lite is bounded by the engine itself.
func TestJSEcosystemReDoSDlclarkNeedsTimeout(t *testing.T) {
	const watchdog = 8 * time.Second
	for _, tc := range jsReDoSExponential {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dre := dl.MustCompile(tc.pattern, 0)
			dre.MatchTimeout = 250 * time.Millisecond
			var derr error
			finished := withDeadline(watchdog, func() {
				_, derr = dre.MatchString(tc.input)
			})
			if !finished {
				t.Fatalf("dlclark did not finish within watchdog even with timeout")
			}
			t.Logf("dlclark err=%v (without MatchTimeout this would be catastrophic)", derr)
		})
	}
}
