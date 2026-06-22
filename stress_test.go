package pcre2lite_test

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	lib "github.com/VillanCh/go-pcre2-lite"
	p2 "github.com/VillanCh/go-pcre2-lite/regexp2"
)

// Go's native fuzzing engine collects coverage from Go code only; with a cgo
// core the feedback loop stalls (execs/sec drops to zero) even though nothing is
// wrong. TestRandomStress is a deterministic, reproducible alternative that
// actually drives a large number of adversarial pattern+input pairs through the
// engine and asserts every one of them is bounded and panic-free. The patterns
// are biased towards nested quantifiers and alternations (ReDoS shapes) and the
// inputs towards long single-character runs (the backtracking worst case).

func randomStressPattern(rng *rand.Rand) string {
	atoms := []string{"a", "b", "c", `\d`, `\w`, ".", "[a-z]", "[0-9]", "x", "ab", `\s`}
	quants := []string{"*", "+", "?", "{2,4}", "{0,3}", "{1,}", ""}

	var sb strings.Builder
	if rng.Intn(3) == 0 {
		sb.WriteString("^")
	}
	groups := rng.Intn(4) + 1
	for g := 0; g < groups; g++ {
		switch rng.Intn(3) {
		case 1:
			sb.WriteString("(")
		case 2:
			sb.WriteString("(?:")
		}
		opened := rng.Intn(3)
		inner := rng.Intn(3) + 1
		for k := 0; k < inner; k++ {
			sb.WriteString(atoms[rng.Intn(len(atoms))])
			sb.WriteString(quants[rng.Intn(len(quants))])
			if rng.Intn(3) == 0 && k < inner-1 {
				sb.WriteString("|")
			}
		}
		if opened > 0 {
			sb.WriteString(")")
			// Quantifying a group produces nested quantifiers: the classic
			// catastrophic-backtracking shape.
			sb.WriteString(quants[rng.Intn(len(quants))])
		}
	}
	if rng.Intn(2) == 0 {
		sb.WriteString("$")
	}
	return sb.String()
}

func randomStressInput(rng *rand.Rand) string {
	chars := "ab01x ,@"
	n := rng.Intn(64) + 8
	var sb strings.Builder
	c := chars[rng.Intn(len(chars))]
	for i := 0; i < n; i++ {
		if rng.Intn(6) == 0 {
			c = chars[rng.Intn(len(chars))]
		}
		sb.WriteByte(c)
	}
	if rng.Intn(2) == 0 {
		sb.WriteByte('!') // a char outside the usual class set to force mismatch
	}
	return sb.String()
}

func TestRandomStressBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in -short mode")
	}
	rng := rand.New(rand.NewSource(0xC0FFEE))

	const (
		iterations    = 20000
		perOpDeadline = 2 * time.Second
		matchLimit    = 200000
	)

	var (
		compiled int
		maxOp    time.Duration
		slowPat  string
		slowIn   string
	)

	for i := 0; i < iterations; i++ {
		pattern := randomStressPattern(rng)
		input := randomStressInput(rng)
		b := []byte(input)

		re, err := lib.Compile(pattern, lib.CompileOptions{
			MatchLimit: matchLimit, DepthLimit: matchLimit,
		})
		if err != nil {
			continue
		}
		compiled++

		var (
			panicked bool
			panicMsg string
			elapsed  time.Duration
		)
		ok := withDeadline(perOpDeadline, func() {
			defer func() {
				if r := recover(); r != nil {
					panicked = true
					panicMsg = fmt.Sprintf("%v", r)
				}
			}()
			start := time.Now()
			_, _ = re.Match(b)
			_, _ = re.Find(b, 0)
			_, _ = re.FindAll(b, 500)
			elapsed = time.Since(start)
		})
		re.Close()

		if panicked {
			t.Fatalf("panic on pattern=%q input=%q: %s", pattern, input, panicMsg)
		}
		if !ok {
			t.Fatalf("operation exceeded %v (UNBOUNDED): pattern=%q input=%q",
				perOpDeadline, pattern, input)
		}
		if elapsed > maxOp {
			maxOp = elapsed
			slowPat, slowIn = pattern, input
		}
	}

	t.Logf("compiled %d/%d random patterns; all bounded and panic-free", compiled, iterations)
	t.Logf("slowest op: %v on pattern=%q input=%q", maxOp, slowPat, slowIn)
}

func TestRandomStressCompatBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in -short mode")
	}
	rng := rand.New(rand.NewSource(0xBADF00D))

	const (
		iterations    = 10000
		perOpDeadline = 2 * time.Second
	)

	var compiled int
	var maxOp time.Duration

	for i := 0; i < iterations; i++ {
		pattern := randomStressPattern(rng)
		input := randomStressInput(rng)

		re, err := p2.Compile(pattern, 0)
		if err != nil {
			continue
		}
		if err := re.SetMatchLimits(200000, 200000); err != nil {
			t.Fatalf("SetMatchLimits: %v", err)
		}
		compiled++

		var (
			panicked bool
			panicMsg string
			elapsed  time.Duration
		)
		ok := withDeadline(perOpDeadline, func() {
			defer func() {
				if r := recover(); r != nil {
					panicked = true
					panicMsg = fmt.Sprintf("%v", r)
				}
			}()
			start := time.Now()
			_, _ = re.MatchString(input)
			if m, err := re.FindStringMatch(input); err == nil && m != nil {
				// exercise group/replace paths too
				_ = m.GroupCount()
				_, _ = re.Replace(input, "x", 0, -1)
			}
			elapsed = time.Since(start)
		})

		if panicked {
			t.Fatalf("panic on pattern=%q input=%q: %s", pattern, input, panicMsg)
		}
		if !ok {
			t.Fatalf("operation exceeded %v (UNBOUNDED): pattern=%q input=%q",
				perOpDeadline, pattern, input)
		}
		if elapsed > maxOp {
			maxOp = elapsed
		}
	}

	t.Logf("compiled %d/%d random patterns through compat layer; all bounded", compiled, iterations)
	t.Logf("slowest op: %v", maxOp)
}
