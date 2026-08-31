package regexp2

import (
	"errors"
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
