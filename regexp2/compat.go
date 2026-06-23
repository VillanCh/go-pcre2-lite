package regexp2

import (
	"strconv"
	"strings"
)

// 本文件实现一组"语法兼容兜底"(syntax compatibility fallback): 某些正则在 .NET(dlclark)
// 或 RE2 下可以编译, 但在 PCRE2 下因更严格而被拒绝. 当原始 pattern 编译失败时, 这里尝试把
// 这些构造重写成 PCRE2 等价(或近似等价)的形式后再编译一次. 只有改写后能成功编译才会采用, 否则
// 返回原始错误, 因此对本就能编译的 pattern 完全无副作用.
//
// 目前覆盖两类:
//  1. 字符类里 "集合简写不能作为范围端点" 导致的 invalid range. 例如 [\d\w-_]: PCRE2 会把
//     \w-_ 当成 \w 到 _ 的范围而报错, 而 .NET/RE2 把 - 视为字面量. 改写为把该 - 转义成 \-.
//  2. lookbehind 里的无界量词. 例如 (?<="text":\s*"): \s* 长度无上界. PCRE2 10.43 已原生支持
//     "有界变长 lookbehind"(各分支长度有上限即可), 因此有界量词(? {n,m})无需改写; 只需把无界
//     量词收紧成有上界的形式: * -> {0,N}, + -> {1,N}, {n,} -> {n,N}. 这是一个兜底近似(超过 N
//     次重复将不被匹配), 与 .NET 的无上限语义存在差异, 但覆盖绝大多数真实场景. 配合 wrapper.c
//     里把 max_varlookbehind 调高, 收紧后的 lookbehind 即可编译.

// varLookbehindCap 是无界量词(* + {n,})在 lookbehind 内被收紧到的重复次数上界.
const varLookbehindCap = 512

// rewriteForPCRE2Compat 对 expr 依次应用各兼容改写, 返回改写结果与是否发生变化.
func rewriteForPCRE2Compat(expr string) (string, bool) {
	out := escapeUnrangeableClassHyphens(expr)
	out = boundVarLookbehind(out)
	return out, out != expr
}

// isSetEscapeLetter 报告 \X 是否是一个"集合"类简写(匹配多个字符), 这类简写不能作为字符类范围端点.
func isSetEscapeLetter(b byte) bool {
	switch b {
	case 'd', 'D', 'w', 'W', 's', 'S', 'h', 'H', 'v', 'V', 'p', 'P':
		return true
	}
	return false
}

// escapeUnrangeableClassHyphens 在字符类内部, 当 - 的某一端是集合简写(\d \w ...)或 POSIX 类
// (如 [:alpha:])这种不能作为范围端点的原子时, 把该 - 转义成 \-, 使其成为字面量. 这与 .NET/RE2
// 对此类 - 的处理一致, 消除 PCRE2 的 "invalid range in character class" 报错.
func escapeUnrangeableClassHyphens(expr string) string {
	n := len(expr)
	var out []byte
	i := 0
	inClass := false
	classContentStart := -1 // out 中当前字符类内容起始位置, 用于判断前导 -
	prevWasSet := false     // 类内上一个原子是否是不可作为范围端点的集合
	for i < n {
		c := expr[i]
		if !inClass {
			switch c {
			case '\\':
				out = append(out, c)
				if i+1 < n {
					out = append(out, expr[i+1])
					i += 2
				} else {
					i++
				}
			case '[':
				inClass = true
				prevWasSet = false
				out = append(out, c)
				i++
				if i < n && expr[i] == '^' {
					out = append(out, '^')
					i++
				}
				if i < n && expr[i] == ']' { // 紧跟的 ] 是字面成员
					out = append(out, ']')
					i++
				}
				classContentStart = len(out)
			default:
				out = append(out, c)
				i++
			}
			continue
		}
		// 字符类内部
		switch c {
		case '\\':
			if i+1 < n {
				nx := expr[i+1]
				if (nx == 'p' || nx == 'P') && i+2 < n && expr[i+2] == '{' {
					j := i + 3
					for j < n && expr[j] != '}' {
						j++
					}
					if j < n {
						j++ // 含 }
					}
					out = append(out, expr[i:j]...)
					prevWasSet = true
					i = j
				} else {
					out = append(out, c, nx)
					prevWasSet = isSetEscapeLetter(nx)
					i += 2
				}
			} else {
				out = append(out, c)
				i++
			}
		case '[':
			if i+1 < n && expr[i+1] == ':' { // POSIX 类 [:name:]
				j := i + 2
				for j+1 < n && !(expr[j] == ':' && expr[j+1] == ']') {
					j++
				}
				if j+1 < n {
					out = append(out, expr[i:j+2]...)
					i = j + 2
					prevWasSet = true
					continue
				}
			}
			out = append(out, c)
			prevWasSet = false
			i++
		case ']':
			inClass = false
			prevWasSet = false
			out = append(out, c)
			i++
		case '-':
			isLeading := len(out) == classContentStart
			isTrailing := i+1 < n && expr[i+1] == ']'
			if isLeading || isTrailing || i+1 >= n {
				out = append(out, c)
				prevWasSet = false
				i++
				continue
			}
			nextIsSet := false
			if expr[i+1] == '\\' && i+2 < n {
				nextIsSet = isSetEscapeLetter(expr[i+2])
			} else if expr[i+1] == '[' && i+2 < n && expr[i+2] == ':' {
				nextIsSet = true
			}
			if prevWasSet || nextIsSet {
				out = append(out, '\\', '-')
			} else {
				out = append(out, '-')
			}
			prevWasSet = false
			i++
		default:
			out = append(out, c)
			prevWasSet = false
			i++
		}
	}
	return string(out)
}

// skipClass 返回 s 中从 i(指向 '[')处字符类结束(']' 的下一个位置)的下标.
func skipClass(s string, i int) int {
	n := len(s)
	i++ // 跳过 '['
	if i < n && s[i] == '^' {
		i++
	}
	if i < n && s[i] == ']' { // 前导字面 ]
		i++
	}
	for i < n {
		switch {
		case s[i] == '\\':
			i += 2
		case s[i] == '[' && i+1 < n && s[i+1] == ':':
			j := i + 2
			for j+1 < n && !(s[j] == ':' && s[j+1] == ']') {
				j++
			}
			if j+1 < n {
				i = j + 2
			} else {
				i++
			}
		case s[i] == ']':
			return i + 1
		default:
			i++
		}
	}
	return n
}

// findMatchingParen 给定 s 中 open 处的 '(', 返回与之匹配的 ')' 下标(转义与字符类感知), 无则 -1.
func findMatchingParen(s string, open int) int {
	n := len(s)
	depth := 0
	i := open
	for i < n {
		switch s[i] {
		case '\\':
			i += 2
			continue
		case '[':
			i = skipClass(s, i)
			continue
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return -1
}

// boundVarLookbehind 把变长 lookbehind 改写为定长分支的或. 非 lookbehind 部分原样保留.
func boundVarLookbehind(expr string) string {
	if !strings.Contains(expr, "(?<") {
		return expr
	}
	n := len(expr)
	var out []byte
	i := 0
	for i < n {
		c := expr[i]
		if c == '\\' {
			out = append(out, c)
			if i+1 < n {
				out = append(out, expr[i+1])
				i += 2
			} else {
				i++
			}
			continue
		}
		if c == '[' {
			j := skipClass(expr, i)
			out = append(out, expr[i:j]...)
			i = j
			continue
		}
		if c == '(' && i+3 < n && expr[i+1] == '?' && expr[i+2] == '<' &&
			(expr[i+3] == '=' || expr[i+3] == '!') {
			closeIdx := findMatchingParen(expr, i)
			if closeIdx < 0 {
				out = append(out, c)
				i++
				continue
			}
			head := expr[i : i+4] // (?<= 或 (?<!
			body := expr[i+4 : closeIdx]
			if newBody, ok := boundUnboundedQuantifiers(body); ok {
				out = append(out, head...)
				out = append(out, newBody...)
				out = append(out, ')')
				i = closeIdx + 1
				continue
			}
			// 无需改写: 只输出 head, 让循环继续扫描 body(以处理嵌套 lookbehind).
			out = append(out, head...)
			i += 4
			continue
		}
		out = append(out, c)
		i++
	}
	return string(out)
}

// unboundedQuant 描述 lookbehind body 中一个无界量词算子的位置与下界.
type unboundedQuant struct {
	opStart int // 算子(不含惰性/独占修饰)起始下标
	opEnd   int // 算子结束下标(开区间), 惰性/独占修饰保留在其后
	min     int // 最小重复次数: * 为 0, + 为 1, {n,} 为 n
}

// boundUnboundedQuantifiers 把 lookbehind body 中的无界量词(* + {n,})收紧成有上界形式
// ({0,N} {1,N} {n,N}, N=varLookbehindCap). 有界量词(? {n,m} {n})原样保留(PCRE2 10.43 原生
// 支持有界变长 lookbehind). 返回收紧后的 body 及是否发生改写. 扫描感知转义/字符类/分组前缀.
func boundUnboundedQuantifiers(body string) (string, bool) {
	qs := findUnboundedQuantifiers(body)
	if len(qs) == 0 {
		return body, false
	}
	var b strings.Builder
	last := 0
	for _, q := range qs {
		b.WriteString(body[last:q.opStart])
		b.WriteByte('{')
		b.WriteString(strconv.Itoa(q.min))
		b.WriteByte(',')
		b.WriteString(strconv.Itoa(varLookbehindCap))
		b.WriteByte('}')
		last = q.opEnd
	}
	b.WriteString(body[last:])
	return b.String(), true
}

// findUnboundedQuantifiers 扫描 body, 返回所有无界量词(* + {n,}). 有界量词不返回.
func findUnboundedQuantifiers(body string) []unboundedQuant {
	n := len(body)
	var qs []unboundedQuant
	prevQuantifiable := false // 上一个 token 是否是可被量词修饰的原子
	i := 0
	for i < n {
		c := body[i]
		switch {
		case c == '\\':
			i += 2
			prevQuantifiable = true
		case c == '[':
			i = skipClass(body, i)
			prevQuantifiable = true
		case c == '(':
			// '(' 之后(含分组前缀 (? ... 的 ?)都不是可被量词修饰的原子
			prevQuantifiable = false
			i++
		case c == ')':
			prevQuantifiable = true
			i++
		case c == '|':
			prevQuantifiable = false
			i++
		case c == '*' || c == '+':
			if !prevQuantifiable {
				i++
				continue
			}
			min := 0
			if c == '+' {
				min = 1
			}
			qs = append(qs, unboundedQuant{opStart: i, opEnd: i + 1, min: min})
			i++
			prevQuantifiable = false
		case c == '?':
			// ? 自身是有界量词; 也可能是前一个量词的惰性修饰. 两种情况都跳过即可.
			i++
			prevQuantifiable = false
		case c == '{':
			lo, hi, after, ok := parseBrace(body, i)
			if !ok {
				i++ // 字面 {
				prevQuantifiable = true
				continue
			}
			if !prevQuantifiable {
				i = after
				prevQuantifiable = true
				continue
			}
			if hi == -1 { // {n,} 无界
				qs = append(qs, unboundedQuant{opStart: i, opEnd: after, min: lo})
			}
			i = after
			prevQuantifiable = false
		default:
			i++
			prevQuantifiable = true
		}
	}
	return qs
}

// parseBrace 解析 s 中 i 处(指向 '{')的 {n} / {n,} / {n,m} 量词. 返回 lo, hi(-1 表示无界),
// after(算子结束后的下标)与是否解析成功. 形如 {,m} 视为 {0,m}.
func parseBrace(s string, i int) (lo, hi, after int, ok bool) {
	n := len(s)
	j := i + 1
	loStart := j
	for j < n && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	hasLo := j > loStart
	if j < n && s[j] == '}' {
		if !hasLo {
			return 0, 0, 0, false
		}
		v, _ := strconv.Atoi(s[loStart:j])
		return v, v, j + 1, true
	}
	if j < n && s[j] == ',' {
		lov := 0
		if hasLo {
			lov, _ = strconv.Atoi(s[loStart:j])
		}
		j++
		hiStart := j
		for j < n && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		hasHi := j > hiStart
		if j < n && s[j] == '}' {
			if hasHi {
				hv, _ := strconv.Atoi(s[hiStart:j])
				return lov, hv, j + 1, true
			}
			return lov, -1, j + 1, true // {n,}
		}
	}
	return 0, 0, 0, false
}
