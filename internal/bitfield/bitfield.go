package bitfield

import (
	"encoding/hex"
	"math"
)

type BitField struct {
	b      []byte
	length uint32
}

// New returns a new bitField value. Bytes in buf are copied.
// If buf is nil, a new buffer is created to store "length" bits.
func New(buf []byte, length uint32) BitField {
	if length < 0 {
		panic("length < 0")
	}
	if len(buf) > math.MaxInt32 {
		panic("buffer is too big")
	}

	div, mod := divMod32(length, 8)
	lastByteIncomplete := mod != 0

	requiredBytes := div
	if lastByteIncomplete {
		requiredBytes++
	}

	if buf != nil && uint32(len(buf)) < requiredBytes {
		panic("not enough bytes in slice for specified length")
	}

	b := make([]byte, requiredBytes)

	if buf != nil {
		copy(b, buf)

		// Truncate last byte according to length.
		if lastByteIncomplete {
			b[len(b)-1] &= ^(0xff >> mod)
		}
	}

	return BitField{b, length}
}

// Bytes returns bytes in b.
func (b BitField) Bytes() []byte { return b.b }

// Len returns bit length.
func (b BitField) Len() uint32 { return b.length }

// Hex returns bytes as string.
func (b BitField) Hex() string { return hex.EncodeToString(b.b) }

// Set bit i. 0 is the most significant bit. Panics if i >= b.Len().
func (b BitField) Set(i uint32) {
	b.checkIndex(i)
	div, mod := divMod32(i, 8)
	b.b[div] |= 1 << (7 - mod)
}

// SetTo sets bit i to value. Panics if i >= b.Len().
func (b BitField) SetTo(i uint32, value bool) {
	b.checkIndex(i)
	if value {
		b.Set(i)
	}
	b.Clear(i)
}

// Clear bit i. 0 is the most significant bit. Panics if i >= b.Len().
func (b BitField) Clear(i uint32) {
	b.checkIndex(i)
	div, mod := divMod32(i, 8)
	b.b[div] &= ^(1 << (7 - mod))
}

// ClearAll clears all bits.
func (b BitField) ClearAll() {
	for i := range b.b {
		b.b[i] = 0
	}
}

// Test bit i. 0 is the most significant bit. Panics if i >= b.Len().
func (b BitField) Test(i uint32) bool {
	b.checkIndex(i)
	div, mod := divMod32(i, 8)
	return (b.b[div] & (1 << (7 - mod))) > 0
}

var countCache = [256]byte{
	0, 1, 1, 2, 1, 2, 2, 3, 1, 2, 2, 3, 2, 3, 3, 4,
	1, 2, 2, 3, 2, 3, 3, 4, 2, 3, 3, 4, 3, 4, 4, 5,
	1, 2, 2, 3, 2, 3, 3, 4, 2, 3, 3, 4, 3, 4, 4, 5,
	2, 3, 3, 4, 3, 4, 4, 5, 3, 4, 4, 5, 4, 5, 5, 6,
	1, 2, 2, 3, 2, 3, 3, 4, 2, 3, 3, 4, 3, 4, 4, 5,
	2, 3, 3, 4, 3, 4, 4, 5, 3, 4, 4, 5, 4, 5, 5, 6,
	2, 3, 3, 4, 3, 4, 4, 5, 3, 4, 4, 5, 4, 5, 5, 6,
	3, 4, 4, 5, 4, 5, 5, 6, 4, 5, 5, 6, 5, 6, 6, 7,
	1, 2, 2, 3, 2, 3, 3, 4, 2, 3, 3, 4, 3, 4, 4, 5,
	2, 3, 3, 4, 3, 4, 4, 5, 3, 4, 4, 5, 4, 5, 5, 6,
	2, 3, 3, 4, 3, 4, 4, 5, 3, 4, 4, 5, 4, 5, 5, 6,
	3, 4, 4, 5, 4, 5, 5, 6, 4, 5, 5, 6, 5, 6, 6, 7,
	2, 3, 3, 4, 3, 4, 4, 5, 3, 4, 4, 5, 4, 5, 5, 6,
	3, 4, 4, 5, 4, 5, 5, 6, 4, 5, 5, 6, 5, 6, 6, 7,
	3, 4, 4, 5, 4, 5, 5, 6, 4, 5, 5, 6, 5, 6, 6, 7,
	4, 5, 5, 6, 5, 6, 6, 7, 5, 6, 6, 7, 6, 7, 7, 8,
}

// Count returns the count of set bits.
func (b BitField) Count() uint32 {
	var total uint32
	for _, v := range b.b {
		total += uint32(countCache[v])
	}
	return total
}

// All returns true if all bits are set, false otherwise.
func (b BitField) All() bool {
	return b.Count() == b.length
}

func (b BitField) checkIndex(i uint32) {
	if i < 0 || i >= b.Len() {
		panic("index out of bound")
	}
}

func divMod32(a, b uint32) (uint32, uint32) { return a / b, a % b }
