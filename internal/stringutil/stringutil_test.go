package stringutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAsciify(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain ascii", "hello world", "hello world"},
		{"printable boundaries kept", " ~", " ~"}, // 0x20 and 0x7e
		{"tab and newline replaced", "a\tb\nc", "a_b_c"},
		{"del byte replaced", "a\x7fb", "a_b"}, // 0x7f is not < 127
		{"high bytes replaced", "\x80\xff", "__"},
		{"utf8 replaced per byte", "héllo", "h__llo"}, // é is two UTF-8 bytes
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Asciify(tc.in))
		})
	}
}

func TestPrintable(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain ascii", "hello", "hello"},
		{"printable unicode kept", "héllo", "héllo"},
		{"null replaced", "a\x00b", "a�b"},
		{"newline replaced", "a\nb", "a�b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Printable(tc.in))
		})
	}
}
