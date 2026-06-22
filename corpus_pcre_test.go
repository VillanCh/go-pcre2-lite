package pcre2lite_test

// This test drives a large, real-world corpus of patterns through both
// github.com/dlclark/regexp2 and go-pcre2-lite/regexp2 and asserts they agree.
//
// Corpus: testdata/pcre2_testoutput1.txt is "testoutput1" from PCRE2 v10.21
// (public domain), as shipped with github.com/dlclark/regexp2. We do NOT assert
// against the file's recorded output (it encodes non-UTF 8-bit semantics);
// instead we use it purely as a source of thousands of real patterns and
// inputs, and run a DIFFERENTIAL comparison between the two Go libraries. The
// parsing mirrors dlclark's own regexp_pcre_test.go so option handling matches.

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	p2 "github.com/VillanCh/go-pcre2-lite/regexp2"
	dl "github.com/dlclark/regexp2"
)

const corpusPath = "testdata/pcre2_testoutput1.txt"

// minAgreement is the required whole-match agreement ratio between dlclark and
// pcre2-lite over the corpus inputs where both engines compile the pattern and
// neither errors (timeout / match-limit). The remaining divergences stem from
// the documented UTF-vs-8bit and .NET-vs-PCRE semantic differences.
const minAgreement = 0.95

func TestPCRECorpusDifferential(t *testing.T) {
	f, err := os.Open(corpusPath)
	if err != nil {
		t.Skipf("corpus not available: %v", err)
	}
	defer f.Close()

	var (
		patterns        int
		compileBoth     int
		compileOnlyP2   int // p2 compiles, dl rejects (p2/PCRE2 is more capable)
		compileOnlyDl   int // dl compiles, p2 rejects
		inputsCompared  int
		matchConsistent int
		matchDiverge    int
		engineErrors    int
	)
	var diffSamples []string
	addSample := func(format string, args ...interface{}) {
		if len(diffSamples) < 50 {
			diffSamples = append(diffSamples, fmt.Sprintf(format, args...))
		}
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)

	for sc.Scan() {
		line := sc.Text()
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		delim := line[0]
		if delim != '/' && delim != '"' {
			continue
		}

		// Accumulate a (possibly multi-line) pattern up to its closing delim.
		pattern := line
		allowFirst := false
		for !corpusContainsEnder(line, delim, allowFirst) {
			if !sc.Scan() {
				break
			}
			line = sc.Text()
			pattern += "\n" + line
			allowFirst = true
		}
		patterns++

		dlRe, p2Re := corpusCompile(pattern)

		// Collect the test inputs for this pattern (4-space indented lines),
		// stopping at the blank separator before the next pattern.
		var inputs []string
		for sc.Scan() {
			l := sc.Text()
			if strings.TrimSpace(l) == "" {
				break
			}
			if strings.HasPrefix(l, "\\= Expect") {
				continue
			}
			if strings.HasPrefix(l, "    ") {
				inputs = append(inputs, strings.TrimRight(l[4:], " "))
			}
			// Result lines (" 0: ...", "No match", " 1: ...") are ignored.
		}

		switch {
		case dlRe != nil && p2Re != nil:
			compileBoth++
		case dlRe == nil && p2Re != nil:
			compileOnlyP2++
			continue
		case dlRe != nil && p2Re == nil:
			compileOnlyDl++
			addSample("COMPILE-DIVERGE (p2 rejects, dl accepts) pattern=%q", pattern)
			continue
		default:
			continue // both rejected: consistent
		}

		dlRe.MatchTimeout = 250 * time.Millisecond

		for _, raw := range inputs {
			if raw == "\\" {
				continue
			}
			in := corpusUnescape(raw)

			dlM, dlErr := dlRe.FindStringMatch(in)
			p2M, p2Err := p2Re.FindStringMatch(in)
			if dlErr != nil || p2Err != nil {
				engineErrors++
				continue
			}
			inputsCompared++
			if corpusMatchEqual(dlM, p2M) {
				matchConsistent++
			} else {
				matchDiverge++
				addSample("MATCH-DIVERGE pattern=%q input=%q dl=%s p2=%s",
					pattern, in, dlMatchStr(dlM), p2MatchStr(p2M))
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanning corpus: %v", err)
	}

	agreement := 1.0
	if total := matchConsistent + matchDiverge; total > 0 {
		agreement = float64(matchConsistent) / float64(total)
	}

	t.Logf("patterns parsed:           %d", patterns)
	t.Logf("compiled by both:          %d", compileBoth)
	t.Logf("compiled only by p2 (PCRE2 superset): %d", compileOnlyP2)
	t.Logf("compiled only by dl:       %d", compileOnlyDl)
	t.Logf("inputs compared:           %d", inputsCompared)
	t.Logf("match consistent:          %d", matchConsistent)
	t.Logf("match divergent:           %d", matchDiverge)
	t.Logf("engine errors (skipped):   %d", engineErrors)
	t.Logf("whole-match agreement:     %.4f", agreement)
	if len(diffSamples) > 0 {
		t.Logf("divergence samples (capped at %d):", len(diffSamples))
		for _, s := range diffSamples {
			t.Logf("  %s", s)
		}
	}

	if agreement < minAgreement {
		t.Errorf("whole-match agreement %.4f below required %.4f", agreement, minAgreement)
	}
}

// corpusCompile parses the trailing modifiers off a /pattern/opts spec and
// compiles it with both engines. A compile failure (or panic) yields a nil
// *Regexp for that engine rather than aborting the whole corpus run.
func corpusCompile(pattern string) (*dl.Regexp, *p2.Regexp) {
	index := strings.LastIndexAny(pattern, "/\"")
	var dlOpts dl.RegexOptions
	var p2Opts p2.RegexOptions
	if index >= 0 && index+1 < len(pattern) {
		textOptions := pattern[index+1:]
		pattern = pattern[:index+1]
		for _, opt := range strings.Split(textOptions, ",") {
			if opt == "dupnames" {
				continue
			}
			if strings.Contains(opt, "i") {
				dlOpts |= dl.IgnoreCase
				p2Opts |= p2.IgnoreCase
			}
			if strings.Contains(opt, "s") {
				dlOpts |= dl.Singleline
				p2Opts |= p2.Singleline
			}
			if strings.Contains(opt, "m") {
				dlOpts |= dl.Multiline
				p2Opts |= p2.Multiline
			}
			if strings.Contains(opt, "x") {
				dlOpts |= dl.IgnorePatternWhitespace
				p2Opts |= p2.IgnorePatternWhitespace
			}
		}
	}
	if len(pattern) < 2 {
		return nil, nil
	}
	pattern = pattern[1 : len(pattern)-1]

	dlRe := func() (re *dl.Regexp) {
		defer func() { _ = recover() }()
		r, err := dl.Compile(pattern, dlOpts)
		if err != nil {
			return nil
		}
		return r
	}()
	p2Re := func() (re *p2.Regexp) {
		defer func() { _ = recover() }()
		r, err := p2.Compile(pattern, p2Opts)
		if err != nil {
			return nil
		}
		return r
	}()
	return dlRe, p2Re
}

func corpusMatchEqual(a *dl.Match, b *p2.Match) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return a.Index == b.Index && a.Length == b.Length && a.String() == b.String()
}

func dlMatchStr(m *dl.Match) string {
	if m == nil {
		return "<no-match>"
	}
	return fmt.Sprintf("%q", m.String())
}

func p2MatchStr(m *p2.Match) string {
	if m == nil {
		return "<no-match>"
	}
	return fmt.Sprintf("%q", m.String())
}

func corpusContainsEnder(line string, ender byte, allowFirst bool) bool {
	index := strings.LastIndexByte(line, ender)
	if index > 0 {
		return true
	}
	return index == 0 && allowFirst
}

// corpusUnescape decodes the backslash escapes used in the corpus test inputs
// (\xNN, \nnn octal, \n \r \t etc.), mirroring dlclark's unEscapeToMatch.
func corpusUnescape(line string) string {
	idx := strings.IndexRune(line, '\\')
	if idx == -1 {
		return line
	}
	var buf strings.Builder
	buf.WriteString(line[:idx])

	inEscape := false
	for i := idx; i < len(line); i++ {
		ch := line[i]
		if ch == '\\' {
			if inEscape {
				buf.WriteByte(ch)
			}
			inEscape = !inEscape
			continue
		}
		if inEscape {
			switch ch {
			case 'x':
				buf.WriteByte(corpusScanHex(line, &i))
			case 'a':
				buf.WriteByte(0x07)
			case 'b':
				buf.WriteByte('\b')
			case 'e':
				buf.WriteByte(0x1b)
			case 'f':
				buf.WriteByte('\f')
			case 'n':
				buf.WriteByte('\n')
			case 'r':
				buf.WriteByte('\r')
			case 't':
				buf.WriteByte('\t')
			case 'v':
				buf.WriteByte(0x0b)
			default:
				if ch >= '0' && ch <= '7' {
					buf.WriteByte(corpusScanOctal(line, &i))
				} else {
					buf.WriteByte(ch)
				}
			}
			inEscape = false
		} else {
			buf.WriteByte(ch)
		}
	}
	return buf.String()
}

func corpusScanHex(line string, idx *int) byte {
	if *idx >= len(line)-2 {
		return 0
	}
	(*idx)++
	d1 := corpusHexDigit(line[*idx])
	(*idx)++
	d2 := corpusHexDigit(line[*idx])
	if d1 < 0 || d2 < 0 {
		return 0
	}
	return byte(d1*0x10 + d2)
}

func corpusHexDigit(ch byte) int {
	if d := uint(ch - '0'); d <= 9 {
		return int(d)
	}
	if d := uint(ch - 'a'); d <= 5 {
		return int(d + 0xa)
	}
	if d := uint(ch - 'A'); d <= 5 {
		return int(d + 0xa)
	}
	return -1
}

func corpusScanOctal(line string, idx *int) byte {
	c := 3
	if diff := len(line) - *idx; c > diff {
		c = diff
	}
	i := 0
	d := int(line[*idx] - '0')
	for c > 0 && d <= 7 {
		i *= 8
		i += d
		c--
		(*idx)++
		if *idx < len(line) {
			d = int(line[*idx] - '0')
		}
	}
	(*idx)--
	i &= 0xFF
	return byte(i)
}
