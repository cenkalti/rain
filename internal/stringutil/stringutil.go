package stringutil

import (
	"strings"
	"unicode"
)

// Asciify replaces non-ascii characters with '_'.
func Asciify(s string) string {
	b := []byte(s)
	for i, val := range b {
		if val >= 32 && val < 127 {
			continue
		}
		b[i] = '_'
	}
	return string(b)
}

// Printable returns a new string with non-printable characters are replaced with Unicode replacement character.
func Printable(s string) string {
	return strings.Map(func(r rune) rune {
		if !unicode.IsPrint(r) {
			return unicode.ReplacementChar
		}
		return r
	}, s)
}
