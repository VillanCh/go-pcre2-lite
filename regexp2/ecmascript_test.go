package regexp2

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestECMAScriptWhitespaceEscapes(t *testing.T) {
	spaces := []rune{
		'\t', '\n', '\v', '\f', '\r', ' ',
		'\u00a0', '\u1680', '\u2000', '\u2001', '\u2002', '\u2003',
		'\u2004', '\u2005', '\u2006', '\u2007', '\u2008', '\u2009',
		'\u200a', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff',
	}
	nonSpaces := []rune{'A', '\u0085', '\u180e', '\u200b'}

	space := MustCompile(`^\s$`, ECMAScript)
	nonSpace := MustCompile(`^\S$`, ECMAScript)
	for _, r := range spaces {
		if ok, err := space.MatchString(string(r)); err != nil || !ok {
			t.Errorf("\\s did not match U+%04X: ok=%v err=%v", r, ok, err)
		}
		if ok, err := nonSpace.MatchString(string(r)); err != nil || ok {
			t.Errorf("\\S matched U+%04X: ok=%v err=%v", r, ok, err)
		}
	}
	for _, r := range nonSpaces {
		if ok, err := space.MatchString(string(r)); err != nil || ok {
			t.Errorf("\\s matched U+%04X: ok=%v err=%v", r, ok, err)
		}
		if ok, err := nonSpace.MatchString(string(r)); err != nil || !ok {
			t.Errorf("\\S did not match U+%04X: ok=%v err=%v", r, ok, err)
		}
	}
}

func TestECMAScriptWhitespaceInsideClasses(t *testing.T) {
	cases := []struct {
		pattern string
		matches []rune
		rejects []rune
	}{
		{`^[x\s]$`, []rune{'x', '\u2000'}, []rune{'y', '\u200b'}},
		{`^[\Sx]$`, []rune{'x', 'y', '\u200b'}, []rune{'\u2000'}},
		{`^[^\S]$`, []rune{'\u2000'}, []rune{'x', '\u200b'}},
		{`^[^x\S]$`, []rune{'\u2000'}, []rune{'x', 'y', '\u200b'}},
		{`^[\s\S]$`, []rune{'x', '\n', '\u2000', '\u200b'}, nil},
		{`^[^\s\S]$`, nil, []rune{'x', '\n', '\u2000', '\u200b'}},
	}
	for _, tc := range cases {
		re := MustCompile(tc.pattern, ECMAScript)
		for _, r := range tc.matches {
			if ok, err := re.MatchString(string(r)); err != nil || !ok {
				t.Errorf("%s did not match U+%04X: ok=%v err=%v", tc.pattern, r, ok, err)
			}
		}
		for _, r := range tc.rejects {
			if ok, err := re.MatchString(string(r)); err != nil || ok {
				t.Errorf("%s matched U+%04X: ok=%v err=%v", tc.pattern, r, ok, err)
			}
		}
	}
}

func TestECMAScriptEmptyCharacterClasses(t *testing.T) {
	cases := []struct {
		pattern string
		input   string
		want    bool
	}{
		{`[]`, "\x00", false},
		{`([])\1`, "\x00\x00", false},
		{`[^]`, "\x00", true},
		{`[^]`, "\n", true},
		{`([^])\1`, "aa", true},
		{`[]?`, "", true},
	}
	for _, tc := range cases {
		re, err := Compile(tc.pattern, ECMAScript)
		if err != nil {
			t.Fatalf("Compile(%q): %v", tc.pattern, err)
		}
		got, err := re.MatchString(tc.input)
		if err != nil || got != tc.want {
			t.Errorf("%q on %q: got=%v want=%v err=%v", tc.pattern, tc.input, got, tc.want, err)
		}
	}
}

func TestECMAScriptDotLineTerminators(t *testing.T) {
	plain := MustCompile(`^.$`, ECMAScript)
	for _, r := range []rune{'\n', '\r', '\u2028', '\u2029'} {
		if ok, err := plain.MatchString(string(r)); err != nil || ok {
			t.Errorf("plain dot matched U+%04X: ok=%v err=%v", r, ok, err)
		}
	}
	for _, r := range []rune{'x', '\u0085', '\u2000'} {
		if ok, err := plain.MatchString(string(r)); err != nil || !ok {
			t.Errorf("plain dot did not match U+%04X: ok=%v err=%v", r, ok, err)
		}
	}

	dotAll := MustCompile(`^.$`, ECMAScript|Singleline)
	for _, r := range []rune{'\n', '\r', '\u2028', '\u2029'} {
		if ok, err := dotAll.MatchString(string(r)); err != nil || !ok {
			t.Errorf("dotAll did not match U+%04X: ok=%v err=%v", r, ok, err)
		}
	}
}

func TestECMAScriptRewriteLeavesEscapedBackslashAlone(t *testing.T) {
	const expr = `^\\s$`
	got, changed := rewriteECMAScriptPattern(expr, false, false)
	if changed || got != expr {
		t.Fatalf("rewriteECMAScriptPattern(%q) = %q, changed=%v", expr, got, changed)
	}
}

func TestECMAScriptLegacyDecimalEscape(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{name: "production-regression", pattern: `(~~?)(?:(?!~)<inner>)+\2`, input: "~<inner>\x02", want: true},
		{name: "existing-backreference", pattern: `(a)\1`, input: "aa", want: true},
		{name: "out-of-range-before-capture", pattern: `\2(a)`, input: "\x02a", want: true},
		{name: "two-digit-octal", pattern: `(a)\12`, input: "a\n", want: true},
		{name: "identity-eight", pattern: `\8`, input: "8", want: true},
		{name: "class-octal", pattern: `[\2]`, input: "\x02", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			re, err := Compile(test.pattern, ECMAScript)
			if err != nil {
				t.Fatal(err)
			}
			matched, err := re.MatchString(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if matched != test.want {
				t.Fatalf("MatchString(%q) = %v, want %v", test.input, matched, test.want)
			}
		})
	}
}

func TestECMAScriptUnsetBackreferencesMatchEmpty(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
	}{
		{name: "numeric forward", pattern: `\1(A)`, input: "A"},
		{name: "numeric unmatched branch", pattern: `(A|(B))\2C`, input: "AC"},
		{name: "named forward", pattern: `\k<x>(?<x>A)`, input: "A"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			re, err := Compile(test.pattern, ECMAScript)
			if err != nil {
				t.Fatal(err)
			}
			matched, err := re.MatchString(test.input)
			if err != nil || !matched {
				t.Fatalf("MatchString(%q) = %v, err=%v", test.input, matched, err)
			}
		})
	}
}

func TestECMAScriptLegacyUnknownNamedReferenceIsIdentityEscape(t *testing.T) {
	re, err := Compile(`^\k<missing>$`, ECMAScript)
	if err != nil {
		t.Fatal(err)
	}
	if matched, err := re.MatchString("k<missing>"); err != nil || !matched {
		t.Fatalf("legacy identity escape: matched=%v err=%v", matched, err)
	}
	if _, err := Compile(`\k<missing>`, ECMAScript|Unicode); err == nil {
		t.Fatal("Unicode mode must reject an unknown named reference")
	}
}

func TestECMAScriptIdentifierNameCaptures(t *testing.T) {
	const astral = "𝓑𝓻𝓸𝔀𝓷"
	re, err := Compile(`^(?<$>a)(?<π>b)(?<\u{1d4d1}\u{1d4fb}\u{1d4f8}\u{1d500}\u{1d4f7}>c)\k<$>$`, ECMAScript|Unicode)
	if err != nil {
		t.Fatal(err)
	}
	m, err := re.FindStringMatch("abca")
	if err != nil || m == nil {
		t.Fatalf("match=%v err=%v", m, err)
	}
	for name, want := range map[string]string{"$": "a", "π": "b", astral: "c"} {
		group := m.GroupByName(name)
		if group == nil || group.String() != want {
			t.Errorf("group %q = %v, want %q", name, group, want)
		}
	}
	if got := re.GroupNameFromNumber(3); got != astral {
		t.Fatalf("group 3 name = %q, want %q", got, astral)
	}
	replaced, err := re.Replace("abca", `${$}-${π}-${𝓑𝓻𝓸𝔀𝓷}`, -1, -1)
	if err != nil || replaced != "a-b-c" {
		t.Fatalf("Replace = %q, err=%v", replaced, err)
	}
}

func TestECMAScriptRejectsInvalidCaptureNames(t *testing.T) {
	for _, pattern := range []string{
		`(?<>a)`,
		`(?<1a>a)`,
		`(?<a-b>a)`,
		`(?<a!>a)`,
		`(?<\uD800>a)`,
		`(?<\u{DFFF}>a)`,
	} {
		if _, err := Compile(pattern, ECMAScript|Unicode); err == nil {
			t.Errorf("Compile(%q) unexpectedly succeeded", pattern)
		}
	}
}

func TestECMAScriptRejectsUnknownNamedReferenceWhenNamesExist(t *testing.T) {
	if _, err := Compile(`(?<present>a)\k<missing>`, ECMAScript); err == nil {
		t.Fatal("expected an unknown named-reference error")
	}
}

func TestECMAScriptLookbehindDirectionAndCaptures(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    []string
		options RegexOptions
	}{
		{name: "repeated capture", pattern: `(?<=(\w){3})def`, input: "abcdef", want: []string{"def", "a"}},
		{name: "ordered variable alternatives", pattern: `.*(?<=(..|...|....))(.*)`, input: "xabcd", want: []string{"xabcd", "cd", ""}},
		{name: "forward backreference", pattern: `(?<=\1(\w))d`, input: "abcCd", want: []string{"d", "C"}, options: IgnoreCase},
		{name: "greedy capture before backreference", pattern: `(?<=(\w+)\1)c`, input: "ababc", want: []string{"c", "abab"}},
		{name: "external capture reference", pattern: `(.)(?<=(\1\1))`, input: "abb", want: []string{"b", "b", "bb"}},
		{name: "mutual references", pattern: `(?<=a(.\2)b(\1)).{4}`, input: "aabcacbc", want: []string{"cacb", "a", ""}},
		{name: "outer quantifier backtracks into lookbehind", pattern: `^faaao?(?<=^f[oa]+(?=o))`, input: "faaao", want: []string{"faaa"}},
		{name: "multiline start anchor", pattern: `(?<=^[a-c]{3})def`, input: "xyz\nabcdef", want: []string{"def"}, options: Multiline},
		{name: "word boundary sees right context", pattern: `(?<=\b)[d-f]{3}`, input: "abc def", want: []string{"def"}},
		{name: "nested lookahead", pattern: `(?<=ab(?=c)\wd)\w\w`, input: "abcdef", want: []string{"ef"}},
		{name: "nested lookbehind", pattern: `(?<=a(?=([bc]{2}(?<!a{2}))d)\w{3})\w\w`, input: "abcdef", want: []string{"ef", "bc"}},
		{name: "multiple assertions", pattern: `(?<=a)b(?<=ab)c`, input: "abc", want: []string{"bc"}},
		{name: "sliced subject does not expose prefix", pattern: `(?=(abcdefghijklmn))(?<=\1)a`, input: "abcdefghijklmn", want: nil},
		{name: "negative assertion clears capture", pattern: `(?<!(\d){3})f`, input: "abcdef", want: []string{"f", "<unset>"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			re, err := Compile(test.pattern, ECMAScript|test.options)
			if err != nil {
				t.Fatalf("Compile(%q): %v", test.pattern, err)
			}
			m, err := re.FindStringMatch(test.input)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			if m != nil {
				for _, group := range m.Groups() {
					if len(group.Captures) == 0 {
						got = append(got, "<unset>")
					} else {
						got = append(got, group.String())
					}
				}
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestECMAScriptNamedLookbehindCapture(t *testing.T) {
	re, err := Compile(`(?<=(?<a>\w)+)f`, ECMAScript|Unicode)
	if err != nil {
		t.Fatal(err)
	}
	m, err := re.FindStringMatch("abcdef")
	if err != nil || m == nil {
		t.Fatalf("match=%v err=%v", m, err)
	}
	if group := m.GroupByName("a"); group == nil || group.String() != "a" {
		t.Fatalf("named group = %v, want a", group)
	}
}

func TestECMAScriptLookbehindConcurrentAndLimits(t *testing.T) {
	re, err := Compile(`(?<=(\w){3})def`, ECMAScript)
	if err != nil {
		t.Fatal(err)
	}
	if err := re.SetMatchLimits(100000, 100000); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for i := 0; i < 32; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for j := 0; j < 100; j++ {
				m, matchErr := re.FindStringMatch("abcdef")
				if matchErr != nil || m == nil || m.GroupByNumber(1).String() != "a" {
					t.Errorf("match=%v err=%v", m, matchErr)
					return
				}
			}
		}()
	}
	wait.Wait()
}

func TestECMAScriptSetEscapeRangeEndpoint(t *testing.T) {
	re, err := Compile(`^[^\s-_]$`, ECMAScript)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		input string
		want  bool
	}{
		{input: "a", want: true},
		{input: " ", want: false},
		{input: "-", want: false},
		{input: "_", want: false},
	} {
		matched, err := re.MatchString(test.input)
		if err != nil {
			t.Fatal(err)
		}
		if matched != test.want {
			t.Fatalf("MatchString(%q) = %v, want %v", test.input, matched, test.want)
		}
	}
}

func TestECMAScriptUnicodeEscapes(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		options RegexOptions
		input   string
		want    bool
	}{
		{"code point escape", `(?<=a)\u{65}_`, ECMAScript | Unicode, "ae_", true},
		{"code point escape rejects wrong prefix", `(?<=a)\u{65}_`, ECMAScript | Unicode, "e_", false},
		{"four digit escape unicode", `^\u0065$`, ECMAScript | Unicode, "e", true},
		{"four digit escape legacy", `^\u0065$`, ECMAScript, "e", true},
		{"surrogate pair unicode", `^\uD83D\uDE00$`, ECMAScript | Unicode, "😀", true},
		{"code point escape in class", `^[\u{65}]$`, ECMAScript | Unicode, "e", true},
		{"legacy brace is quantifier", `^\u{2}$`, ECMAScript, "uu", true},
		{"legacy invalid brace is literal", `^\u{0_2}$`, ECMAScript, "u{0_2}", true},
		{"legacy incomplete hex is identity escape", `^(?=)\x0$`, ECMAScript, "x0", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			re, err := Compile(tc.pattern, tc.options)
			if err != nil {
				t.Fatalf("Compile(%q): %v", tc.pattern, err)
			}
			got, err := re.MatchString(tc.input)
			if err != nil || got != tc.want {
				t.Fatalf("%q on %q: got=%v want=%v err=%v", tc.pattern, tc.input, got, tc.want, err)
			}
		})
	}
}

func TestECMAScriptUnsupportedUnicodeEscapesFailClosed(t *testing.T) {
	for _, pattern := range []string{
		`\x0`,
		`\u{6_5}`,
		`\u{110000}`,
		`\u{D800}`,
		`\uD800`,
	} {
		if _, err := Compile(pattern, ECMAScript|Unicode); err == nil {
			t.Errorf("Compile(%q) unexpectedly succeeded", pattern)
		}
	}
	for _, pattern := range []string{`\u{D800}`, `\uD800`} {
		if _, err := Compile(pattern, ECMAScript|Unicode); !errors.Is(err, ErrUnsupportedRune) {
			t.Errorf("Compile(%q): err=%v, want ErrUnsupportedRune", pattern, err)
		}
	}
}

func TestFindRunesMatchStartingAtASCIIAndUnicode(t *testing.T) {
	ascii := MustCompile(`b?`, ECMAScript)
	for _, start := range []int{0, 1, 2, 3} {
		m, err := ascii.FindRunesMatchStartingAt([]rune("abc"), start)
		if err != nil {
			t.Fatalf("ASCII start %d: %v", start, err)
		}
		if m == nil || m.Index < start {
			t.Fatalf("ASCII start %d: invalid match %#v", start, m)
		}
	}
	if _, err := ascii.FindRunesMatchStartingAt([]rune("abc"), 4); err == nil {
		t.Fatal("expected out-of-range start error")
	}

	unicode := MustCompile(`雪?`, ECMAScript)
	m, err := unicode.FindRunesMatchStartingAt([]rune("甲雪乙"), 1)
	if err != nil || m == nil || m.Index != 1 || m.String() != "雪" {
		t.Fatalf("Unicode start: match=%v err=%v", m, err)
	}
	m, err = unicode.FindRunesMatchStartingAt([]rune("甲雪乙"), 3)
	if err != nil || m == nil || m.Index != 3 || m.String() != "" {
		t.Fatalf("Unicode end start: match=%v err=%v", m, err)
	}
}

func TestRuneAPIsRejectUnrepresentableUTF16Values(t *testing.T) {
	re := MustCompile(`.`, ECMAScript)
	inputs := [][]rune{{0xd800}, {0xdfff}, {0x110000}, {-1}}
	for _, input := range inputs {
		if _, err := re.MatchRunes(input); !errors.Is(err, ErrUnsupportedRune) {
			t.Errorf("MatchRunes(%U): err=%v", input, err)
		}
		if _, err := re.FindRunesMatch(input); !errors.Is(err, ErrUnsupportedRune) {
			t.Errorf("FindRunesMatch(%U): err=%v", input, err)
		}
		if _, err := re.FindRunesMatchStartingAt(input, 0); !errors.Is(err, ErrUnsupportedRune) {
			t.Errorf("FindRunesMatchStartingAt(%U): err=%v", input, err)
		}
	}
}
