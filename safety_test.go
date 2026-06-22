package pcre2lite_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	lib "github.com/VillanCh/go-pcre2-lite"
	p2 "github.com/VillanCh/go-pcre2-lite/regexp2"
	dl "github.com/dlclark/regexp2"
)

// withDeadline runs fn in a goroutine and reports whether it finished within d.
// It is a watchdog: a correctly bounded engine must always finish, so a missed
// deadline means catastrophic backtracking (or a hang) that the match/depth
// limits failed to contain. The leaked goroutine on timeout is acceptable in a
// failing test that is about to abort.
func withDeadline(d time.Duration, fn func()) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// redosCases are classic catastrophic-backtracking patterns paired with inputs
// crafted so that a naive backtracking engine explores an exponential or very
// large search space and never (in practice) returns.
var redosCases = []struct {
	name    string
	pattern string
	input   string
}{
	{"nested-plus-anchored", `(a+)+$`, strings.Repeat("a", 60) + "!"},
	{"nested-star-anchored", `(a*)*$`, strings.Repeat("a", 60) + "!"},
	{"alt-overlap", `(a|a)*$`, strings.Repeat("a", 60) + "!"},
	{"alt-overlap-2", `^(a|ab)+$`, strings.Repeat("ab", 40) + "!"},
	{"two-quantifier", `(x+x+)+y`, strings.Repeat("x", 60)},
	{"dotstar-count", `(.*,){25}z`, strings.Repeat("a,", 50)},
	{"word-star-anchored", `^(\w+)*$`, strings.Repeat("a", 60) + "!"},
	{"digit-plus-anchored", `(\d+)+$`, strings.Repeat("1", 60) + "!"},
	{"group-alt-plus", `(a+)+\d`, strings.Repeat("a", 60)},
	{"evil-email", `^([a-zA-Z0-9])(([\-.]|[_]+)?([a-zA-Z0-9]+))*(@)`, strings.Repeat("a", 60) + "!"},
}

// redosWatchdog is generous: the PCRE2 default match limit (10M steps) resolves
// well under this even on a slow machine. We only want to catch a true hang.
const redosWatchdog = 8 * time.Second

func isLimitErr(err error) bool {
	return errors.Is(err, lib.ErrMatchLimit) || errors.Is(err, lib.ErrDepthLimit)
}

// TestReDoSLowLevelBounded verifies that the low-level engine, with the default
// (build-time) limits, always terminates on catastrophic patterns and reports
// either a clean no-match or a bounded ErrMatchLimit / ErrDepthLimit.
func TestReDoSLowLevelBounded(t *testing.T) {
	for _, tc := range redosCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			re := lib.MustCompile(tc.pattern, lib.CompileOptions{UTF: true, UCP: true})
			defer re.Close()

			var (
				matched bool
				err     error
				elapsed time.Duration
			)
			finished := withDeadline(redosWatchdog, func() {
				start := time.Now()
				matched, err = re.Match([]byte(tc.input))
				elapsed = time.Since(start)
			})
			if !finished {
				t.Fatalf("match did not finish within %v: catastrophic backtracking was NOT bounded", redosWatchdog)
			}
			t.Logf("matched=%v err=%v elapsed=%v", matched, err, elapsed)
			if matched {
				t.Errorf("input was crafted not to match, but matched")
			}
			if err != nil && !isLimitErr(err) {
				t.Errorf("unexpected error (want nil or limit error): %v", err)
			}
		})
	}
}

// TestReDoSCompatBounded verifies the same guarantee through the regexp2
// drop-in layer with its default options.
func TestReDoSCompatBounded(t *testing.T) {
	for _, tc := range redosCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			re := p2.MustCompile(tc.pattern, 0)

			var (
				matched bool
				err     error
				elapsed time.Duration
			)
			finished := withDeadline(redosWatchdog, func() {
				start := time.Now()
				matched, err = re.MatchString(tc.input)
				elapsed = time.Since(start)
			})
			if !finished {
				t.Fatalf("match did not finish within %v: catastrophic backtracking was NOT bounded", redosWatchdog)
			}
			t.Logf("matched=%v err=%v elapsed=%v", matched, err, elapsed)
			if matched {
				t.Errorf("input was crafted not to match, but matched")
			}
			if err != nil && !isLimitErr(err) {
				t.Errorf("unexpected error (want nil or limit error): %v", err)
			}
		})
	}
}

// TestReDoSTightLimitFailsFast verifies that lowering the match limit turns a
// runaway pattern into a fast, deterministic ErrMatchLimit. This is the knob
// applications should use to defend against malicious patterns/inputs.
func TestReDoSTightLimitFailsFast(t *testing.T) {
	const tightLimit = 50000
	const fast = 500 * time.Millisecond

	hitLimit := 0
	for _, tc := range redosCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			re := lib.MustCompile(tc.pattern, lib.CompileOptions{
				UTF: true, UCP: true, MatchLimit: tightLimit,
			})
			defer re.Close()

			var (
				err     error
				elapsed time.Duration
			)
			finished := withDeadline(redosWatchdog, func() {
				start := time.Now()
				_, err = re.Match([]byte(tc.input))
				elapsed = time.Since(start)
			})
			if !finished {
				t.Fatalf("match did not finish within %v even with a tight limit", redosWatchdog)
			}
			t.Logf("err=%v elapsed=%v", err, elapsed)
			if elapsed > fast {
				t.Errorf("tight limit should fail fast, took %v (> %v)", elapsed, fast)
			}
			if isLimitErr(err) {
				hitLimit++
			}
		})
	}
	// At least some of the crafted cases must actually exercise the limit;
	// otherwise the protection is not being demonstrated at all.
	if hitLimit == 0 {
		t.Errorf("no case hit the match limit; expected the tight limit to bite")
	}
	t.Logf("%d/%d cases hit the tight match limit", hitLimit, len(redosCases))
}

// TestReDoSCompatSetMatchLimits verifies the regexp2-layer SetMatchLimits knob
// provides the same fail-fast behaviour.
func TestReDoSCompatSetMatchLimits(t *testing.T) {
	re := p2.MustCompile(`(a+)+$`, 0)
	if err := re.SetMatchLimits(50000, 0); err != nil {
		t.Fatalf("SetMatchLimits: %v", err)
	}
	input := strings.Repeat("a", 80) + "!"

	var (
		err     error
		elapsed time.Duration
	)
	finished := withDeadline(redosWatchdog, func() {
		start := time.Now()
		_, err = re.MatchString(input)
		elapsed = time.Since(start)
	})
	if !finished {
		t.Fatalf("match did not finish within %v", redosWatchdog)
	}
	t.Logf("err=%v elapsed=%v", err, elapsed)
	if elapsed > 500*time.Millisecond {
		t.Errorf("expected fail-fast, took %v", elapsed)
	}
}

// TestReDoSDefaultDoesNotKillLegitMatch makes sure the ReDoS protection does
// not produce false positives: a genuinely-matching input on the very same
// patterns must still match cleanly under the default limits.
func TestReDoSDefaultDoesNotKillLegitMatch(t *testing.T) {
	legit := []struct {
		pattern string
		input   string
	}{
		{`(a+)+$`, strings.Repeat("a", 60)},
		{`(a*)*$`, strings.Repeat("a", 60)},
		{`(a|a)*$`, strings.Repeat("a", 60)},
		{`^(\w+)*$`, strings.Repeat("a", 60)},
		{`(\d+)+$`, strings.Repeat("1", 60)},
	}
	for _, tc := range legit {
		re := p2.MustCompile(tc.pattern, 0)
		ok, err := re.MatchString(tc.input)
		if err != nil {
			t.Errorf("pattern %q: unexpected error on legit input: %v", tc.pattern, err)
			continue
		}
		if !ok {
			t.Errorf("pattern %q: expected match on legit input", tc.pattern)
		}
	}
}

// TestReDoSParityWithDlclarkTimeout is a comparison: dlclark/regexp2 needs an
// explicit MatchTimeout to survive these inputs (its default is "forever").
// This documents that go-pcre2-lite is bounded by the engine even at defaults.
func TestReDoSParityWithDlclarkTimeout(t *testing.T) {
	for _, tc := range redosCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dre := dl.MustCompile(tc.pattern, 0)
			dre.MatchTimeout = 250 * time.Millisecond

			var derr error
			finished := withDeadline(redosWatchdog, func() {
				_, derr = dre.MatchString(tc.input)
			})
			if !finished {
				t.Fatalf("dlclark did not finish within watchdog even with timeout set")
			}
			t.Logf("dlclark err=%v", derr)
		})
	}
}

// --- edge / safety input tests ---------------------------------------------

// TestEmptyPattern verifies an empty pattern matches at position 0 on any input
// and matches the empty string, mirroring regexp2.
func TestEmptyPattern(t *testing.T) {
	re := p2.MustCompile("", 0)
	dre := dl.MustCompile("", 0)

	for _, in := range []string{"", "a", "hello", "你好"} {
		m, err := re.FindStringMatch(in)
		if err != nil {
			t.Fatalf("p2 empty pattern on %q: %v", in, err)
		}
		dm, _ := dre.FindStringMatch(in)
		if (m == nil) != (dm == nil) {
			t.Fatalf("empty pattern match presence mismatch on %q: p2=%v dl=%v", in, m != nil, dm != nil)
		}
		if m != nil && dm != nil {
			if m.Index != dm.Index || m.Length != dm.Length {
				t.Errorf("empty pattern on %q: p2 idx=%d len=%d, dl idx=%d len=%d",
					in, m.Index, m.Length, dm.Index, dm.Length)
			}
		}
	}
}

// TestHugeRepetitionCompile verifies an oversized quantifier does not crash and
// is handled gracefully (PCRE2 caps a single quantifier at 65535).
func TestHugeRepetitionCompile(t *testing.T) {
	for _, pat := range []string{`a{1000000}`, `a{0,1000000}`, `(ab){100000}`} {
		re, err := lib.Compile(pat, lib.CompileOptions{})
		if err != nil {
			var ce *lib.CompileError
			if !errors.As(err, &ce) {
				t.Errorf("pattern %q: want CompileError, got %T: %v", pat, err, err)
			}
			t.Logf("pattern %q rejected at compile: %v", pat, err)
			continue
		}
		re.Close()
		t.Logf("pattern %q compiled", pat)
	}
}

// TestDeepNestingCompile verifies deeply nested groups do not crash the
// compiler (PCRE2 enforces a parenthesis nesting limit and returns an error).
func TestDeepNestingCompile(t *testing.T) {
	for _, depth := range []int{100, 250, 1000, 5000} {
		pat := strings.Repeat("(", depth) + "a" + strings.Repeat(")", depth)
		re, err := lib.Compile(pat, lib.CompileOptions{})
		if err != nil {
			t.Logf("depth=%d rejected: %v", depth, err)
			continue
		}
		re.Close()
		t.Logf("depth=%d compiled", depth)
	}
}

// TestDeepNestingMatchNoStackOverflow drives a moderately deep pattern against
// a long input. PCRE2 10.x uses heap-based matching, so this must not overflow
// the C stack; it should finish (match, no-match, or a bounded limit error).
func TestDeepNestingMatchNoStackOverflow(t *testing.T) {
	re, err := lib.Compile(`(((((((((a)))))))))*$`, lib.CompileOptions{UTF: true})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer re.Close()

	input := []byte(strings.Repeat("a", 5000) + "b")
	var matchErr error
	finished := withDeadline(redosWatchdog, func() {
		_, matchErr = re.Match(input)
	})
	if !finished {
		t.Fatalf("deep-nesting match did not finish within %v", redosWatchdog)
	}
	if matchErr != nil && !isLimitErr(matchErr) {
		t.Errorf("unexpected error: %v", matchErr)
	}
}

// TestLongInputBooleanBounded checks that scanning a multi-megabyte subject for
// a simple pattern is fast and allocation-light, with no pathological blowup.
func TestLongInputBounded(t *testing.T) {
	re := lib.MustCompile(`needle`, lib.CompileOptions{})
	defer re.Close()

	haystack := append(bytes.Repeat([]byte("a"), 16<<20), []byte("needle")...)

	var (
		ok      bool
		err     error
		elapsed time.Duration
	)
	finished := withDeadline(redosWatchdog, func() {
		start := time.Now()
		ok, err = re.Match(haystack)
		elapsed = time.Since(start)
	})
	if !finished {
		t.Fatalf("16MB scan did not finish within %v", redosWatchdog)
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected to find needle")
	}
	t.Logf("16MB scan elapsed=%v", elapsed)
}

// TestInvalidUTF8LowLevel verifies that, in UTF mode, an invalid UTF-8 subject
// produces a clean ErrBadUTF instead of a crash or garbage.
func TestInvalidUTF8LowLevel(t *testing.T) {
	re := lib.MustCompile(`.`, lib.CompileOptions{UTF: true})
	defer re.Close()

	_, err := re.Match([]byte{0xff, 0xfe, 0x00, 0x80})
	if !errors.Is(err, lib.ErrBadUTF) {
		t.Errorf("want ErrBadUTF for invalid UTF-8 subject, got %v", err)
	}
}

// TestInvalidUTF8Compat verifies that the regexp2 layer tolerates invalid UTF-8
// input by normalising it (no error, no crash), like a rune-oriented API.
func TestInvalidUTF8Compat(t *testing.T) {
	re := p2.MustCompile(`.`, 0)
	in := string([]byte{0xff, 0xfe, 'a', 0x80})
	m, err := re.FindStringMatch(in)
	if err != nil {
		t.Fatalf("compat layer should normalise invalid UTF-8, got error: %v", err)
	}
	if m == nil {
		t.Fatalf("expected a match on normalised input")
	}
}

// TestNullBytesInSubject verifies NUL bytes in the subject are data, not
// terminators (the API is length-delimited).
func TestNullBytesInSubject(t *testing.T) {
	re := lib.MustCompile(`b`, lib.CompileOptions{})
	defer re.Close()

	subject := []byte("a\x00b\x00c")
	m, err := re.Find(subject, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil || m.Groups[0].Start != 2 {
		t.Fatalf("expected match of 'b' at byte 2, got %+v", m)
	}
}

// TestConcurrentReDoSNoRace runs many goroutines hammering a bounded runaway
// pattern to ensure the scratch pool and limits behave under contention.
func TestConcurrentReDoSNoRace(t *testing.T) {
	re := lib.MustCompile(`(a+)+$`, lib.CompileOptions{UTF: true, MatchLimit: 20000})
	defer re.Close()
	input := []byte(strings.Repeat("a", 60) + "!")

	const workers = 32
	done := make(chan struct{}, workers)
	finished := withDeadline(redosWatchdog, func() {
		for i := 0; i < workers; i++ {
			go func() {
				defer func() { done <- struct{}{} }()
				for j := 0; j < 20; j++ {
					_, _ = re.Match(input)
				}
			}()
		}
		for i := 0; i < workers; i++ {
			<-done
		}
	})
	if !finished {
		t.Fatalf("concurrent bounded matches did not finish within %v", redosWatchdog)
	}
}
