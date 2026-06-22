package pcre2lite_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	lib "github.com/VillanCh/go-pcre2-lite"
	p2 "github.com/VillanCh/go-pcre2-lite/regexp2"
	dl "github.com/dlclark/regexp2"
)

// The patterns below are the classic regex benchmark suite used by the Go
// standard library (src/regexp/exec_test.go) and reused by dlclark/regexp2.
// Running them across dl / p2 / std gives a like-for-like throughput picture
// over increasingly back-tracking-heavy patterns.
const (
	benchEasy0  = "ABCDEFGHIJKLMNOPQRSTUVWXYZ$"
	benchEasy1  = "A[AB]B[BC]C[CD]D[DE]E[EF]F[FG]G[GH]H[HI]I[IJ]J$"
	benchMedium = "[XYZ]ABCDEFGHIJKLMNOPQRSTUVWXYZ$"
	benchHard   = "[ -~]*ABCDEFGHIJKLMNOPQRSTUVWXYZ$"
	benchHard1  = "ABCD|CDEF|EFGH|GHIJ|IJKL|KLMN|MNOP|OPQR|QRST|STUV|UVWX|WXYZ"
)

var benchTextBuf []byte

// makeBenchText reproduces the pseudo-random ASCII generator from the Go
// standard library regexp benchmarks so results are comparable.
func makeBenchText(n int) []byte {
	if len(benchTextBuf) >= n {
		return benchTextBuf[:n]
	}
	benchTextBuf = make([]byte, n)
	x := ^uint32(0)
	for i := range benchTextBuf {
		x += x
		x ^= 1
		if int32(x) < 0 {
			x ^= 0x88888eef
		}
		if x%31 == 0 {
			benchTextBuf[i] = '\n'
		} else {
			benchTextBuf[i] = byte(x%(0x7E+1-0x20) + 0x20)
		}
	}
	return benchTextBuf
}

func benchStdCorpus(b *testing.B, pattern string, n int) {
	text := makeBenchText(n)
	s := string(text)

	// These patterns are designed not to match the random text. We do not
	// assert on the result here: with a backtracking engine the "hard" patterns
	// can hit a match/depth limit (p2) or a wall-clock timeout (dl) on large
	// inputs, which is exactly the safety behaviour we want -- not a benchmark
	// failure. Correctness is covered by the differential and corpus tests.
	b.Run("dl", func(b *testing.B) {
		re := dl.MustCompile(pattern, 0)
		re.MatchTimeout = time.Second
		b.SetBytes(int64(n))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = re.MatchString(s)
		}
	})
	b.Run("p2", func(b *testing.B) {
		re := p2.MustCompile(pattern, 0)
		b.SetBytes(int64(n))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = re.MatchString(s)
		}
	})
	b.Run("ll", func(b *testing.B) {
		re := lib.MustCompile(pattern, lib.CompileOptions{UTF: true, UCP: true})
		b.SetBytes(int64(n))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = re.Match(text)
		}
	})
	b.Run("std", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		b.SetBytes(int64(n))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = re.Match(text)
		}
	})
}

func BenchmarkCorpusEasy0_1K(b *testing.B)   { benchStdCorpus(b, benchEasy0, 1<<10) }
func BenchmarkCorpusEasy0_32K(b *testing.B)  { benchStdCorpus(b, benchEasy0, 32<<10) }
func BenchmarkCorpusEasy1_1K(b *testing.B)   { benchStdCorpus(b, benchEasy1, 1<<10) }
func BenchmarkCorpusEasy1_32K(b *testing.B)  { benchStdCorpus(b, benchEasy1, 32<<10) }
func BenchmarkCorpusMedium_1K(b *testing.B)  { benchStdCorpus(b, benchMedium, 1<<10) }
func BenchmarkCorpusMedium_32K(b *testing.B) { benchStdCorpus(b, benchMedium, 32<<10) }
func BenchmarkCorpusHard_1K(b *testing.B)    { benchStdCorpus(b, benchHard, 1<<10) }
func BenchmarkCorpusHard_32K(b *testing.B)   { benchStdCorpus(b, benchHard, 32<<10) }
func BenchmarkCorpusHard1_1K(b *testing.B)   { benchStdCorpus(b, benchHard1, 1<<10) }
func BenchmarkCorpusHard1_32K(b *testing.B)  { benchStdCorpus(b, benchHard1, 32<<10) }

// BenchmarkReDoSCost measures the worst-case cost of a single match attempt on
// a catastrophic-backtracking pattern. This quantifies the "blast radius" of a
// malicious input: with the default limit it is bounded (the engine gives up
// after the step budget); with a tight limit it is cheap.
func BenchmarkReDoSCost(b *testing.B) {
	pattern := `(a+)+$`
	input := []byte(strings.Repeat("a", 60) + "!")

	b.Run("default-limit", func(b *testing.B) {
		re := lib.MustCompile(pattern, lib.CompileOptions{UTF: true, UCP: true})
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			re.Match(input) // returns ErrMatchLimit, bounded
		}
	})
	b.Run("tight-limit-50k", func(b *testing.B) {
		re := lib.MustCompile(pattern, lib.CompileOptions{UTF: true, UCP: true, MatchLimit: 50000})
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			re.Match(input)
		}
	})
}
