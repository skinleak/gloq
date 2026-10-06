package gloq

import (
	"strconv"
	"unicode"
	"unicode/utf8"
)

// Everything gloq writes to a terminal passes through these helpers, so logged
// data can never start a forged log line or smuggle in terminal control
// sequences.

// appendString writes a value, quoting it when it would be ambiguous or unsafe.
func appendString(buf []byte, value string) []byte {
	if value == "" || needsQuoting(value) {
		return strconv.AppendQuote(buf, value)
	}
	return append(buf, value...)
}

// needsQuoting reports whether a key or value contains separators, quotes,
// control characters, invalid UTF-8, or any other non-printable rune.
func needsQuoting(value string) bool {
	for index := 0; index < len(value); {
		char := value[index]
		if char < utf8.RuneSelf {
			if char <= ' ' || char == '=' || char == '"' || char == 0x7f {
				return true
			}
			index++
			continue
		}
		r, size := utf8.DecodeRuneInString(value[index:])
		if (r == utf8.RuneError && size == 1) || !unicode.IsPrint(r) {
			return true
		}
		index += size
	}
	return false
}

// appendText writes free text, such as a message, without quotes. Control
// characters and invisible formatting runes are escaped Go-style; tabs are
// kept. Callers that render multi-line text split it on '\n' first.
func appendText(buf []byte, text string) []byte {
	start := 0
	for index := 0; index < len(text); {
		char := text[index]
		if char < utf8.RuneSelf {
			if (char >= ' ' && char != 0x7f) || char == '\t' {
				index++
				continue
			}
			buf = append(buf, text[start:index]...)
			buf = appendEscapedByte(buf, char)
			index++
			start = index
			continue
		}
		r, size := utf8.DecodeRuneInString(text[index:])
		if r == utf8.RuneError && size == 1 {
			buf = append(buf, text[start:index]...)
			buf = appendEscapedByte(buf, char)
		} else if !unicode.IsGraphic(r) {
			buf = append(buf, text[start:index]...)
			buf = appendEscapedRune(buf, r)
		} else {
			index += size
			continue
		}
		index += size
		start = index
	}
	return append(buf, text[start:]...)
}

const hexDigits = "0123456789abcdef"

func appendEscapedByte(buf []byte, char byte) []byte {
	switch char {
	case '\n':
		return append(buf, `\n`...)
	case '\r':
		return append(buf, `\r`...)
	case '\t':
		return append(buf, `\t`...)
	}
	return append(buf, '\\', 'x', hexDigits[char>>4], hexDigits[char&0xf])
}

func appendEscapedRune(buf []byte, r rune) []byte {
	if r > 0xffff {
		buf = append(buf, '\\', 'U')
		for shift := 28; shift >= 0; shift -= 4 {
			buf = append(buf, hexDigits[(r>>uint(shift))&0xf])
		}
		return buf
	}
	buf = append(buf, '\\', 'u')
	for shift := 12; shift >= 0; shift -= 4 {
		buf = append(buf, hexDigits[(r>>uint(shift))&0xf])
	}
	return buf
}
