package pcre2lite_test

import (
	"testing"

	p2 "github.com/VillanCh/go-pcre2-lite/regexp2"
	dl "github.com/dlclark/regexp2"
)

func TestReplaceDifferential(t *testing.T) {
	cases := []struct {
		name        string
		pattern     string
		opts        dl.RegexOptions
		input       string
		replacement string
		startAt     int
		count       int
	}{
		{"simple", `\d+`, 0, "a1 b22 c333", "#", -1, -1},
		{"group-ref", `(\w+)@(\w+)`, 0, "me@host you@there", "$2.$1", -1, -1},
		{"named-ref", `(?<k>\w+)=(?<v>\w+)`, 0, "a=1;b=2", "${v}:${k}", -1, -1},
		{"whole-match", `\d+`, 0, "x9y", "[$&]", -1, -1},
		{"dollar-literal", `a`, 0, "banana", "$$", -1, -1},
		{"left-right", `b`, 0, "abc", "<$`|$'>", -1, -1},
		{"count-limit", `o`, 0, "ooooo", "0", -1, 2},
		{"unicode", `(\p{Han})`, 0, "你好", "[$1]", -1, -1},
		{"no-match", `z+`, 0, "abc", "X", -1, -1},
		{"last-group", `(a)(b)(c)`, 0, "abc", "$+", -1, -1},
		{"ignorecase", `a`, dl.IgnoreCase, "AaA", "_", -1, -1},
		{"replace-empty", `x*`, 0, "abc", "-", -1, -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reDL := dl.MustCompile(c.pattern, c.opts)
			reP2 := p2.MustCompile(c.pattern, p2.RegexOptions(c.opts))
			outDL, errDL := reDL.Replace(c.input, c.replacement, c.startAt, c.count)
			outP2, errP2 := reP2.Replace(c.input, c.replacement, c.startAt, c.count)
			if (errDL == nil) != (errP2 == nil) {
				t.Fatalf("error mismatch: dl=%v p2=%v", errDL, errP2)
			}
			if outDL != outP2 {
				t.Fatalf("replace mismatch:\n dl=%q\n p2=%q", outDL, outP2)
			}
		})
	}
}

func TestEscapeDifferential(t *testing.T) {
	inputs := []string{
		"", "abc", "a.b*c", "1+1=2", "(group)", "[set]", "a\tb\nc",
		"price$ 100%", "你好 世界", "back\\slash", "caret^dollar$",
		"a b c", "#comment", "{brace}", "|pipe|", "\x00nul", "\x07bell",
	}
	for _, in := range inputs {
		if got, want := p2.Escape(in), dl.Escape(in); got != want {
			t.Errorf("Escape(%q): p2=%q dl=%q", in, got, want)
		}
	}
}

func TestUnescapeDifferential(t *testing.T) {
	inputs := []string{
		"abc", `a\.b`, `\d\w`, `tab\there`, `\x41\x42`, `\x{1F600}`,
		`\u0041`, `\101`, `\n\r\t`, `no slashes`, `end\\`, `\cA`, `\a\f\v`,
	}
	for _, in := range inputs {
		gotP2, errP2 := p2.Unescape(in)
		gotDL, errDL := dl.Unescape(in)
		if (errP2 == nil) != (errDL == nil) {
			t.Errorf("Unescape(%q) error mismatch: p2=%v dl=%v", in, errP2, errDL)
			continue
		}
		if errP2 == nil && gotP2 != gotDL {
			t.Errorf("Unescape(%q): p2=%q dl=%q", in, gotP2, gotDL)
		}
	}
}

func TestReplaceFuncDifferential(t *testing.T) {
	reDL := dl.MustCompile(`\w+`, 0)
	reP2 := p2.MustCompile(`\w+`, 0)
	input := "hello world foo"
	outDL, _ := reDL.ReplaceFunc(input, func(m dl.Match) string {
		return "[" + m.String() + "]"
	}, -1, -1)
	outP2, _ := reP2.ReplaceFunc(input, func(m p2.Match) string {
		return "[" + m.String() + "]"
	}, -1, -1)
	if outDL != outP2 {
		t.Fatalf("ReplaceFunc mismatch: dl=%q p2=%q", outDL, outP2)
	}
}

func TestStartingAtDifferential(t *testing.T) {
	reDL := dl.MustCompile(`\w+`, 0)
	reP2 := p2.MustCompile(`\w+`, 0)
	input := "abc def ghi"
	for _, start := range []int{0, 1, 4, 7, 11} {
		mDL, _ := reDL.FindStringMatchStartingAt(input, start)
		mP2, _ := reP2.FindStringMatchStartingAt(input, start)
		if (mDL == nil) != (mP2 == nil) {
			t.Fatalf("start %d nil mismatch", start)
		}
		if mDL != nil {
			if mDL.String() != mP2.String() || mDL.Index != mP2.Index {
				t.Fatalf("start %d: dl=(%q,%d) p2=(%q,%d)", start,
					mDL.String(), mDL.Index, mP2.String(), mP2.Index)
			}
		}
	}
}
