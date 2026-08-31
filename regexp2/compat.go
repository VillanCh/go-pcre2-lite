package regexp2

import (
	"fmt"
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
//  2. lookbehind 里的无界量词. 例如 (?<="text":\s*"): \s* 长度无上界. PCRE2 10.47 已原生支持
//     "有界变长 lookbehind"(各分支长度有上限即可), 因此有界量词(? {n,m})无需改写; 只需把无界
//     量词收紧成有上界的形式: * -> {0,N}, + -> {1,N}, {n,} -> {n,N}. 这是一个兜底近似(超过 N
//     次重复将不被匹配), 与 .NET 的无上限语义存在差异, 但覆盖绝大多数真实场景. 配合 wrapper.c
//     里把 max_varlookbehind 调高, 收紧后的 lookbehind 即可编译.

// varLookbehindCap 是无界量词(* + {n,})在 lookbehind 内被收紧到的重复次数上界.
const varLookbehindCap = 512

// ecmaWhitespaceClassBody is the exact ECMAScript WhiteSpace + LineTerminator
// set used by the \s and \S character class escapes. In particular it includes
// the Unicode space separators and BOM, while excluding U+0085 and U+200B.
const ecmaWhitespaceClassBody = `\x09-\x0d\x20\x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

func validateECMAScriptUnicodeEscapes(expr string) error {
	for i := 0; i < len(expr); {
		if expr[i] != '\\' {
			i++
			continue
		}
		if i+1 >= len(expr) {
			return fmt.Errorf("invalid trailing escape at byte %d", i)
		}
		switch expr[i+1] {
		case 'x':
			if i+4 > len(expr) || !isHexBytes(expr[i+2:i+4]) {
				return fmt.Errorf("invalid ECMAScript hex escape at byte %d", i)
			}
			i += 4
		case 'u':
			if i+2 < len(expr) && expr[i+2] == '{' {
				relEnd := strings.IndexByte(expr[i+3:], '}')
				if relEnd < 0 {
					return fmt.Errorf("unterminated ECMAScript Unicode escape at byte %d", i)
				}
				end := i + 3 + relEnd
				digits := expr[i+3 : end]
				if len(digits) < 1 || len(digits) > 6 || !isHexBytes(digits) {
					return fmt.Errorf("invalid ECMAScript Unicode escape at byte %d", i)
				}
				value, _ := strconv.ParseUint(digits, 16, 32)
				if value > 0x10ffff {
					return fmt.Errorf("ECMAScript Unicode escape is out of range at byte %d", i)
				}
				if isUTF16Surrogate(value) {
					return fmt.Errorf("%w in pattern at byte %d", ErrUnsupportedRune, i)
				}
				i = end + 1
				continue
			}
			if i+6 > len(expr) || !isHexBytes(expr[i+2:i+6]) {
				return fmt.Errorf("invalid ECMAScript Unicode escape at byte %d", i)
			}
			value, _ := strconv.ParseUint(expr[i+2:i+6], 16, 16)
			if value >= 0xd800 && value <= 0xdbff && i+12 <= len(expr) &&
				expr[i+6:i+8] == `\u` && isHexBytes(expr[i+8:i+12]) {
				low, _ := strconv.ParseUint(expr[i+8:i+12], 16, 16)
				if low >= 0xdc00 && low <= 0xdfff {
					i += 12
					continue
				}
			}
			if isUTF16Surrogate(value) {
				return fmt.Errorf("%w in pattern at byte %d", ErrUnsupportedRune, i)
			}
			i += 6
		default:
			i += 2
		}
	}
	return nil
}

// rewriteECMAScriptPattern translates the small set of ECMAScript atoms whose
// semantics differ from PCRE2. Goja normally performs the same translation for
// its RE2 fast path, but sends the original expression to its fallback engine
// as soon as it sees a lookaround, backreference, or another RE2-incompatible
// construct. Keeping the translation here makes that fallback self-contained.
func rewriteECMAScriptPattern(expr string, dotAll, unicode bool) (string, bool) {
	var out strings.Builder
	out.Grow(len(expr))
	changed := false
	captureCount := countECMAScriptCaptures(expr)

	for i := 0; i < len(expr); {
		switch expr[i] {
		case '\\':
			switch {
			case i+1 >= len(expr):
				out.WriteByte(expr[i])
				i++
				continue
			case expr[i+1] == 's':
				out.WriteByte('[')
				out.WriteString(ecmaWhitespaceClassBody)
				out.WriteByte(']')
				changed = true
				i += 2
			case expr[i+1] == 'S':
				out.WriteString(`[^`)
				out.WriteString(ecmaWhitespaceClassBody)
				out.WriteByte(']')
				changed = true
				i += 2
			default:
				replacement, next, escapeChanged := rewriteECMAScriptEscape(expr, i, unicode, captureCount, false)
				out.WriteString(replacement)
				changed = changed || escapeChanged
				i = next
			}
		case '[':
			end := findECMAScriptClassEnd(expr, i)
			if end < 0 {
				out.WriteString(expr[i:])
				i = len(expr)
				continue
			}
			bodyStart := i + 1
			negated := false
			if bodyStart < end && expr[bodyStart] == '^' {
				negated = true
				bodyStart++
			}
			body := expr[bodyStart:end]
			if body == "" {
				if negated {
					out.WriteString(`(?s:.)`)
				} else {
					out.WriteString(`(?!)`)
				}
				changed = true
			} else {
				rewritten, classChanged := rewriteECMAScriptClass(body, negated, unicode)
				out.WriteString(rewritten)
				changed = changed || classChanged
			}
			i = end + 1
		case '.':
			if dotAll {
				out.WriteByte('.')
			} else {
				out.WriteString(`[^\r\n\x{2028}\x{2029}]`)
				changed = true
			}
			i++
		default:
			out.WriteByte(expr[i])
			i++
		}
	}
	if !changed {
		return expr, false
	}
	return out.String(), true
}

// countECMAScriptCaptures counts capturing parentheses without attempting to
// validate the entire expression. The total is needed before rewriting legacy
// decimal escapes because JavaScript permits forward backreferences, while a
// decimal escape larger than the final capture count has Annex B octal or
// identity-escape semantics in non-Unicode mode.
func countECMAScriptCaptures(expr string) int {
	count := 0
	for i := 0; i < len(expr); {
		switch expr[i] {
		case '\\':
			i += 2
		case '[':
			i = findECMAScriptClassEnd(expr, i)
			if i < 0 {
				return count
			}
			i++
		case '(':
			if i+1 >= len(expr) || expr[i+1] != '?' {
				count++
			} else if i+2 < len(expr) && expr[i+2] == '<' &&
				(i+3 >= len(expr) || (expr[i+3] != '=' && expr[i+3] != '!')) {
				count++
			}
			i++
		default:
			i++
		}
	}
	return count
}

// rewriteECMAScriptEscape translates JavaScript's Unicode escape spelling to
// PCRE2's spelling. In legacy (non-u) mode an invalid \x or \u starts an
// identity escape, so the backslash is discarded and the following bytes are
// parsed normally. Lone UTF-16 surrogates intentionally remain unsupported:
// PCRE2's 8-bit UTF mode cannot represent those code units, so callers that
// need exact JavaScript semantics must fall back to a UTF-16-aware engine.
func rewriteECMAScriptEscape(expr string, start int, unicode bool, captureCount int, inClass bool) (string, int, bool) {
	if start+1 >= len(expr) {
		return expr[start:], len(expr), false
	}
	switch expr[start+1] {
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		end := start + 2
		for end < len(expr) && expr[end] >= '0' && expr[end] <= '9' {
			end++
		}
		decimal, err := strconv.Atoi(expr[start+1 : end])
		if !inClass && err == nil && decimal <= captureCount {
			return expr[start:end], end, false
		}
		if unicode {
			// Leave invalid Unicode-mode escapes untouched so PCRE2 rejects
			// them instead of silently changing the expression.
			return expr[start:end], end, false
		}
		first := expr[start+1]
		if first == '8' || first == '9' {
			// NonOctalDecimalEscape: \8 and \9 are identity escapes.
			return string(first), start + 2, true
		}
		maxDigits := 2
		if first <= '3' {
			maxDigits = 3
		}
		octalEnd := start + 1
		for octalEnd < end && octalEnd < start+1+maxDigits &&
			expr[octalEnd] >= '0' && expr[octalEnd] <= '7' {
			octalEnd++
		}
		value, _ := strconv.ParseUint(expr[start+1:octalEnd], 8, 8)
		return `\x{` + strconv.FormatUint(value, 16) + `}`, octalEnd, true
	case 'x':
		if start+4 <= len(expr) && isHexBytes(expr[start+2:start+4]) {
			return expr[start : start+4], start + 4, false
		}
		if !unicode {
			return "x", start + 2, true
		}
	case 'u':
		if unicode && start+2 < len(expr) && expr[start+2] == '{' {
			if end := strings.IndexByte(expr[start+3:], '}'); end >= 0 {
				end += start + 3
				digits := expr[start+3 : end]
				if len(digits) >= 1 && len(digits) <= 6 && isHexBytes(digits) {
					value, _ := strconv.ParseUint(digits, 16, 32)
					if value <= 0x10ffff && !isUTF16Surrogate(value) {
						return `\x{` + digits + `}`, end + 1, true
					}
				}
			}
			return expr[start : start+2], start + 2, false
		}
		if !unicode && start+2 < len(expr) && expr[start+2] == '{' {
			return "u", start + 2, true
		}
		if start+6 <= len(expr) && isHexBytes(expr[start+2:start+6]) {
			value, _ := strconv.ParseUint(expr[start+2:start+6], 16, 16)
			if value >= 0xd800 && value <= 0xdbff && unicode &&
				start+12 <= len(expr) && expr[start+6:start+8] == `\u` &&
				isHexBytes(expr[start+8:start+12]) {
				low, _ := strconv.ParseUint(expr[start+8:start+12], 16, 16)
				if low >= 0xdc00 && low <= 0xdfff {
					codePoint := 0x10000 + ((value - 0xd800) << 10) + (low - 0xdc00)
					return `\x{` + strconv.FormatUint(codePoint, 16) + `}`, start + 12, true
				}
			}
			if !isUTF16Surrogate(value) {
				return `\x{` + expr[start+2:start+6] + `}`, start + 6, true
			}
			return expr[start : start+6], start + 6, false
		}
		if !unicode {
			return "u", start + 2, true
		}
	}
	return expr[start : start+2], start + 2, false
}

func isHexBytes(s string) bool {
	for i := 0; i < len(s); i++ {
		if !((s[i] >= '0' && s[i] <= '9') ||
			(s[i] >= 'a' && s[i] <= 'f') ||
			(s[i] >= 'A' && s[i] <= 'F')) {
			return false
		}
	}
	return true
}

func isUTF16Surrogate(value uint64) bool {
	return value >= 0xd800 && value <= 0xdfff
}

func findECMAScriptClassEnd(expr string, start int) int {
	for i := start + 1; i < len(expr); i++ {
		if expr[i] == '\\' {
			i++
			continue
		}
		if expr[i] == ']' {
			return i
		}
	}
	return -1
}

// rewriteECMAScriptClass handles \s and \S inside a character class. A \S
// mixed with other members cannot be represented by one traditional PCRE2
// class, so it is expressed as a non-capturing alternation (or, for a negated
// class, a one-character lookahead plus a consuming class).
func rewriteECMAScriptClass(body string, negated, unicode bool) (string, bool) {
	var members strings.Builder
	members.Grow(len(body))
	hasSpace, hasNonSpace, changed := false, false, false
	prevWasSet := false
	for i := 0; i < len(body); {
		if body[i] == '\\' && i+1 < len(body) {
			switch body[i+1] {
			case 's':
				members.WriteString(ecmaWhitespaceClassBody)
				hasSpace = true
				prevWasSet = true
				i += 2
				continue
			case 'S':
				hasNonSpace = true
				prevWasSet = true
				i += 2
				continue
			default:
				replacement, next, escapeChanged := rewriteECMAScriptEscape(body, i, unicode, 0, true)
				members.WriteString(replacement)
				changed = changed || escapeChanged
				prevWasSet = isSetEscapeLetter(body[i+1])
				i = next
				continue
			}
		}
		if body[i] == '-' {
			nextIsSet := i+2 < len(body) && body[i+1] == '\\' && isSetEscapeLetter(body[i+2])
			if prevWasSet || nextIsSet {
				members.WriteString(`\-`)
				changed = true
			} else {
				members.WriteByte('-')
			}
			prevWasSet = false
			i++
			continue
		}
		members.WriteByte(body[i])
		prevWasSet = false
		i++
	}

	if !hasSpace && !hasNonSpace && !changed {
		prefix := "["
		if negated {
			prefix = "[^"
		}
		return prefix + body + "]", false
	}
	other := members.String()
	if !hasNonSpace {
		prefix := "["
		if negated {
			prefix = "[^"
		}
		return prefix + other + "]", true
	}
	if hasSpace {
		// \s and \S together cover the entire character domain.
		if negated {
			return `(?!)`, true
		}
		return `(?s:.)`, true
	}
	if !negated {
		if other == "" {
			return `[^` + ecmaWhitespaceClassBody + `]`, true
		}
		return `(?:[` + other + `]|[^` + ecmaWhitespaceClassBody + `])`, true
	}
	if other == "" {
		return `[` + ecmaWhitespaceClassBody + `]`, true
	}
	return `(?:(?=[` + ecmaWhitespaceClassBody + `])[^` + other + `])`, true
}

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
// ({0,N} {1,N} {n,N}, N=varLookbehindCap). 有界量词(? {n,m} {n})原样保留(PCRE2 10.47 原生
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
