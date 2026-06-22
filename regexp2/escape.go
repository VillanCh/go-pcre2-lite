package regexp2

import (
	"errors"
	"strconv"
	"strings"
	"unicode"
)

var (
	errInvalidCount       = errors.New("Count too small")
	errIllegalEndEscape   = errors.New("illegal \\ at end of pattern")
	errUnrecognizedEscape = errors.New("unrecognized escape sequence")
	errMissingControl     = errors.New("missing control character")
	errUnrecognizedCtrl   = errors.New("unrecognized control character")
	errTooFewHex          = errors.New("insufficient hexadecimal digits")
	errMissingBrace       = errors.New("missing closing brace in \\x{...}")
	errInvalidHex         = errors.New("hex value too large")
)

const escapeMeta = `\.+*?()|[]{}^$# `

func escapeImpl(input string) string {
	var b strings.Builder
	for _, r := range input {
		escapeRune(&b, r)
	}
	return b.String()
}

func escapeRune(b *strings.Builder, r rune) {
	if unicode.IsPrint(r) {
		if strings.IndexRune(escapeMeta, r) >= 0 {
			b.WriteRune('\\')
		}
		b.WriteRune(r)
		return
	}
	switch r {
	case '\a':
		b.WriteString(`\a`)
	case '\f':
		b.WriteString(`\f`)
	case '\n':
		b.WriteString(`\n`)
	case '\r':
		b.WriteString(`\r`)
	case '\t':
		b.WriteString(`\t`)
	case '\v':
		b.WriteString(`\v`)
	default:
		if r < 0x100 {
			b.WriteString(`\x`)
			s := strconv.FormatInt(int64(r), 16)
			if len(s) == 1 {
				b.WriteRune('0')
			}
			b.WriteString(s)
			return
		}
		b.WriteString(`\u`)
		b.WriteString(strconv.FormatInt(int64(r), 16))
	}
}

// escCursor is a minimal rune cursor mirroring the subset of the regexp2
// parser used by Unescape (with all options off).
type escCursor struct {
	r   []rune
	pos int
}

func (c *escCursor) charsRight() int { return len(c.r) - c.pos }
func (c *escCursor) rightMost() bool { return c.pos == len(c.r) }
func (c *escCursor) rightChar(i int) rune {
	return c.r[c.pos+i]
}
func (c *escCursor) moveRightGetChar() rune {
	ch := c.r[c.pos]
	c.pos++
	return ch
}
func (c *escCursor) moveRight(i int) { c.pos += i }
func (c *escCursor) moveLeft()       { c.pos-- }
func (c *escCursor) textpos() int    { return c.pos }
func (c *escCursor) textto(p int)    { c.pos = p }

func unescapeImpl(input string) (string, error) {
	idx := strings.IndexRune(input, '\\')
	if idx == -1 {
		return input, nil
	}
	var buf strings.Builder
	buf.WriteString(input[:idx])

	c := &escCursor{r: []rune(input[idx+1:])}
	for {
		if c.rightMost() {
			return "", errIllegalEndEscape
		}
		r, err := c.scanCharEscape()
		if err != nil {
			return "", err
		}
		buf.WriteRune(r)
		if c.rightMost() {
			return buf.String(), nil
		}
		r = c.moveRightGetChar()
		for r != '\\' {
			buf.WriteRune(r)
			if c.rightMost() {
				return buf.String(), nil
			}
			r = c.moveRightGetChar()
		}
	}
}

func (c *escCursor) scanCharEscape() (rune, error) {
	ch := c.moveRightGetChar()
	if ch >= '0' && ch <= '7' {
		c.moveLeft()
		return c.scanOctal(), nil
	}
	switch ch {
	case 'x':
		if c.charsRight() > 0 && c.rightChar(0) == '{' {
			c.moveRight(1)
			return c.scanHexUntilBrace()
		}
		return c.scanHex(2)
	case 'u':
		return c.scanHex(4)
	case 'a':
		return '\u0007', nil
	case 'b':
		return '\b', nil
	case 'e':
		return '\u001B', nil
	case 'f':
		return '\f', nil
	case 'n':
		return '\n', nil
	case 'r':
		return '\r', nil
	case 't':
		return '\t', nil
	case 'v':
		return '\u000B', nil
	case 'c':
		return c.scanControl()
	default:
		if isWordRune(ch) {
			return 0, errUnrecognizedEscape
		}
		return ch, nil
	}
}

func (c *escCursor) scanControl() (rune, error) {
	if c.charsRight() <= 0 {
		return 0, errMissingControl
	}
	ch := c.moveRightGetChar()
	if ch >= 'a' && ch <= 'z' {
		ch = ch - ('a' - 'A')
	}
	ch = ch - '@'
	if ch >= 0 && ch < ' ' {
		return ch, nil
	}
	return 0, errUnrecognizedCtrl
}

func (c *escCursor) scanHexUntilBrace() (rune, error) {
	i := 0
	hasContent := false
	for c.charsRight() > 0 {
		ch := c.moveRightGetChar()
		if ch == '}' {
			if !hasContent {
				return 0, errTooFewHex
			}
			return rune(i), nil
		}
		hasContent = true
		d := hexDigit(ch)
		if d < 0 {
			return 0, errMissingBrace
		}
		i = i*0x10 + d
		if i > unicode.MaxRune {
			return 0, errInvalidHex
		}
	}
	return 0, errMissingBrace
}

func (c *escCursor) scanHex(count int) (rune, error) {
	i := 0
	if c.charsRight() >= count {
		for count > 0 {
			d := hexDigit(c.moveRightGetChar())
			if d < 0 {
				break
			}
			i = i*0x10 + d
			count--
		}
	}
	if count > 0 {
		return 0, errTooFewHex
	}
	return rune(i), nil
}

func (c *escCursor) scanOctal() rune {
	count := 3
	if count > c.charsRight() {
		count = c.charsRight()
	}
	i := 0
	d := int(c.rightChar(0) - '0')
	for count > 0 && d <= 7 && d >= 0 {
		i = i*8 + d
		count--
		c.moveRight(1)
		if !c.rightMost() {
			d = int(c.rightChar(0) - '0')
		}
	}
	i &= 0xFF
	return rune(i)
}

func hexDigit(ch rune) int {
	if d := uint(ch - '0'); d <= 9 {
		return int(d)
	}
	if d := uint(ch - 'a'); d <= 5 {
		return int(d + 0xa)
	}
	if d := uint(ch - 'A'); d <= 5 {
		return int(d + 0xa)
	}
	return -1
}
