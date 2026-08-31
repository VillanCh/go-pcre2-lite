package pcre2lite_test

import (
	"regexp"
	"strings"
	"testing"

	lib "github.com/VillanCh/go-pcre2-lite"
	p2 "github.com/VillanCh/go-pcre2-lite/regexp2"
)

const benchmarkEmailPattern = `[\w.+-]+@[\w-]+\.[\w.-]+`

func BenchmarkMatchShort(b *testing.B) {
	input := "please contact test.user+tag@example.co.uk for details"
	inputBytes := []byte(input)

	b.Run("compat", func(b *testing.B) {
		re := p2.MustCompile(benchmarkEmailPattern, 0)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if ok, err := re.MatchString(input); err != nil || !ok {
				b.Fatalf("match=%v err=%v", ok, err)
			}
		}
	})
	b.Run("low-level", func(b *testing.B) {
		re := lib.MustCompile(benchmarkEmailPattern, lib.CompileOptions{UTF: true, UCP: true})
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if ok, err := re.Match(inputBytes); err != nil || !ok {
				b.Fatalf("match=%v err=%v", ok, err)
			}
		}
	})
	b.Run("stdlib", func(b *testing.B) {
		re := regexp.MustCompile(benchmarkEmailPattern)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if !re.Match(inputBytes) {
				b.Fatal("no match")
			}
		}
	})
}

func BenchmarkECMAScriptLookbehind(b *testing.B) {
	re := p2.MustCompile(`(?<=(\w){3})def`, p2.ECMAScript)
	input := strings.Repeat("x", 4096) + "abcdef"
	b.SetBytes(int64(len(input)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m, err := re.FindStringMatch(input)
		if err != nil || m == nil || m.GroupByNumber(1).String() != "a" {
			b.Fatalf("match=%v err=%v", m, err)
		}
	}
}

func BenchmarkMatchLimit(b *testing.B) {
	re := lib.MustCompile(`(a+)+$`, lib.CompileOptions{
		UTF:        true,
		UCP:        true,
		MatchLimit: 50_000,
	})
	input := []byte(strings.Repeat("a", 60) + "!")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = re.Match(input)
	}
}
