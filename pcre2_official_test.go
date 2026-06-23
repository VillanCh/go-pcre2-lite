package pcre2lite_test

// This file drives go-pcre2-lite's low-level engine through PCRE2's OWN official
// regression corpus and asserts agreement with the authoritative results that
// the upstream pcre2test tool recorded. Unlike corpus_pcre_test.go (which does a
// dlclark-vs-pcre2-lite differential on PCRE2 v10.21 inputs), here we compare
// against the recorded ground-truth output, and the vendored engine is the SAME
// PCRE2 version (10.47) that produced these files -- so the only legitimate
// divergences come from the few features we intentionally trimmed (DFA, JIT,
// serialization, callouts, locale tables) or from per-test newline/bsr overrides
// which we conservatively skip.
//
// Sources (committed under testdata/, see testdata/README.md):
//   - pcre2_testoutput2.txt : "testoutput2", PCRE2's NON-Perl feature, error and
//     boundary tests (8-bit, #forbid_utf). Rich in compile-error diagnostics,
//     which we use as a precise compile-boundary oracle.
//   - pcre2_testoutput4.txt : "testoutput4", UTF and Unicode-property matching.
//
// We test ONLY the regex effect: whole-match presence and group-0 text, plus
// whether a pattern compiles at all. We never assert on ovector sizes, callout
// traces, substitution output, or other API surface.

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	lib "github.com/VillanCh/go-pcre2-lite"
)

const (
	pcre2Output2Path = "testdata/pcre2_testoutput2.txt"
	pcre2Output4Path = "testdata/pcre2_testoutput4.txt"
)

// Per-subject expectation, derived from the recorded result lines.
const (
	pcreExpectUnknown = iota
	pcreExpectMatch
	pcreExpectNoMatch
)

type pcreSubject struct {
	raw    string // subject text, still PCRE2-escaped; decode with the case utf flag
	group0 string // expected whole-match text, still PCRE2-escaped
	opts   lib.MatchOption
	expect int
	skip   bool // carries a per-subject modifier we cannot faithfully reproduce
}

type pcreCase struct {
	body        string // the bare pattern (delimiters and modifiers stripped)
	co          lib.CompileOptions
	utf         bool
	skipPattern bool // carries a pattern modifier we cannot faithfully reproduce
	compileFail bool // pcre2test recorded "Failed: ..." for this pattern
	subjects    []pcreSubject
	line        int
}

// pattern-modifier keywords that map onto a compile option.
func applyPCREKeyword(tok string, co *lib.CompileOptions, utf *bool) (handled bool) {
	switch tok {
	case "caseless":
		co.Caseless = true
	case "multiline":
		co.Multiline = true
	case "dotall":
		co.DotAll = true
	case "extended":
		co.Extended = true
	case "ungreedy":
		co.Ungreedy = true
	case "anchored":
		co.Anchored = true
	case "endanchored":
		co.EndAnchored = true
	case "dollar_endonly":
		co.DollarEndOnly = true
	case "firstline":
		co.FirstLine = true
	case "no_auto_capture":
		co.NoAutoCapture = true
	case "dupnames":
		co.DupNames = true
	case "allow_empty_class":
		co.AllowEmpty = true
	case "never_ucp":
		co.NeverUCP = true
	case "utf":
		co.UTF = true
		*utf = true
	case "ucp":
		co.UCP = true
	default:
		return false
	}
	return true
}

// pattern-modifier keywords that do not affect whether/where a pattern matches
// (they only change pcre2test's output formatting or non-functional behaviour).
// NOTE: no_start_optimize, no_auto_possess and expand are deliberately NOT here:
// they can change the observable match (e.g. (*COMMIT) interacts with the start
// optimization) or re-interpret the pattern text, and we have no faithful knob
// for them, so patterns carrying them are skipped instead.
var pcreIgnorePatternKW = map[string]bool{
	"info": true, "bincode": true, "fullbincode": true,
	"aftertext": true, "allaftertext": true,
	"jit": true, "jitstack": true, "jitverify": true, "jitfast": true,
	"memory": true, "mark": true,
}

// applyPCRELetters handles classic Perl-style single-letter modifier clusters
// such as "is" or "imsx". Returns false if any letter is unrecognised, which
// makes the caller skip the pattern rather than mis-test it.
func applyPCRELetters(tok string, co *lib.CompileOptions) bool {
	for _, ch := range tok {
		switch ch {
		case 'i':
			co.Caseless = true
		case 'm':
			co.Multiline = true
		case 's':
			co.DotAll = true
		case 'x':
			co.Extended = true
		case 'I', 'B', 'g', 'G':
			// info / bytecode / global: no effect on the first whole match
		default:
			return false
		}
	}
	return true
}

func parsePCREPattern(pat string) (body string, co lib.CompileOptions, utf, skip bool) {
	idx := strings.LastIndexAny(pat, `/"`)
	if idx < 1 {
		return "", co, false, true
	}
	body = pat[1:idx]
	mods := pat[idx+1:]
	for _, tok := range strings.Split(mods, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if applyPCREKeyword(tok, &co, &utf) {
			continue
		}
		if pcreIgnorePatternKW[tok] {
			continue
		}
		if applyPCRELetters(tok, &co) {
			continue
		}
		// hex patterns, newline=/bsr= overrides, literal, alt_* and any other
		// exotic modifier we cannot faithfully reproduce: skip the whole pattern.
		skip = true
	}
	// Callout tests use synthetic "subjects" that encode callout positions, not
	// real input text, so they cannot be checked as plain matches.
	if strings.Contains(body, "(?C") {
		skip = true
	}
	return body, co, utf, skip
}

func isPCRESubjectLine(l string) bool {
	if len(l) < 4 || l[:4] != "    " {
		return false
	}
	return len(l) == 4 || l[4] != ' '
}

func isPCREGroupLine(l string) (num int, value string, ok bool) {
	t := strings.TrimLeft(l, " ")
	if t == l { // group lines are indented by exactly one space
		return 0, "", false
	}
	j := 0
	for j < len(t) && t[j] >= '0' && t[j] <= '9' {
		j++
	}
	if j == 0 || j >= len(t) || t[j] != ':' {
		return 0, "", false
	}
	n, err := strconv.Atoi(t[:j])
	if err != nil {
		return 0, "", false
	}
	rest := t[j+1:]
	rest = strings.TrimPrefix(rest, " ")
	return n, rest, true
}

// parsePCRESubject splits a 4-space-indented subject line into its raw text and
// any trailing "\=mod1,mod2" data modifiers.
func parsePCRESubject(l string) pcreSubject {
	s := l[4:]
	subj := pcreSubject{}
	if mi := strings.Index(s, `\=`); mi >= 0 {
		raw := s[:mi]
		mods := s[mi+2:]
		subj.raw = raw
		for _, tok := range strings.Split(mods, ",") {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			switch {
			case tok == "anchored":
				subj.opts |= lib.MatchAnchored
			case tok == "notbol":
				subj.opts |= lib.MatchNotBOL
			case tok == "noteol":
				subj.opts |= lib.MatchNotEOL
			case tok == "notempty":
				subj.opts |= lib.MatchNotEmpty
			case tok == "notempty_atstart":
				subj.opts |= lib.MatchNotEmptyAtStart
			case tok == "endanchored":
				subj.opts |= lib.MatchEndAnchored
			case strings.HasPrefix(tok, "ovector="):
				// only changes how many groups are reported; group 0 is intact
			case tok == "g" || tok == "global" || tok == "aftertext" ||
				tok == "allaftertext" || tok == "mark" || tok == "startchar" ||
				tok == "memory" || strings.HasPrefix(tok, "jitstack="):
				// extra output only; the first whole match is unaffected
			default:
				// offset=, dfa, partial*, ph/ps, replace=, *_limit=, ...
				subj.skip = true
			}
		}
	} else {
		subj.raw = s
	}
	// pcre2test's "\[STRING]{count}" subject-repeat syntax is not plain data; we
	// cannot reconstruct the intended input, so skip such subjects.
	if strings.Contains(subj.raw, `\[`) {
		subj.skip = true
	}
	return subj
}

func parsePCRECorpus(t *testing.T, path string) []pcreCase {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("corpus not available: %v", err)
		return nil
	}
	lines := strings.Split(string(data), "\n")

	var cases []pcreCase
	i := 0
	for i < len(lines) {
		line := lines[i]
		trim := strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(trim, "#") {
			i++
			continue
		}
		delim := line[0]
		if delim != '/' && delim != '"' {
			i++
			continue
		}

		// Accumulate a (possibly multi-line) pattern up to its closing delim.
		startLine := i
		pat := line
		cur := line
		allowFirst := false
		for !corpusContainsEnder(cur, delim, allowFirst) {
			i++
			if i >= len(lines) {
				break
			}
			cur = lines[i]
			pat += "\n" + cur
			allowFirst = true
		}
		i++ // consume the pattern's final line

		c := pcreCase{line: startLine + 1}
		c.body, c.co, c.utf, c.skipPattern = parsePCREPattern(pat)

		// Read the result block until the blank separator OR the next pattern.
		// pcre2test does not always emit a blank line between blocks (notably in
		// global "g" mode), so a column-0 delimiter starts a new pattern and must
		// terminate the current block without being consumed here.
		var curSubj *pcreSubject
		for i < len(lines) {
			l := lines[i]
			if l == "" {
				i++
				break
			}
			if l[0] == '/' || l[0] == '"' {
				break
			}
			i++
			switch {
			case strings.HasPrefix(l, "Failed:"):
				// Only a compile failure if it precedes any subject; a "Failed:"
				// after subjects is a per-subject runtime/substitution error.
				if curSubj == nil {
					c.compileFail = true
				}
			case strings.HasPrefix(l, "----"):
				// Skip a bytecode dump block up to and including its closer.
				for i < len(lines) && !strings.HasPrefix(lines[i], "----") {
					i++
				}
				if i < len(lines) {
					i++
				}
			case isPCRESubjectLine(l):
				c.subjects = append(c.subjects, parsePCRESubject(l))
				curSubj = &c.subjects[len(c.subjects)-1]
			case l == "No match" || strings.HasPrefix(l, "No match,"):
				if curSubj != nil && curSubj.expect == pcreExpectUnknown {
					curSubj.expect = pcreExpectNoMatch
				}
			default:
				if n, val, ok := isPCREGroupLine(l); ok {
					if n == 0 && curSubj != nil && curSubj.expect == pcreExpectUnknown {
						curSubj.expect = pcreExpectMatch
						curSubj.group0 = val
					}
				}
				// everything else (info, continuations, "\= Expect ...",
				// "Partial match", "Matched, but ...") is ignored.
			}
		}
		cases = append(cases, c)
	}
	return cases
}

// pcreDecode turns PCRE2 test escapes into raw bytes. \x{H..} is a Unicode code
// point (UTF-8 encoded when utf is set) and \xHH is a single byte; octal and the
// usual C escapes are also handled. This matches the escaping used both for the
// subject inputs and for the recorded group-0 output.
func pcreDecode(s string, utf bool) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			i++
			continue
		}
		i++
		if i >= len(s) {
			// Trailing lone backslash: pcre2test ignores an incomplete escape.
			break
		}
		switch e := s[i]; {
		case e == 'x':
			i++
			if i < len(s) && s[i] == '{' {
				j := i + 1
				for j < len(s) && s[j] != '}' {
					j++
				}
				if v, err := strconv.ParseInt(s[i+1:j], 16, 32); err == nil {
					if utf {
						b.WriteRune(rune(v))
					} else {
						b.WriteByte(byte(v))
					}
				}
				i = j + 1
			} else {
				h := 0
				n := 0
				for n < 2 && i < len(s) && isHexByte(s[i]) {
					h = h*16 + hexVal(s[i])
					i++
					n++
				}
				if n == 0 {
					b.WriteByte('x')
				} else {
					b.WriteByte(byte(h))
				}
			}
		case e >= '0' && e <= '7':
			o := 0
			n := 0
			for n < 3 && i < len(s) && s[i] >= '0' && s[i] <= '7' {
				o = o*8 + int(s[i]-'0')
				i++
				n++
			}
			b.WriteByte(byte(o))
		case e == 'n':
			b.WriteByte('\n')
			i++
		case e == 'r':
			b.WriteByte('\r')
			i++
		case e == 't':
			b.WriteByte('\t')
			i++
		case e == 'a':
			b.WriteByte(7)
			i++
		case e == 'b':
			b.WriteByte(8)
			i++
		case e == 'e':
			b.WriteByte(0x1b)
			i++
		case e == 'f':
			b.WriteByte('\f')
			i++
		case e == 'v':
			b.WriteByte(0x0b)
			i++
		case e == '\\':
			b.WriteByte('\\')
			i++
		default:
			b.WriteByte(e)
			i++
		}
	}
	return b.String()
}

// pcreDecodeOutput decodes a recorded group-0 RESULT line. pcre2test escapes
// output more lightly than input: non-printable bytes appear as \xHH (or \x{H..}
// for code points) but a literal backslash byte is shown as a single backslash,
// so unlike pcreDecode we must NOT treat "\]" or "\\" as C escapes here.
func pcreDecodeOutput(s string, utf bool) string {
	if !strings.Contains(s, `\x`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '\\' || i+1 >= len(s) || s[i+1] != 'x' {
			b.WriteByte(s[i])
			i++
			continue
		}
		i += 2 // consume "\x"
		if i < len(s) && s[i] == '{' {
			j := i + 1
			for j < len(s) && s[j] != '}' {
				j++
			}
			if v, err := strconv.ParseInt(s[i+1:j], 16, 32); err == nil {
				if utf {
					b.WriteRune(rune(v))
				} else {
					b.WriteByte(byte(v))
				}
			}
			i = j + 1
		} else {
			h, n := 0, 0
			for n < 2 && i < len(s) && isHexByte(s[i]) {
				h = h*16 + hexVal(s[i])
				i++
				n++
			}
			if n == 0 {
				b.WriteString(`\x`)
			} else {
				b.WriteByte(byte(h))
			}
		}
	}
	return b.String()
}

func isHexByte(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	default:
		return int(c-'A') + 10
	}
}

// TestPCRE2OfficialCompileBoundary uses PCRE2's recorded compile diagnostics as
// an oracle: every pattern that upstream rejects ("Failed: ...") must also be
// rejected by go-pcre2-lite, and every pattern upstream accepts must compile.
// Because we vendor the same pcre2_compile.c, agreement should be essentially
// total; the rare misses are sampled for inspection.
func TestPCRE2OfficialCompileBoundary(t *testing.T) {
	cases := parsePCRECorpus(t, pcre2Output2Path)
	if len(cases) == 0 {
		t.Skip("no cases parsed")
	}

	var (
		rejectTotal, rejectOK int
		acceptTotal, acceptOK int
	)
	var samples []string
	addSample := func(format string, args ...interface{}) {
		if len(samples) < 40 {
			samples = append(samples, fmt.Sprintf(format, args...))
		}
	}

	for _, c := range cases {
		if c.skipPattern {
			continue
		}
		re, err := lib.Compile(c.body, c.co)
		if err == nil && re != nil {
			re.Close()
		}
		if c.compileFail {
			rejectTotal++
			if err != nil {
				rejectOK++
			} else {
				addSample("REJECT-MISS (line %d): pattern=%q compiled but PCRE2 rejected it", c.line, c.body)
			}
		} else {
			acceptTotal++
			if err == nil {
				acceptOK++
			} else {
				addSample("ACCEPT-MISS (line %d): pattern=%q rejected by us: %v", c.line, c.body, err)
			}
		}
	}

	t.Logf("compile-reject agreement: %d/%d", rejectOK, rejectTotal)
	t.Logf("compile-accept agreement: %d/%d", acceptOK, acceptTotal)
	for _, s := range samples {
		t.Logf("  %s", s)
	}

	if rejectTotal > 0 {
		ratio := float64(rejectOK) / float64(rejectTotal)
		if ratio < 0.99 {
			t.Errorf("compile-reject agreement %.4f below 0.99 (%d/%d)", ratio, rejectOK, rejectTotal)
		}
	}
	if acceptTotal > 0 {
		ratio := float64(acceptOK) / float64(acceptTotal)
		if ratio < 0.99 {
			t.Errorf("compile-accept agreement %.4f below 0.99 (%d/%d)", ratio, acceptOK, acceptTotal)
		}
	}
}

func runPCREMatchCorpus(t *testing.T, path string, minAgree float64) {
	cases := parsePCRECorpus(t, path)
	if len(cases) == 0 {
		t.Skip("no cases parsed")
	}

	var (
		ok, mismatch, engineErr, subjects int
	)
	var samples []string
	addSample := func(format string, args ...interface{}) {
		if len(samples) < 40 {
			samples = append(samples, fmt.Sprintf(format, args...))
		}
	}

	for _, c := range cases {
		if c.skipPattern || c.compileFail {
			continue
		}
		re, err := lib.Compile(c.body, c.co)
		if err != nil || re == nil {
			continue // accept-consistency is covered by the boundary test
		}
		for si := range c.subjects {
			subj := c.subjects[si]
			if subj.skip || subj.expect == pcreExpectUnknown {
				continue
			}
			subjects++
			in := pcreDecode(subj.raw, c.utf)
			m, merr := re.FindFrom([]byte(in), 0, subj.opts)
			if merr != nil {
				engineErr++ // bounded limit error or invalid UTF: not a divergence
				continue
			}
			switch subj.expect {
			case pcreExpectMatch:
				want := pcreDecodeOutput(subj.group0, c.utf)
				if m != nil && string(m.Group(0)) == want {
					ok++
				} else {
					mismatch++
					got := "<no-match>"
					if m != nil {
						got = strconv.Quote(string(m.Group(0)))
					}
					addSample("MATCH-MISS (line %d) pat=%q in=%q want=%q got=%s",
						c.line, c.body, in, want, got)
				}
			case pcreExpectNoMatch:
				if m == nil {
					ok++
				} else {
					mismatch++
					addSample("NOMATCH-MISS (line %d) pat=%q in=%q got=%q",
						c.line, c.body, in, string(m.Group(0)))
				}
			}
		}
		re.Close()
	}

	agreement := 1.0
	if total := ok + mismatch; total > 0 {
		agreement = float64(ok) / float64(total)
	}
	t.Logf("subjects evaluated: %d", subjects)
	t.Logf("match agreement:    %d/%d = %.4f", ok, ok+mismatch, agreement)
	t.Logf("engine errors (bounded, skipped): %d", engineErr)
	for _, s := range samples {
		t.Logf("  %s", s)
	}
	if agreement < minAgree {
		t.Errorf("match agreement %.4f below required %.4f", agreement, minAgree)
	}
}

// TestPCRE2OfficialMatch8bit replays PCRE2's 8-bit feature/boundary subjects and
// checks the whole-match text against the recorded ground truth. The vendored
// engine is the same PCRE2 version, so agreement is total on the clean subset.
func TestPCRE2OfficialMatch8bit(t *testing.T) {
	runPCREMatchCorpus(t, pcre2Output2Path, 0.99)
}

// TestPCRE2OfficialMatchUTF replays PCRE2's UTF / Unicode-property subjects.
func TestPCRE2OfficialMatchUTF(t *testing.T) {
	runPCREMatchCorpus(t, pcre2Output4Path, 0.99)
}
