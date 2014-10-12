// Provides support for manipulating bits in a []byte.

package rain

import "encoding/hex"

type bitfield struct {
	b      []byte
	length uint32
}

// NewBitfield creates a new Bitfield of length bits.
func newBitfield(length uint32) *bitfield {
	return &bitfield{make([]byte, (length+7)/8), length}
}

// NewBitfieldBytes returns a new Bitfield from bytes.
// Bytes in b are not copied. Unused bits in last byte are cleared.
// Panics if b is not big enough to hold "length" bits.
func newBitfieldBytes(b []byte, length uint32) *bitfield {
	div, mod := divMod32(length, 8)
	lastByteIncomplete := mod != 0
	requiredBytes := div
	if lastByteIncomplete {
		requiredBytes++
	}
	if uint32(len(b)) < requiredBytes {
		panic("not enough bytes in slice for specified length")
	}
	if lastByteIncomplete {
		b[len(b)-1] &= ^(0xff >> mod)
	}
	return &bitfield{b[:requiredBytes], length}
}

// Bytes returns bytes in b. If you modify the returned slice the bits in b are modified too.
func (b *bitfield) Bytes() []byte { return b.b }

// Len returns the number of bits as given to New.
func (b *bitfield) Len() uint32 { return b.length }

// Hex returns bytes as string. If not all the bits in last byte are used, they encode as not set.
func (b *bitfield) Hex() string { return hex.EncodeToString(b.b) }

// Set bit i. 0 is the most significant bit. Panics if i >= b.Len().
func (b *bitfield) Set(i uint32) {
	b.checkIndex(i)
	div, mod := divMod32(i, 8)
	b.b[div] |= 1 << (7 - mod)
}

// SetTo sets bit i to value. Panics if i >= b.Len().
func (b *bitfield) SetTo(i uint32, value bool) {
	b.checkIndex(i)
	if value {
		b.Set(i)
	} else {
		b.Clear(i)
	}
}

// Clear bit i. 0 is the most significant bit. Panics if i >= b.Len().
func (b *bitfield) Clear(i uint32) {
	b.checkIndex(i)
	div, mod := divMod32(i, 8)
	b.b[div] &= ^(1 << (7 - mod))
}

// FirstSet returns the index of the first bit that is set starting from start.
func (b *bitfield) FirstSet(start uint32) (uint32, bool) {
	for i := start; i < b.length; i++ {
		if b.Test(i) {
			return i, true
		}
	}
	return 0, false
}

// FirstClear returns the index of the first bit that is not set starting from start.
func (b *bitfield) FirstClear(start uint32) (uint32, bool) {
	for i := start; i < b.length; i++ {
		if !b.Test(i) {
			return i, true
		}
	}
	return 0, false
}

// ClearAll clears all bits.
func (b *bitfield) ClearAll() {
	for i := range b.b {
		b.b[i] = 0
	}
}

// Test bit i. 0 is the most significant bit. Panics if i >= b.Len().
func (b *bitfield) Test(i uint32) bool {
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
func (b *bitfield) Count() uint32 {
	var total uint32
	for _, v := range b.b {
		total += uint32(countCache[v])
	}
	return total
}

// All returns true if all bits are set, false otherwise.
func (b *bitfield) All() bool {
	return b.Count() == b.length
}

func (b *bitfield) checkIndex(i uint32) {
	if i >= b.Len() {
		panic("index out of bound")
	}
}

func divMod32(a, b uint32) (uint32, uint32) { return a / b, a % b }
