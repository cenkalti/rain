package metainfo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCalculatePieceLength(t *testing.T) {
	l := calculatePieceLength(1)
	if l != 32<<10 {
		t.FailNow()
	}
	l = calculatePieceLength(1 << 40)
	if l != 16<<20 {
		t.FailNow()
	}
	l = calculatePieceLength(500 << 20)
	if l != 256<<10 {
		t.FailNow()
	}
	l = calculatePieceLength(5 << 30)
	if l != 4<<20 {
		t.FailNow()
	}
}

func TestCleanName(t *testing.T) {
	cases := []struct {
		name    string
		cleaned string
		max     int
	}{
		{"foo.bar", "foo.bar", 10},
		{"foo.bar", "foo.bar", 7},
		{"foo.bar", "fo.bar", 6},
		{"foo.bar", ".bar", 4},
		{"foo.bar", "foo", 3},
		{"foobar", "foobar", 10},
		{"foobar", "fo", 2},
		{"ğğğğ", "ğğğğ", 9},
		{"ğğğğ", "ğğğğ", 8},
		{"ğğğğ", "ğğğ", 7},
		{"ğğğğ", "ğğğ", 6},
	}
	for _, c := range cases {
		assert.Equal(t, c.cleaned, cleanNameN(c.name, c.max))
	}
}
