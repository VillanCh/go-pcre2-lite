package pcre2lite

import (
	"errors"
	"sync"
	"testing"
)

func mustCompile(t *testing.T, pattern string, opts CompileOptions) *Regexp {
	t.Helper()
	re, err := Compile(pattern, opts)
	if err != nil {
		t.Fatalf("Compile(%q) failed: %v", pattern, err)
	}
	t.Cleanup(func() { re.Close() })
	return re
}

func TestMatchBasic(t *testing.T) {
	re := mustCompile(t, `^h.llo`, CompileOptions{})
	ok, err := re.Match([]byte("hello world"))
	if err != nil || !ok {
		t.Fatalf("expected match, got ok=%v err=%v", ok, err)
	}
	ok, err = re.Match([]byte("goodbye"))
	if err != nil || ok {
		t.Fatalf("expected no match, got ok=%v err=%v", ok, err)
	}
}

func TestCaptureGroups(t *testing.T) {
	re := mustCompile(t, `(\d+)-(\d+)`, CompileOptions{})
	m, err := re.Find([]byte("ab 12-345 cd"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if m == nil {
		t.Fatal("expected a match")
	}
	if got := m.GroupString(0); got != "12-345" {
		t.Errorf("group0=%q want 12-345", got)
	}
	if got := m.GroupString(1); got != "12" {
		t.Errorf("group1=%q want 12", got)
	}
	if got := m.GroupString(2); got != "345" {
		t.Errorf("group2=%q want 345", got)
	}
	if re.CaptureCount() != 3 {
		t.Errorf("CaptureCount=%d want 3", re.CaptureCount())
	}
}

func TestNamedGroups(t *testing.T) {
	re := mustCompile(t, `(?<year>\d{4})-(?<mon>\d{2})`, CompileOptions{})
	if n, ok := re.NamedGroupNumber("year"); !ok || n != 1 {
		t.Errorf("year -> %d,%v want 1,true", n, ok)
	}
	if n, ok := re.NamedGroupNumber("mon"); !ok || n != 2 {
		t.Errorf("mon -> %d,%v want 2,true", n, ok)
	}
	if name, ok := re.NumberedGroupName(1); !ok || name != "year" {
		t.Errorf("group 1 -> %q,%v want year,true", name, ok)
	}
	m, err := re.Find([]byte("2023-06-22"), 0)
	if err != nil || m == nil {
		t.Fatalf("find: m=%v err=%v", m, err)
	}
	if m.GroupString(1) != "2023" || m.GroupString(2) != "06" {
		t.Errorf("captures: %q %q", m.GroupString(1), m.GroupString(2))
	}
}

func TestLookbehind(t *testing.T) {
	re := mustCompile(t, `(?<=foo)bar`, CompileOptions{})
	m, err := re.Find([]byte("foobar"), 0)
	if err != nil || m == nil {
		t.Fatalf("m=%v err=%v", m, err)
	}
	if m.Groups[0].Start != 3 || m.Groups[0].End != 6 {
		t.Errorf("span=%+v want {3 6}", m.Groups[0])
	}
}

func TestBackreference(t *testing.T) {
	re := mustCompile(t, `(\w)\1`, CompileOptions{})
	m, err := re.Find([]byte("hello"), 0)
	if err != nil || m == nil {
		t.Fatalf("m=%v err=%v", m, err)
	}
	if m.GroupString(0) != "ll" {
		t.Errorf("group0=%q want ll", m.GroupString(0))
	}
}

func TestUnicodeProperty(t *testing.T) {
	re := mustCompile(t, `\p{Han}+`, CompileOptions{UTF: true, UCP: true})
	m, err := re.Find([]byte("你好世界x"), 0)
	if err != nil || m == nil {
		t.Fatalf("m=%v err=%v", m, err)
	}
	if got := m.GroupString(0); got != "你好世界" {
		t.Errorf("group0=%q want 你好世界", got)
	}
	if m.Groups[0].End != 12 {
		t.Errorf("byte end=%d want 12", m.Groups[0].End)
	}
}

func TestNULBytes(t *testing.T) {
	re := mustCompile(t, "a\x00b", CompileOptions{})
	ok, err := re.Match([]byte("xa\x00by"))
	if err != nil || !ok {
		t.Fatalf("nul match: ok=%v err=%v", ok, err)
	}
}

func TestInvalidUTF(t *testing.T) {
	re := mustCompile(t, `.`, CompileOptions{UTF: true})
	_, err := re.Match([]byte{0xff, 0xfe})
	if !errors.Is(err, ErrBadUTF) {
		t.Fatalf("expected ErrBadUTF, got %v", err)
	}
}

func TestStartOffset(t *testing.T) {
	re := mustCompile(t, `\d+`, CompileOptions{})
	input := []byte("111 222 333")
	m, err := re.Find(input, 4)
	if err != nil || m == nil {
		t.Fatalf("m=%v err=%v", m, err)
	}
	if m.GroupString(0) != "222" {
		t.Errorf("got %q want 222", m.GroupString(0))
	}
}

func TestMatchLimit(t *testing.T) {
	re := mustCompile(t, `(a+)+$`, CompileOptions{MatchLimit: 1000})
	_, err := re.Match([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa!"))
	if !errors.Is(err, ErrMatchLimit) {
		t.Fatalf("expected ErrMatchLimit, got %v", err)
	}
}

func TestCloseIdempotentAndAfterClose(t *testing.T) {
	re, err := Compile(`abc`, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := re.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := re.Close(); err != nil {
		t.Fatalf("second close should be nil, got %v", err)
	}
	if _, err := re.Match([]byte("abc")); !errors.Is(err, ErrClosed) {
		t.Fatalf("match after close: want ErrClosed, got %v", err)
	}
}

func TestFindAllGlobal(t *testing.T) {
	re := mustCompile(t, `\d+`, CompileOptions{})
	ms, err := re.FindAll([]byte("a1 b22 c333"), -1)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1", "22", "333"}
	if len(ms) != len(want) {
		t.Fatalf("got %d matches want %d", len(ms), len(want))
	}
	for i, w := range want {
		if ms[i].GroupString(0) != w {
			t.Errorf("match %d = %q want %q", i, ms[i].GroupString(0), w)
		}
	}
}

func TestFindAllEmptyMatches(t *testing.T) {
	// Pattern that can match empty; ensures the global loop advances correctly.
	re := mustCompile(t, `a*`, CompileOptions{})
	ms, err := re.FindAll([]byte("aba"), -1)
	if err != nil {
		t.Fatal(err)
	}
	// Expected (PCRE2 semantics): "a" @0, "" @1, "a" @2, "" @3
	got := make([]string, len(ms))
	for i, m := range ms {
		got[i] = m.GroupString(0)
	}
	want := []string{"a", "", "a", ""}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestFindAllUnicodeEmpty(t *testing.T) {
	re := mustCompile(t, `x*`, CompileOptions{UTF: true})
	ms, err := re.FindAll([]byte("你x好"), -1)
	if err != nil {
		t.Fatalf("findall: %v", err)
	}
	// Empty matches must land on rune boundaries, never split a multibyte rune.
	for _, m := range ms {
		if m.Groups[0].Start > 0 && m.Groups[0].Start < len("你") {
			t.Fatalf("empty match split a rune at offset %d", m.Groups[0].Start)
		}
	}
}

func TestCompileError(t *testing.T) {
	_, err := Compile(`(unclosed`, CompileOptions{})
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *CompileError, got %T %v", err, err)
	}
	if ce.Message == "" {
		t.Errorf("expected non-empty compile message")
	}
}

func TestConcurrentMatch(t *testing.T) {
	re := mustCompile(t, `(\w+)@(\w+)`, CompileOptions{})
	input := []byte("user@example more text alice@host")
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				m, err := re.Find(input, 0)
				if err != nil || m == nil {
					t.Errorf("concurrent find failed: %v", err)
					return
				}
				if m.GroupString(1) != "user" {
					t.Errorf("bad capture %q", m.GroupString(1))
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestMatchOptionParity(t *testing.T) {
	cases := []struct {
		got  MatchOption
		want uint32
	}{
		{MatchAnchored, uint32(cP2LMOptAnchored())},
		{MatchNotBOL, uint32(cP2LMOptNotBOL())},
		{MatchNotEOL, uint32(cP2LMOptNotEOL())},
		{MatchNotEmpty, uint32(cP2LMOptNotEmpty())},
		{MatchNotEmptyAtStart, uint32(cP2LMOptNotEmptyAtStart())},
		{MatchEndAnchored, uint32(cP2LMOptEndAnchored())},
	}
	for i, c := range cases {
		if uint32(c.got) != c.want {
			t.Errorf("case %d: Go=%d C=%d", i, uint32(c.got), c.want)
		}
	}
}

func TestBooleanMatchNoAlloc(t *testing.T) {
	re := mustCompile(t, `\w+@\w+\.\w+`, CompileOptions{})
	input := []byte("contact me at test@example.com please")
	allocs := testing.AllocsPerRun(200, func() {
		ok, err := re.Match(input)
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
	})
	if allocs > 0 {
		t.Errorf("boolean Match allocated %.1f times/op, want 0", allocs)
	}
}
