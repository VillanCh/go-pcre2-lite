package regexp2_test

import (
	"testing"

	pcre2 "github.com/VillanCh/go-pcre2-lite/regexp2"
	dlregexp2 "github.com/dlclark/regexp2"
)

func TestECMAScriptDifferentialAgainstRegexp2(t *testing.T) {
	tests := []struct {
		pattern string
		options pcre2.RegexOptions
		inputs  []string
	}{
		{`^\s$`, pcre2.ECMAScript, []string{" ", "\n", "\u00a0", "\u2000", "\u200b", "A"}},
		{`^[\Sx]$`, pcre2.ECMAScript, []string{" ", "x", "y", "\u200b"}},
		{`^[^x\S]$`, pcre2.ECMAScript, []string{" ", "x", "y", "\u2000"}},
		{`[]`, pcre2.ECMAScript, []string{"", "x", "\n"}},
		{`[^]`, pcre2.ECMAScript, []string{"", "x", "\n"}},
		{`(?<=a)\u{65}_`, pcre2.ECMAScript | pcre2.Unicode, []string{"ae_", "e_", "ax_"}},
		{`^\u0065$`, pcre2.ECMAScript | pcre2.Unicode, []string{"e", "u0065"}},
		{`^\u{2}$`, pcre2.ECMAScript, []string{"uu", "u{2}"}},
		{`^\u{0_2}$`, pcre2.ECMAScript, []string{"u{0_2}", "uu"}},
		{`^(?=)\x0$`, pcre2.ECMAScript, []string{"x0", "\x00"}},
	}

	for _, tc := range tests {
		t.Run(tc.pattern, func(t *testing.T) {
			pcreRE, err := pcre2.Compile(tc.pattern, tc.options)
			if err != nil {
				t.Fatalf("pcre2 Compile: %v", err)
			}
			dlRE, err := dlregexp2.Compile(tc.pattern, dlregexp2.RegexOptions(tc.options))
			if err != nil {
				t.Fatalf("dlclark Compile: %v", err)
			}
			for _, input := range tc.inputs {
				pcreMatch, pcreErr := pcreRE.FindStringMatch(input)
				dlMatch, dlErr := dlRE.FindStringMatch(input)
				if pcreErr != nil || dlErr != nil {
					t.Fatalf("input %q: pcreErr=%v dlErr=%v", input, pcreErr, dlErr)
				}
				if (pcreMatch == nil) != (dlMatch == nil) {
					t.Errorf("input %q: pcre=%v dl=%v", input, pcreMatch, dlMatch)
					continue
				}
				if pcreMatch != nil && (pcreMatch.Index != dlMatch.Index || pcreMatch.Length != dlMatch.Length || pcreMatch.String() != dlMatch.String()) {
					t.Errorf("input %q: pcre=(%d,%d,%q) dl=(%d,%d,%q)", input,
						pcreMatch.Index, pcreMatch.Length, pcreMatch.String(),
						dlMatch.Index, dlMatch.Length, dlMatch.String())
				}
			}
		})
	}
}

func FuzzECMAScriptCompileAndMatchDoesNotPanic(f *testing.F) {
	for _, seed := range []struct{ pattern, input string }{
		{`(?<=a)\u{65}_`, "ae_"},
		{`[\s\S]`, "\u2028"},
		{`[^]`, "\x00"},
		{`(?=)\x0`, "x0"},
		{`[\u{65}\S]`, "e"},
	} {
		f.Add(seed.pattern, seed.input)
	}
	f.Fuzz(func(t *testing.T, pattern, input string) {
		if len(pattern) > 1024 || len(input) > 4096 {
			t.Skip()
		}
		re, err := pcre2.Compile(pattern, pcre2.ECMAScript|pcre2.Unicode)
		if err == nil {
			_, _ = re.MatchString(input)
		}
	})
}
