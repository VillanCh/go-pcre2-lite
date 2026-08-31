package regexp2

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	lib "github.com/VillanCh/go-pcre2-lite/internal/pcre2lite"
)

// ECMAScript evaluates the body of a lookbehind from right to left. PCRE2
// accepts most of the same syntax, but evaluates captures, alternatives, and
// backreferences with its own left-to-right lookbehind rules. The difference
// is observable even when both engines accept the same language.
//
// We keep PCRE2 as the only matching engine. Each JavaScript lookbehind is
// replaced by an explicit PCRE2 callout and its body is compiled as an anchored
// regex over the rune-reversed subject at the mirrored assertion position.
// Empty captures before the callout
// reserve the original group numbers; the callback writes the reverse match's
// capture spans into PCRE2's live frame, so later native backreferences and the
// final result see the JavaScript-compatible values.

type ecmaBackendCapture struct {
	number int
	start  int
	name   string
}

type ecmaLookbehindPlan struct {
	number       int
	negative     bool
	reverse      string
	internal     []int
	external     []int
	externalMark map[int]string
	compile      lib.CompileOptions
	static       *lib.Regexp
}

func compileECMAScriptLookbehinds(expr string, co lib.CompileOptions) (string, []*ecmaLookbehindPlan, error) {
	captures := collectBackendCaptures(expr)
	if !strings.Contains(expr, "(?<") {
		return expr, nil, nil
	}

	var out strings.Builder
	out.Grow(len(expr))
	var plans []*ecmaLookbehindPlan
	for i := 0; i < len(expr); {
		switch {
		case expr[i] == '\\':
			end := skipBackendEscape(expr, i)
			out.WriteString(expr[i:end])
			i = end
		case expr[i] == '[':
			end := skipClass(expr, i)
			out.WriteString(expr[i:end])
			i = end
		case i+3 < len(expr) && expr[i:i+3] == "(?<" && (expr[i+3] == '=' || expr[i+3] == '!'):
			close := findMatchingParen(expr, i)
			if close < 0 {
				return "", nil, fmt.Errorf("unterminated ECMAScript lookbehind at byte %d", i)
			}
			if len(plans) == 255 {
				return "", nil, fmt.Errorf("too many ECMAScript lookbehind assertions")
			}
			inside := capturesInRange(captures, i+4, close)
			internalSet := make(map[int]bool, len(inside))
			for _, capture := range inside {
				internalSet[capture.number] = true
			}
			reverse, external, marks, err := reverseECMAScriptExpression(expr[i+4:close], i+4, captures, internalSet)
			if err != nil {
				return "", nil, fmt.Errorf("ECMAScript lookbehind at byte %d: %w", i, err)
			}
			plan := &ecmaLookbehindPlan{
				number:       len(plans) + 1,
				negative:     expr[i+3] == '!',
				reverse:      `(?:` + reverse + `)`,
				external:     external,
				externalMark: marks,
				compile:      co,
			}
			for _, capture := range inside {
				plan.internal = append(plan.internal, capture.number)
				if capture.name != "" {
					out.WriteString("(?<")
					out.WriteString(capture.name)
					out.WriteString(">)")
				} else {
					out.WriteString("()")
				}
			}
			if len(plan.external) == 0 {
				plan.static, err = lib.Compile(plan.reverse, co)
				if err != nil {
					return "", nil, fmt.Errorf("compile reversed ECMAScript lookbehind %q: %w", plan.reverse, err)
				}
			}
			plans = append(plans, plan)
			out.WriteString("(?C")
			out.WriteString(strconv.Itoa(plan.number))
			out.WriteByte(')')
			i = close + 1
		default:
			out.WriteByte(expr[i])
			i++
		}
	}
	if len(plans) > 0 {
		// A callout is semantically constraining even though it consumes no
		// characters. PCRE2's auto-possess pass cannot see that constraint and
		// may otherwise turn the atom before it possessive, preventing the
		// backtracking required by JavaScript (for example o?(?<=...)).
		return "(*NO_AUTO_POSSESS)" + out.String(), plans, nil
	}
	return out.String(), plans, nil
}

func collectBackendCaptures(expr string) map[int]ecmaBackendCapture {
	captures := make(map[int]ecmaBackendCapture)
	number := 0
	for i := 0; i < len(expr); {
		switch expr[i] {
		case '\\':
			i = skipBackendEscape(expr, i)
		case '[':
			i = skipClass(expr, i)
		case '(':
			if i+1 >= len(expr) || expr[i+1] != '?' {
				number++
				captures[i] = ecmaBackendCapture{number: number, start: i}
				i++
				continue
			}
			if i+3 < len(expr) && expr[i+2] == '<' && expr[i+3] != '=' && expr[i+3] != '!' {
				if rel := strings.IndexByte(expr[i+3:], '>'); rel >= 0 {
					end := i + 3 + rel
					number++
					captures[i] = ecmaBackendCapture{number: number, start: i, name: expr[i+3 : end]}
					i = end + 1
					continue
				}
			}
			i++
		default:
			i++
		}
	}
	return captures
}

func capturesInRange(captures map[int]ecmaBackendCapture, start, end int) []ecmaBackendCapture {
	out := make([]ecmaBackendCapture, 0)
	for _, capture := range captures {
		if capture.start >= start && capture.start < end {
			out = append(out, capture)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].number < out[j].number })
	return out
}

type ecmaReverseAtom struct {
	value      string
	quantifier string
}

func reverseECMAScriptExpression(expr string, base int, captures map[int]ecmaBackendCapture, internal map[int]bool) (string, []int, map[int]string, error) {
	branches, err := splitECMAScriptBranches(expr)
	if err != nil {
		return "", nil, nil, err
	}
	externalSet := make(map[int]bool)
	marks := make(map[int]string)
	reversed := make([]string, 0, len(branches))
	for _, branch := range branches {
		value, err := reverseECMAScriptSequence(expr[branch[0]:branch[1]], base+branch[0], captures, internal, externalSet, marks)
		if err != nil {
			return "", nil, nil, err
		}
		reversed = append(reversed, value)
	}
	external := make([]int, 0, len(externalSet))
	for number := range externalSet {
		external = append(external, number)
	}
	sort.Ints(external)
	return strings.Join(reversed, "|"), external, marks, nil
}

func reverseECMAScriptSequence(expr string, base int, captures map[int]ecmaBackendCapture, internal, externalSet map[int]bool, marks map[int]string) (string, error) {
	atoms := make([]ecmaReverseAtom, 0, len(expr))
	for i := 0; i < len(expr); {
		var atom string
		switch expr[i] {
		case '\\':
			end := skipBackendEscape(expr, i)
			token := expr[i:end]
			if number, ok := backendBackreferenceNumber(token); ok {
				if internal[number] {
					atom = `\k<i` + strconv.Itoa(number) + `>`
				} else {
					mark := "(?P2L_EXTERNAL_" + strconv.Itoa(number) + ")"
					atom = mark
					externalSet[number] = true
					marks[number] = mark
				}
			} else {
				atom = token
			}
			i = end
		case '[':
			end := skipClass(expr, i)
			atom = expr[i:end]
			i = end
		case '(':
			close := findMatchingParen(expr, i)
			if close < 0 {
				return "", fmt.Errorf("unterminated group at byte %d", base+i)
			}
			prefix, bodyStart, captureNumber, kind, err := parseBackendGroup(expr, i, base, captures)
			if err != nil {
				return "", err
			}
			body, nestedExternal, nestedMarks, err := reverseECMAScriptExpression(expr[bodyStart:close], base+bodyStart, captures, internal)
			if err != nil {
				return "", err
			}
			for _, number := range nestedExternal {
				externalSet[number] = true
				marks[number] = nestedMarks[number]
			}
			switch kind {
			case "capture":
				atom = "(?<i" + strconv.Itoa(captureNumber) + ">" + body + ")"
			case "ahead":
				atom = "(?<=" + body + ")"
			case "ahead-not":
				atom = "(?<!" + body + ")"
			case "behind":
				atom = "(?=" + body + ")"
			case "behind-not":
				atom = "(?!" + body + ")"
			default:
				atom = prefix + body + ")"
			}
			i = close + 1
		case '^':
			atom = `$`
			i++
		case '$':
			atom = `^`
			i++
		default:
			_, size := utf8.DecodeRuneInString(expr[i:])
			if size == 0 {
				size = 1
			}
			atom = expr[i : i+size]
			i += size
		}

		quantifier, next := parseECMAScriptQuantifier(expr, i)
		atoms = append(atoms, ecmaReverseAtom{value: atom, quantifier: quantifier})
		i = next
	}
	var out strings.Builder
	for i := len(atoms) - 1; i >= 0; i-- {
		out.WriteString(atoms[i].value)
		out.WriteString(atoms[i].quantifier)
	}
	return out.String(), nil
}

func splitECMAScriptBranches(expr string) ([][2]int, error) {
	var branches [][2]int
	start := 0
	for i := 0; i < len(expr); {
		switch expr[i] {
		case '\\':
			i = skipBackendEscape(expr, i)
		case '[':
			i = skipClass(expr, i)
		case '(':
			close := findMatchingParen(expr, i)
			if close < 0 {
				return nil, fmt.Errorf("unterminated group at byte %d", i)
			}
			i = close + 1
		case '|':
			branches = append(branches, [2]int{start, i})
			start = i + 1
			i++
		default:
			i++
		}
	}
	branches = append(branches, [2]int{start, len(expr)})
	return branches, nil
}

func parseBackendGroup(expr string, start, base int, captures map[int]ecmaBackendCapture) (prefix string, bodyStart, captureNumber int, kind string, err error) {
	if capture, ok := captures[base+start]; ok {
		if expr[start+1] != '?' {
			return "(", start + 1, capture.number, "capture", nil
		}
		end := strings.IndexByte(expr[start+3:], '>')
		if end < 0 {
			return "", 0, 0, "", fmt.Errorf("unterminated named group at byte %d", base+start)
		}
		return expr[start : start+3+end+1], start + 3 + end + 1, capture.number, "capture", nil
	}
	if start+2 >= len(expr) || expr[start+1] != '?' {
		return "", 0, 0, "", fmt.Errorf("unsupported group at byte %d", base+start)
	}
	switch expr[start+2] {
	case ':':
		return "(?:", start + 3, 0, "group", nil
	case '=':
		return "(?=", start + 3, 0, "ahead", nil
	case '!':
		return "(?!", start + 3, 0, "ahead-not", nil
	case '<':
		if start+3 < len(expr) && expr[start+3] == '=' {
			return "(?<=", start + 4, 0, "behind", nil
		}
		if start+3 < len(expr) && expr[start+3] == '!' {
			return "(?<!", start + 4, 0, "behind-not", nil
		}
	}
	if colon := strings.IndexByte(expr[start+2:], ':'); colon >= 0 {
		colon += start + 2
		if close := strings.IndexByte(expr[start+2:colon], ')'); close < 0 {
			return expr[start : colon+1], colon + 1, 0, "group", nil
		}
	}
	return "", 0, 0, "", fmt.Errorf("unsupported group prefix at byte %d", base+start)
}

func backendBackreferenceNumber(token string) (int, bool) {
	if len(token) >= 2 && token[0] == '\\' && token[1] >= '1' && token[1] <= '9' {
		n, err := strconv.Atoi(token[1:])
		return n, err == nil
	}
	if strings.HasPrefix(token, `\k<g`) && strings.HasSuffix(token, ">") {
		n, err := strconv.Atoi(token[4 : len(token)-1])
		return n, err == nil
	}
	return 0, false
}

func skipBackendEscape(expr string, start int) int {
	if start+1 >= len(expr) {
		return len(expr)
	}
	if expr[start+1] >= '1' && expr[start+1] <= '9' {
		i := start + 2
		for i < len(expr) && expr[i] >= '0' && expr[i] <= '9' {
			i++
		}
		return i
	}
	if (expr[start+1] == 'k' || expr[start+1] == 'p' || expr[start+1] == 'P') && start+2 < len(expr) && (expr[start+2] == '<' || expr[start+2] == '{') {
		endChar := byte('>')
		if expr[start+2] == '{' {
			endChar = '}'
		}
		if rel := strings.IndexByte(expr[start+3:], endChar); rel >= 0 {
			return start + 3 + rel + 1
		}
	}
	if (expr[start+1] == 'x' || expr[start+1] == 'u') && start+2 < len(expr) && expr[start+2] == '{' {
		if rel := strings.IndexByte(expr[start+3:], '}'); rel >= 0 {
			return start + 3 + rel + 1
		}
	}
	return start + 2
}

func parseECMAScriptQuantifier(expr string, start int) (string, int) {
	if start >= len(expr) {
		return "", start
	}
	end := start
	switch expr[start] {
	case '*', '+', '?':
		end++
	case '{':
		_, _, after, ok := parseBrace(expr, start)
		if !ok {
			return "", start
		}
		end = after
	default:
		return "", start
	}
	if end < len(expr) && expr[end] == '?' {
		end++
	}
	return expr[start:end], end
}

func (plan *ecmaLookbehindPlan) evaluate(block *lib.CalloutBlock, reverseSubject []byte) (int, error) {
	if block.CurrentPosition < 0 || block.CurrentPosition > len(block.Subject) {
		return 1, nil
	}
	reverseStart := len(block.Subject) - block.CurrentPosition
	sub := plan.static
	var dynamic *lib.Regexp
	if len(plan.external) > 0 {
		pattern := plan.reverse
		for _, number := range plan.external {
			literal := ""
			if span, ok := block.Capture(number); ok && span.Start >= 0 && span.End <= len(block.Subject) {
				literal = quotePCRE2Literal(reverseUTF8Bytes(block.Subject[span.Start:span.End]))
			}
			pattern = strings.ReplaceAll(pattern, plan.externalMark[number], literal)
		}
		var err error
		dynamic, err = lib.Compile(pattern, plan.compile)
		if err != nil {
			return 1, err
		}
		defer dynamic.Close()
		sub = dynamic
	}
	match, err := sub.FindFrom(reverseSubject, reverseStart, lib.MatchAnchored)
	if err != nil {
		return 1, err
	}
	if plan.negative {
		if match != nil {
			return 1, nil
		}
		for _, number := range plan.internal {
			block.SetCapture(number, lib.SpanUnset, lib.SpanUnset)
		}
		return 0, nil
	}
	if match == nil {
		return 1, nil
	}
	for _, number := range plan.internal {
		local, ok := sub.NamedGroupNumber("i" + strconv.Itoa(number))
		if !ok || local >= len(match.Groups) || match.Groups[local].IsUnset() {
			block.SetCapture(number, lib.SpanUnset, lib.SpanUnset)
			continue
		}
		span := match.Groups[local]
		block.SetCapture(number, len(block.Subject)-span.End, len(block.Subject)-span.Start)
	}
	return 0, nil
}

func reverseUTF8Bytes(value []byte) []byte {
	if len(value) == 0 {
		return nil
	}
	runes := []rune(string(value))
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return []byte(string(runes))
}

func quotePCRE2Literal(value []byte) string {
	var out strings.Builder
	for _, r := range string(value) {
		out.WriteString(`\x{`)
		out.WriteString(strconv.FormatInt(int64(r), 16))
		out.WriteByte('}')
	}
	return out.String()
}
