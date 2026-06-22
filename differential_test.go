package pcre2lite_test

import (
	"fmt"
	"testing"

	p2 "github.com/VillanCh/go-pcre2-lite/regexp2"
	dl "github.com/dlclark/regexp2"
)

type diffCase struct {
	name    string
	pattern string
	opts    dl.RegexOptions
	input   string
	// g0only restricts comparison to group 0 (the whole match). Used for
	// patterns where group numbering legitimately differs (mixed named and
	// unnamed groups) between .NET and PCRE2.
	g0only bool
}

var diffCases = []diffCase{
	{name: "literal", pattern: `hello`, input: "say hello world"},
	{name: "dot-star", pattern: `a.*b`, input: "xaXXXbz"},
	{name: "alternation", pattern: `cat|dog|bird`, input: "I have a dog and a cat"},
	{name: "two-groups", pattern: `(\d+)-(\d+)`, input: "order 12-345 ok"},
	{name: "nested-groups", pattern: `((\w)(\w))`, input: "ab cd"},
	{name: "named-groups", pattern: `(?<y>\d{4})-(?<m>\d{2})-(?<d>\d{2})`, input: "2023-06-22"},
	{name: "named-after-unnamed", pattern: `(\d+)(?<u>[a-z]+)`, input: "12abc"},
	{name: "noncapturing", pattern: `(?:ab)+(c)`, input: "ababc"},
	{name: "lazy-quant", pattern: `<(.+?)>`, input: "<a><b>"},
	{name: "greedy-quant", pattern: `<(.+)>`, input: "<a><b>"},
	{name: "lookahead", pattern: `\d+(?= dollars)`, input: "100 dollars"},
	{name: "neg-lookahead", pattern: `foo(?!bar)`, input: "foobaz foobar"},
	{name: "lookbehind", pattern: `(?<=\$)\d+`, input: "price $42 only"},
	{name: "neg-lookbehind", pattern: `(?<!\$)\b\d+`, input: "id 7 $9"},
	{name: "backref", pattern: `(\w+) \1`, input: "hello hello world"},
	{name: "named-backref", pattern: `(?<q>['"]).*?\k<q>`, input: `say "hi" now`},
	{name: "atomic-group", pattern: `(?>a+)ab`, input: "aaab"},
	{name: "anchors", pattern: `^\w+`, input: "word here"},
	{name: "end-anchor", pattern: `\w+$`, input: "first last"},
	{name: "char-class", pattern: `[a-fA-F0-9]+`, input: "color #FF00ab end"},
	{name: "neg-class", pattern: `[^aeiou ]+`, input: "rhythm and blues"},
	{name: "quantifier-range", pattern: `a{2,4}`, input: "aaaaaa"},
	{name: "word-boundary", pattern: `\bcat\b`, input: "the cat sat catalog"},
	{name: "optional", pattern: `colou?r`, input: "color and colour"},
	{name: "ignorecase", pattern: `hello`, opts: dl.IgnoreCase, input: "HELLO Hello hello"},
	{name: "multiline", pattern: `^\w+`, opts: dl.Multiline, input: "one\ntwo\nthree"},
	{name: "singleline-dot", pattern: `a.b`, opts: dl.Singleline, input: "a\nb"},
	{name: "unicode-han", pattern: `\p{Han}+`, input: "你好abc世界"},
	{name: "unicode-letter", pattern: `\p{L}+`, input: "héllo wörld"},
	{name: "unicode-digit", pattern: `\d+`, input: "abc１２３def"},
	{name: "unicode-mixed", pattern: `(\w+)`, input: "café déjà"},
	{name: "email-ish", pattern: `(\w+)@(\w+)\.(\w+)`, input: "x me@host.com y"},
	{name: "ip-ish", pattern: `(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})`, input: "ip 192.168.0.1 end"},
	{name: "repeated-cap", pattern: `(\d)+`, input: "12345", g0only: true},
	{name: "global-digits", pattern: `\d+`, input: "a1 b22 c333 d4444"},
	{name: "empty-star", pattern: `a*`, input: "baab"},
	{name: "unicode-boundary", pattern: `\bcafé\b`, input: "le café est"},
	{name: "alt-groups", pattern: `(foo)|(bar)`, input: "we got bar here"},
	{name: "optional-group", pattern: `(ab)?c`, input: "c abc"},
	{name: "dotall-multiline", pattern: `^.+$`, opts: dl.Singleline, input: "line1\nline2"},
}

type capSig struct {
	matched bool
	s       string
	idx     int
	length  int
}

type matchSig struct {
	groups []capSig
}

func sigsDL(re *dl.Regexp, input string) ([]matchSig, error) {
	var out []matchSig
	m, err := re.FindStringMatch(input)
	if err != nil {
		return nil, err
	}
	for m != nil {
		gc := m.GroupCount()
		ms := matchSig{}
		for i := 0; i < gc; i++ {
			g := m.GroupByNumber(i)
			cs := capSig{}
			if g != nil && len(g.Captures) > 0 {
				cs.matched = true
				cs.s = g.String()
				cs.idx = g.Index
				cs.length = g.Length
			}
			ms.groups = append(ms.groups, cs)
		}
		out = append(out, ms)
		if len(out) > 2000 {
			break
		}
		m, err = re.FindNextMatch(m)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func sigsP2(re *p2.Regexp, input string) ([]matchSig, error) {
	var out []matchSig
	m, err := re.FindStringMatch(input)
	if err != nil {
		return nil, err
	}
	for m != nil {
		gc := m.GroupCount()
		ms := matchSig{}
		for i := 0; i < gc; i++ {
			g := m.GroupByNumber(i)
			cs := capSig{}
			if g != nil && len(g.Captures) > 0 {
				cs.matched = true
				cs.s = g.String()
				cs.idx = g.Index
				cs.length = g.Length
			}
			ms.groups = append(ms.groups, cs)
		}
		out = append(out, ms)
		if len(out) > 2000 {
			break
		}
		m, err = re.FindNextMatch(m)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func TestDifferential(t *testing.T) {
	var total, passed int
	for _, c := range diffCases {
		total++
		t.Run(c.name, func(t *testing.T) {
			reDL, errDL := dl.Compile(c.pattern, c.opts)
			reP2, errP2 := p2.Compile(c.pattern, p2.RegexOptions(c.opts))
			if (errDL == nil) != (errP2 == nil) {
				t.Fatalf("compile mismatch: dlclark err=%v, pcre2 err=%v", errDL, errP2)
			}
			if errDL != nil {
				return
			}

			sDL, e1 := sigsDL(reDL, c.input)
			sP2, e2 := sigsP2(reP2, c.input)
			if e1 != nil || e2 != nil {
				t.Fatalf("run error: dlclark=%v pcre2=%v", e1, e2)
			}

			if len(sDL) != len(sP2) {
				t.Fatalf("match count: dlclark=%d pcre2=%d\n dl=%s\n p2=%s",
					len(sDL), len(sP2), dumpSigs(sDL), dumpSigs(sP2))
			}
			for i := range sDL {
				// group 0 (whole match) must always agree
				if sDL[i].groups[0] != sP2[i].groups[0] {
					t.Fatalf("match %d group0: dlclark=%+v pcre2=%+v",
						i, sDL[i].groups[0], sP2[i].groups[0])
				}
				if c.g0only {
					continue
				}
				if len(sDL[i].groups) != len(sP2[i].groups) {
					t.Fatalf("match %d group count: dlclark=%d pcre2=%d",
						i, len(sDL[i].groups), len(sP2[i].groups))
				}
				for g := range sDL[i].groups {
					if sDL[i].groups[g] != sP2[i].groups[g] {
						t.Fatalf("match %d group %d: dlclark=%+v pcre2=%+v",
							i, g, sDL[i].groups[g], sP2[i].groups[g])
					}
				}
			}
			passed++
		})
	}
	t.Logf("differential parity: %d/%d cases compared", passed, total)
}

func dumpSigs(ms []matchSig) string {
	s := ""
	for i, m := range ms {
		s += fmt.Sprintf("[%d:", i)
		for _, g := range m.groups {
			s += fmt.Sprintf(" {%v %q %d %d}", g.matched, g.s, g.idx, g.length)
		}
		s += "]"
	}
	return s
}
