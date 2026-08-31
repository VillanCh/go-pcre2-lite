package pcre2lite_test

import (
	"regexp"
	"strings"
	"testing"

	lib "github.com/VillanCh/go-pcre2-lite"
	p2 "github.com/VillanCh/go-pcre2-lite/regexp2"
	dl "github.com/dlclark/regexp2"
)

// The benchmarks compare three backends on identical work:
//
//	dl  = github.com/dlclark/regexp2          (the engine we replace)
//	p2  = .../go-pcre2-lite/regexp2           (drop-in compat, rune output)
//	ll  = .../go-pcre2-lite (low-level)       (byte-oriented fast path)
//	std = standard library regexp (RE2)       (only where syntax allows)
//
// Run with: go test -bench . -benchmem ./

const emailPattern = `[\w.+-]+@[\w-]+\.[\w.-]+`

func buildLargeInput(unit string, target int) string {
	var b strings.Builder
	for b.Len() < target {
		b.WriteString(unit)
	}
	return b.String()
}

func BenchmarkMatchShort(b *testing.B) {
	input := "please contact test.user+tag@example.co.uk for details"
	inputB := []byte(input)

	b.Run("dl", func(b *testing.B) {
		re := dl.MustCompile(emailPattern, 0)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if ok, _ := re.MatchString(input); !ok {
				b.Fatal("no match")
			}
		}
	})
	b.Run("p2", func(b *testing.B) {
		re := p2.MustCompile(emailPattern, 0)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if ok, _ := re.MatchString(input); !ok {
				b.Fatal("no match")
			}
		}
	})
	b.Run("ll", func(b *testing.B) {
		re := lib.MustCompile(emailPattern, lib.CompileOptions{UTF: true, UCP: true})
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if ok, _ := re.Match(inputB); !ok {
				b.Fatal("no match")
			}
		}
	})
	b.Run("std", func(b *testing.B) {
		re := regexp.MustCompile(emailPattern)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if !re.Match(inputB) {
				b.Fatal("no match")
			}
		}
	})
}

func BenchmarkFindCaptures(b *testing.B) {
	pattern := `(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})`
	input := "log entry at 2023-06-22T14:30:59 recorded"
	inputB := []byte(input)

	b.Run("dl", func(b *testing.B) {
		re := dl.MustCompile(pattern, 0)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m, _ := re.FindStringMatch(input)
			if m == nil {
				b.Fatal("no match")
			}
		}
	})
	b.Run("p2", func(b *testing.B) {
		re := p2.MustCompile(pattern, 0)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m, _ := re.FindStringMatch(input)
			if m == nil {
				b.Fatal("no match")
			}
		}
	})
	b.Run("ll", func(b *testing.B) {
		re := lib.MustCompile(pattern, lib.CompileOptions{UTF: true, UCP: true})
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m, _ := re.Find(inputB, 0)
			if m == nil {
				b.Fatal("no match")
			}
		}
	})
	b.Run("std", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if re.FindSubmatchIndex(inputB) == nil {
				b.Fatal("no match")
			}
		}
	})
}

func BenchmarkLargeInput(b *testing.B) {
	pattern := emailPattern
	// 100KB of filler with one match near the end.
	input := buildLargeInput("the quick brown fox jumps over the lazy dog. ", 100*1024) +
		"needle@host.example.org"
	inputB := []byte(input)

	b.Run("dl", func(b *testing.B) {
		re := dl.MustCompile(pattern, 0)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if ok, _ := re.MatchString(input); !ok {
				b.Fatal("no match")
			}
		}
	})
	b.Run("p2", func(b *testing.B) {
		re := p2.MustCompile(pattern, 0)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if ok, _ := re.MatchString(input); !ok {
				b.Fatal("no match")
			}
		}
	})
	b.Run("ll", func(b *testing.B) {
		re := lib.MustCompile(pattern, lib.CompileOptions{UTF: true, UCP: true})
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if ok, _ := re.Match(inputB); !ok {
				b.Fatal("no match")
			}
		}
	})
	b.Run("std", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if !re.Match(inputB) {
				b.Fatal("no match")
			}
		}
	})
}

func BenchmarkUnicode(b *testing.B) {
	pattern := `\p{Han}+`
	input := buildLargeInput("abc 你好世界 def 安全 ", 8*1024)
	inputB := []byte(input)

	b.Run("dl", func(b *testing.B) {
		re := dl.MustCompile(pattern, 0)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if ok, _ := re.MatchString(input); !ok {
				b.Fatal("no match")
			}
		}
	})
	b.Run("p2", func(b *testing.B) {
		re := p2.MustCompile(pattern, 0)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if ok, _ := re.MatchString(input); !ok {
				b.Fatal("no match")
			}
		}
	})
	b.Run("ll", func(b *testing.B) {
		re := lib.MustCompile(pattern, lib.CompileOptions{UTF: true, UCP: true})
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if ok, _ := re.Match(inputB); !ok {
				b.Fatal("no match")
			}
		}
	})
}

// Backreferences are not supported by RE2 (std regexp), so only dl/p2/ll.
func BenchmarkBackref(b *testing.B) {
	pattern := `(\w{3,})\s+\1`
	input := "the the quick brown fox fox jumps"
	inputB := []byte(input)

	b.Run("dl", func(b *testing.B) {
		re := dl.MustCompile(pattern, 0)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if ok, _ := re.MatchString(input); !ok {
				b.Fatal("no match")
			}
		}
	})
	b.Run("p2", func(b *testing.B) {
		re := p2.MustCompile(pattern, 0)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if ok, _ := re.MatchString(input); !ok {
				b.Fatal("no match")
			}
		}
	})
	b.Run("ll", func(b *testing.B) {
		re := lib.MustCompile(pattern, lib.CompileOptions{UTF: true, UCP: true})
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if ok, _ := re.Match(inputB); !ok {
				b.Fatal("no match")
			}
		}
	})
}

func BenchmarkFindAllGlobal(b *testing.B) {
	pattern := `\w+`
	input := buildLargeInput("alpha beta gamma delta epsilon ", 4*1024)
	inputB := []byte(input)

	b.Run("dl", func(b *testing.B) {
		re := dl.MustCompile(pattern, 0)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			n := 0
			m, _ := re.FindStringMatch(input)
			for m != nil {
				n++
				m, _ = re.FindNextMatch(m)
			}
			if n == 0 {
				b.Fatal("no matches")
			}
		}
	})
	b.Run("p2", func(b *testing.B) {
		re := p2.MustCompile(pattern, 0)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			n := 0
			m, _ := re.FindStringMatch(input)
			for m != nil {
				n++
				m, _ = re.FindNextMatch(m)
			}
			if n == 0 {
				b.Fatal("no matches")
			}
		}
	})
	b.Run("ll", func(b *testing.B) {
		re := lib.MustCompile(pattern, lib.CompileOptions{UTF: true, UCP: true})
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ms, _ := re.FindAll(inputB, -1)
			if len(ms) == 0 {
				b.Fatal("no matches")
			}
		}
	})
	b.Run("std", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if len(re.FindAll(inputB, -1)) == 0 {
				b.Fatal("no matches")
			}
		}
	})
}

// BenchmarkManyTinyMatches stresses the "massive number of tiny matches" case,
// where per-match cgo overhead dominates. The batched FindAll/iterator turns N
// cgo round trips into ceil(N/256), so this is where the optimization shows the
// largest effect.
func BenchmarkManyTinyMatches(b *testing.B) {
	pattern := `[a-z]`                          // one match per character
	input := strings.Repeat("abcdefghij", 3000) // 30000 matches
	inputB := []byte(input)

	b.Run("dl", func(b *testing.B) {
		re := dl.MustCompile(pattern, 0)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			n := 0
			m, _ := re.FindStringMatch(input)
			for m != nil {
				n++
				m, _ = re.FindNextMatch(m)
			}
			if n == 0 {
				b.Fatal("no matches")
			}
		}
	})
	b.Run("p2", func(b *testing.B) {
		re := p2.MustCompile(pattern, 0)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			n := 0
			m, _ := re.FindStringMatch(input)
			for m != nil {
				n++
				m, _ = re.FindNextMatch(m)
			}
			if n == 0 {
				b.Fatal("no matches")
			}
		}
	})
	b.Run("ll", func(b *testing.B) {
		re := lib.MustCompile(pattern, lib.CompileOptions{UTF: true, UCP: true})
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ms, _ := re.FindAll(inputB, -1)
			if len(ms) == 0 {
				b.Fatal("no matches")
			}
		}
	})
	b.Run("std", func(b *testing.B) {
		re := regexp.MustCompile(pattern)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if len(re.FindAll(inputB, -1)) == 0 {
				b.Fatal("no matches")
			}
		}
	})
}

func BenchmarkConcurrentMatch(b *testing.B) {
	input := []byte("please contact test.user+tag@example.co.uk for details")
	inputStr := string(input)

	b.Run("dl", func(b *testing.B) {
		re := dl.MustCompile(emailPattern, 0)
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				re.MatchString(inputStr)
			}
		})
	})
	b.Run("ll", func(b *testing.B) {
		re := lib.MustCompile(emailPattern, lib.CompileOptions{UTF: true, UCP: true})
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				re.Match(input)
			}
		})
	})
	b.Run("std", func(b *testing.B) {
		re := regexp.MustCompile(emailPattern)
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				re.Match(input)
			}
		})
	})
}

func BenchmarkCompile(b *testing.B) {
	pattern := `(?<y>\d{4})-(?<m>\d{2})-(?<d>\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(Z|[+-]\d{2}:\d{2})?`

	b.Run("dl", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			dl.MustCompile(pattern, 0)
		}
	})
	b.Run("ll", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			re := lib.MustCompile(pattern, lib.CompileOptions{UTF: true, UCP: true})
			re.Close()
		}
	})
	b.Run("std", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			regexp.MustCompile(pattern)
		}
	})
}
