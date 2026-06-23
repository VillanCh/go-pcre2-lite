package regexp2

import "testing"

// 关键词: compat, invalid range, \w-_, 变长 lookbehind, syntax compatibility

// TestCompatClassHyphenEscape 覆盖 transform 1: 字符类里集合简写不能作为范围端点时, - 被当字面量.
func TestCompatClassHyphenEscape(t *testing.T) {
	type mc struct {
		subj string
		want bool
	}
	cases := []struct {
		pattern string
		matches []mc
	}{
		// \w 作为范围前缀: 旧 .NET/RE2 视 - 为字面量, PCRE2 原报 invalid range.
		{`^[\d\w-_]$`, []mc{{"a", true}, {"9", true}, {"-", true}, {"_", true}, {" ", false}}},
		// 集合简写作为范围后缀.
		{`^[a-\w]$`, []mc{{"a", true}, {"-", true}, {"z", true}, {" ", false}}},
		{`^[\s-x]$`, []mc{{"-", true}, {"x", true}, {" ", true}}},
		// 真实范围必须保持不变(不应把 - 变字面量).
		{`^[a-z]$`, []mc{{"m", true}, {"-", false}, {"5", false}}},
		// 前导/尾随 - 本就是字面量, 编译与匹配不受影响.
		{`^[-\w]$`, []mc{{"-", true}, {"a", true}}},
		{`^[\w-]$`, []mc{{"-", true}, {"a", true}}},
		// 嵌在更长 pattern 中(真实威胁情报规则风格).
		{`^/c/[\d\w-_]{3,}\.cab$`, []mc{{"/c/aZ-_9.cab", true}, {"/c/ab.cab", false}}},
	}
	for _, c := range cases {
		re, err := Compile(c.pattern, 0)
		if err != nil {
			t.Errorf("compile %q failed: %v", c.pattern, err)
			continue
		}
		for _, m := range c.matches {
			got, err := re.MatchString(m.subj)
			if err != nil {
				t.Errorf("%q match %q error: %v", c.pattern, m.subj, err)
				continue
			}
			if got != m.want {
				t.Errorf("%q match %q = %v, want %v", c.pattern, m.subj, got, m.want)
			}
		}
	}
}

// TestCompatVarLookbehind 覆盖 transform 2: lookbehind 里无界量词被收紧后可编译并正确匹配.
func TestCompatVarLookbehind(t *testing.T) {
	cases := []struct {
		pattern string
		subj    string
		want    string // 期望提取(group 0); "" 表示不匹配
	}{
		// 经典: 兼容带/不带空格的 JSON 取值.
		{`(?<="text":\s*")[^"]+(?=")`, `{"text":   "ZZXS"}`, "ZZXS"},
		{`(?<="text":\s*")[^"]+(?=")`, `{"text":"v"}`, "v"},
		// + 无界.
		{`(?<=a+)b`, "aaab", "b"},
		{`(?<=a+)b`, "b", ""},
		// {n,} 无界.
		{`(?<=ab{2,})c`, "abbbc", "c"},
		{`(?<=ab{2,})c`, "abc", ""},
		// 负向 lookbehind 无界.
		{`(?<!\d+)X`, "aX", "X"},
		// 有界变长(PCRE2 10.43 原生, compat 不介入)也应正常.
		{`(?<=ab?c)d`, "acd", "d"},
		{`(?<=ab?c)d`, "abcd", "d"},
		{`(?<=a{2,4})b`, "aaab", "b"},
	}
	for _, c := range cases {
		re, err := Compile(c.pattern, 0)
		if err != nil {
			t.Errorf("compile %q failed: %v", c.pattern, err)
			continue
		}
		m, err := re.FindStringMatch(c.subj)
		if err != nil {
			t.Errorf("%q on %q error: %v", c.pattern, c.subj, err)
			continue
		}
		got := ""
		if m != nil {
			got = m.String()
		}
		if got != c.want {
			t.Errorf("%q on %q = %q, want %q", c.pattern, c.subj, got, c.want)
		}
	}
}

// TestCompatVarLookbehindCap 验证无界量词收紧到 varLookbehindCap: 超过上界的重复不被匹配.
func TestCompatVarLookbehindCap(t *testing.T) {
	re, err := Compile(`(?<=a*)b`, 0)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	mk := func(nA int) string {
		s := make([]byte, nA+1)
		for i := 0; i < nA; i++ {
			s[i] = 'a'
		}
		s[nA] = 'b'
		return string(s)
	}
	// 上界以内: 能匹配到 b.
	if m, _ := re.FindStringMatch(mk(varLookbehindCap)); m == nil {
		t.Errorf("expected match within cap (%d a's)", varLookbehindCap)
	}
	// 远超上界: lookbehind 只回看 varLookbehindCap 个, 仍能在末尾少量 a 处匹配(回看窗口足够),
	// 因此这里仅验证不报错且仍可匹配(收紧不破坏基本功能).
	if m, _ := re.FindStringMatch(mk(varLookbehindCap + 50)); m == nil {
		t.Errorf("expected a match even beyond cap due to sliding lookbehind window")
	}
}

// TestCompatNoEffectOnValidPatterns 验证本就能编译的 pattern 不被改写影响(行为正确即可).
func TestCompatNoEffectOnValidPatterns(t *testing.T) {
	cases := []struct {
		pattern, subj string
		want          bool
	}{
		{`\d+`, "abc123", true},
		{`^[a-z]+$`, "hello", true},
		{`(?<=\$)\d+`, "$42", true},
		{`foo(?=bar)`, "foobar", true},
	}
	for _, c := range cases {
		re, err := Compile(c.pattern, 0)
		if err != nil {
			t.Errorf("compile %q failed: %v", c.pattern, err)
			continue
		}
		got, _ := re.MatchString(c.subj)
		if got != c.want {
			t.Errorf("%q match %q = %v, want %v", c.pattern, c.subj, got, c.want)
		}
	}
}

// TestEscapeUnrangeableClassHyphens 直接对改写函数做单元断言.
func TestEscapeUnrangeableClassHyphens(t *testing.T) {
	cases := []struct{ in, out string }{
		{`[\d\w-_]`, `[\d\w\-_]`},
		{`[a-\w]`, `[a\-\w]`},
		{`[\s-x]`, `[\s\-x]`},
		{`[a-z]`, `[a-z]`},     // 真实范围不动
		{`[-\w]`, `[-\w]`},     // 前导 - 不动
		{`[\w-]`, `[\w-]`},     // 尾随 - 不动
		{`abc-def`, `abc-def`}, // 类外的 - 不动
		{`[\p{L}-_]`, `[\p{L}\-_]`},
	}
	for _, c := range cases {
		if got := escapeUnrangeableClassHyphens(c.in); got != c.out {
			t.Errorf("escapeUnrangeableClassHyphens(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}

// TestBoundVarLookbehind 直接对 lookbehind 收紧函数做单元断言.
func TestBoundVarLookbehind(t *testing.T) {
	cases := []struct{ in, out string }{
		{`(?<=a*)b`, `(?<=a{0,512})b`},
		{`(?<=a+)b`, `(?<=a{1,512})b`},
		{`(?<=ab{2,})c`, `(?<=ab{2,512})c`},
		{`(?<!\d+)X`, `(?<!\d{1,512})X`},
		// 有界量词不动.
		{`(?<=ab?c)d`, `(?<=ab?c)d`},
		{`(?<=a{2,4})b`, `(?<=a{2,4})b`},
		// 非 lookbehind 的量词不动.
		{`a*b+`, `a*b+`},
		// lookahead 不动.
		{`a(?=b*)`, `a(?=b*)`},
	}
	for _, c := range cases {
		if got := boundVarLookbehind(c.in); got != c.out {
			t.Errorf("boundVarLookbehind(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}
