package pcre2lite_test

import (
	"testing"
	"time"

	lib "github.com/VillanCh/go-pcre2-lite"
	p2 "github.com/VillanCh/go-pcre2-lite/regexp2"
	dl "github.com/dlclark/regexp2"
)

func isASCIIStr(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// FuzzCompile checks that compilation never panics for arbitrary patterns: a
// pattern is either accepted or rejected with an error, in both UTF and 8-bit
// modes. A panic (e.g. a slice bounds error in option handling) fails the fuzz.
func FuzzCompile(f *testing.F) {
	for _, p := range []string{
		`abc`, `(a+)+`, `[a-z]+`, `(?<n>x)`, `\p{L}`, `[`, `(`, `a{2,1}`,
		`\`, `(?P<n>a)`, `(?:abc)`, `\x{1F600}`, `(?i)AbC`, `a**`, `[]`,
		`(?<=foo)bar`, `\1(a)`, `(?#comment)x`,
	} {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, pattern string) {
		for _, utf := range []bool{false, true} {
			re, err := lib.Compile(pattern, lib.CompileOptions{UTF: utf, UCP: utf})
			if err == nil {
				re.Close()
			}
		}
	})
}

// FuzzMatch checks that matching never panics and always terminates. The engine
// is compiled with tight match/depth limits so that even a catastrophic
// pattern+input pair returns promptly (ErrMatchLimit) instead of hanging the
// fuzzer. This is the core robustness guarantee under adversarial input.
func FuzzMatch(f *testing.F) {
	seeds := []struct{ p, in string }{
		{`abc`, `xxabcyy`},
		{`(a+)+$`, `aaaaaaaaaa!`},
		{`\d{3}-\d{4}`, `call 555-1234 now`},
		{`(?<y>\d+)-(?<m>\d+)`, `2024-06`},
		{`a|b|c`, `zzcab`},
		{`(.*)*x`, `aaaaaaaaaa`},
		{`(\w+)\s+\1`, `the the end`},
		{``, ``},
		{`[a-z]+`, ``},
		{`^$`, ``},
	}
	for _, s := range seeds {
		f.Add(s.p, s.in)
	}
	f.Fuzz(func(t *testing.T, pattern, input string) {
		mustFinish(t, 5*time.Second, pattern, input, func() {
			re, err := lib.Compile(pattern, lib.CompileOptions{
				UTF: false, MatchLimit: 100000, DepthLimit: 100000,
			})
			if err != nil {
				return
			}
			defer re.Close()

			b := []byte(input)
			_, _ = re.Match(b)
			_, _ = re.Find(b, 0)
			_, _ = re.FindAll(b, 1000)
		})
	})
}

// mustFinish runs fn under a watchdog. Compilation and matching with tight
// limits must always finish quickly; a missed deadline means some construct is
// not bounded by the match/depth limits, which we want the fuzzer to surface as
// a reproducible failure rather than a silent hang.
func mustFinish(t *testing.T, d time.Duration, pattern, input string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("operation exceeded %v (unbounded?): pattern=%q len(pattern)=%d len(input)=%d",
			d, pattern, len(pattern), len(input))
	}
}

// FuzzMatchUTF is like FuzzMatch but exercises the UTF-8 validation and rune
// handling path with arbitrary (possibly invalid) byte sequences.
func FuzzMatchUTF(f *testing.F) {
	for _, s := range []struct{ p, in string }{
		{`.`, "héllo"},
		{`\p{Han}+`, "你好世界"},
		{`(\X)`, "e\u0301"},
		{`.`, string([]byte{0xff, 0xfe})},
	} {
		f.Add(s.p, s.in)
	}
	f.Fuzz(func(t *testing.T, pattern, input string) {
		mustFinish(t, 5*time.Second, pattern, input, func() {
			re, err := lib.Compile(pattern, lib.CompileOptions{
				UTF: true, UCP: true, MatchLimit: 100000, DepthLimit: 100000,
			})
			if err != nil {
				return
			}
			defer re.Close()
			// Invalid UTF-8 subjects must return ErrBadUTF, not panic.
			_, _ = re.Match([]byte(input))

			// The compat layer must normalise anything without erroring/panic.
			cre, cerr := p2.Compile(pattern, 0)
			if cerr != nil {
				return
			}
			_ = cre.SetMatchLimits(100000, 100000)
			_, _ = cre.FindStringMatch(input)
		})
	})
}

// FuzzDifferential compares boolean match results between dlclark/regexp2 and
// go-pcre2-lite/regexp2. It is restricted to ASCII pattern+input (to exclude
// the documented UTF-vs-8bit semantic differences) and only compares when both
// engines compile the pattern and neither hits a limit/timeout. A divergence
// surfaced by `go test -fuzz` should be triaged: it is either a real
// compatibility bug to fix, or a genuine .NET-vs-PCRE semantic difference to
// add to MIGRATION.md.
func FuzzDifferential(f *testing.F) {
	seeds := []struct{ p, in string }{
		{`abc`, `xxabcyy`},
		{`a+b`, `aaab`},
		{`[a-z]+`, `Hello`},
		{`\d{2,4}`, `12345`},
		{`(foo|bar)+`, `foobarbaz`},
		{`^a.*z$`, `a to z`},
		{`(?i)abc`, `ABC`},
		{`\bword\b`, `a word here`},
		{`(a)(b)?(c)`, `ac`},
		{`x*`, `xxxx`},
	}
	for _, s := range seeds {
		f.Add(s.p, s.in)
	}
	f.Fuzz(func(t *testing.T, pattern, input string) {
		if !isASCIIStr(pattern) || !isASCIIStr(input) {
			return
		}
		dre, derr := dl.Compile(pattern, 0)
		p2re, perr := p2.Compile(pattern, 0)
		if derr != nil || perr != nil {
			return
		}
		dre.MatchTimeout = 100 * time.Millisecond
		if err := p2re.SetMatchLimits(200000, 200000); err != nil {
			return
		}

		dM, dErr := dre.MatchString(input)
		pM, pErr := p2re.MatchString(input)
		if dErr != nil || pErr != nil {
			return
		}
		if dM != pM {
			t.Errorf("boolean match divergence: pattern=%q input=%q dl=%v p2=%v",
				pattern, input, dM, pM)
		}
	})
}
