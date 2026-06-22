package regexp2

import (
	"strings"
	"unicode"
)

// MatchEvaluator is a function that takes a match and returns its replacement.
type MatchEvaluator func(Match) string

// Replace replaces occurrences of the regex in input with the replacement
// pattern. count limits the number of replacements (-1 for all); startAt is a
// byte offset into input from which to begin (-1 for the whole string).
//
// Supported replacement tokens: $1, ${name}, $$, $&, $`, $', $+, $_.
func (re *Regexp) Replace(input, replacement string, startAt, count int) (string, error) {
	toks := re.parseReplacement(replacement)
	return re.replace(func(m *Match) string { return applyTokens(toks, m) }, input, startAt, count)
}

// ReplaceFunc replaces occurrences using the evaluator function.
func (re *Regexp) ReplaceFunc(input string, evaluator MatchEvaluator, startAt, count int) (string, error) {
	return re.replace(func(m *Match) string { return evaluator(*m) }, input, startAt, count)
}

func (re *Regexp) replace(eval func(*Match) string, input string, startAt, count int) (string, error) {
	if count < -1 {
		return "", errInvalidCount
	}
	if count == 0 {
		return "", nil
	}

	m, err := re.FindStringMatchStartingAt(input, startAt)
	if err != nil {
		return "", err
	}
	if m == nil {
		return input, nil
	}

	var buf strings.Builder
	text := m.state.text

	// Left-to-right assembly. RightToLeft is not faithfully supported; see
	// MIGRATION.md.
	prevat := 0
	for m != nil {
		if m.Index != prevat {
			buf.WriteString(string(text[prevat:m.Index]))
		}
		prevat = m.Index + m.Length
		buf.WriteString(eval(m))

		count--
		if count == 0 {
			break
		}
		m, err = re.FindNextMatch(m)
		if err != nil {
			return "", err
		}
	}
	if prevat < len(text) {
		buf.WriteString(string(text[prevat:]))
	}
	return buf.String(), nil
}

// replacement tokens
const (
	repLiteral = iota
	repGroup
	repLeft  // $`
	repRight // $'
	repLast  // $+
	repWhole // $_
)

type repToken struct {
	kind int
	str  string
	num  int
}

func (re *Regexp) isSlot(n int) bool {
	return n >= 0 && n < re.re.CaptureCount()
}

func (re *Regexp) parseReplacement(rep string) []repToken {
	runes := []rune(rep)
	n := len(runes)
	var toks []repToken
	var lit []rune
	flush := func() {
		if len(lit) > 0 {
			toks = append(toks, repToken{kind: repLiteral, str: string(lit)})
			lit = lit[:0]
		}
	}

	i := 0
	for i < n {
		c := runes[i]
		if c != '$' {
			lit = append(lit, c)
			i++
			continue
		}
		// c == '$'
		if i+1 >= n {
			lit = append(lit, '$')
			i++
			continue
		}
		j := i + 1
		ch := runes[j]
		angled := false
		if ch == '{' && j+1 < n {
			angled = true
			j++
			ch = runes[j]
		}

		switch {
		case ch >= '0' && ch <= '9':
			start := j
			num := 0
			for j < n && runes[j] >= '0' && runes[j] <= '9' {
				num = num*10 + int(runes[j]-'0')
				j++
			}
			if angled {
				if j < n && runes[j] == '}' && re.isSlot(num) {
					flush()
					toks = append(toks, repToken{kind: repGroup, num: num})
					i = j + 1
					continue
				}
				lit = append(lit, '$')
				i++
				continue
			}
			// non-angled: greedy number must be a valid slot
			if re.isSlot(num) {
				flush()
				toks = append(toks, repToken{kind: repGroup, num: num})
				i = j
				continue
			}
			_ = start
			lit = append(lit, '$')
			i++
			continue

		case angled && isWordRune(ch):
			start := j
			for j < n && isWordRune(runes[j]) {
				j++
			}
			name := string(runes[start:j])
			if j < n && runes[j] == '}' {
				if num, ok := re.re.NamedGroupNumber(name); ok {
					flush()
					toks = append(toks, repToken{kind: repGroup, num: num})
					i = j + 1
					continue
				}
			}
			lit = append(lit, '$')
			i++
			continue

		case !angled:
			switch ch {
			case '$':
				lit = append(lit, '$')
				i += 2
				continue
			case '&':
				flush()
				toks = append(toks, repToken{kind: repGroup, num: 0})
				i += 2
				continue
			case '`':
				flush()
				toks = append(toks, repToken{kind: repLeft})
				i += 2
				continue
			case '\'':
				flush()
				toks = append(toks, repToken{kind: repRight})
				i += 2
				continue
			case '+':
				flush()
				toks = append(toks, repToken{kind: repLast})
				i += 2
				continue
			case '_':
				flush()
				toks = append(toks, repToken{kind: repWhole})
				i += 2
				continue
			default:
				lit = append(lit, '$')
				i++
				continue
			}

		default:
			lit = append(lit, '$')
			i++
			continue
		}
	}
	flush()
	return toks
}

func applyTokens(toks []repToken, m *Match) string {
	var b strings.Builder
	text := m.state.text
	for _, t := range toks {
		switch t.kind {
		case repLiteral:
			b.WriteString(t.str)
		case repGroup:
			if g := m.GroupByNumber(t.num); g != nil {
				b.WriteString(g.String())
			}
		case repLeft:
			b.WriteString(string(text[:m.Index]))
		case repRight:
			b.WriteString(string(text[m.Index+m.Length:]))
		case repLast:
			if g := m.GroupByNumber(m.GroupCount() - 1); g != nil {
				b.WriteString(g.String())
			}
		case repWhole:
			b.WriteString(string(text))
		}
	}
	return b.String()
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
