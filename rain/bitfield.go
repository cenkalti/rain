package rain

import "encoding/hex"

type BitField struct {
	b      []byte
	length int64
}

// NewBitField returns a new BitField value. Bytes in buf are copied.
// If buf is nil, a new buffer is created to store "length" bits.
func NewBitField(buf []byte, length int64) BitField {
	if length < 0 {
		panic("length < 0")
	}

	div, mod := divMod(length, 8)
	lastByteIncomplete := mod != 0

	requiredBytes := div
	if lastByteIncomplete {
		requiredBytes++
	}

	if buf != nil && int64(len(buf)) < requiredBytes {
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
func (b BitField) Len() int64 { return b.length }

// Hex returns bytes as string.
func (b BitField) Hex() string { return hex.EncodeToString(b.b) }

// Set bit i. 0 is the most significant bit. Panics if i >= b.Len().
func (b BitField) Set(i int64) {
	b.checkIndex(i)
	div, mod := divMod(i, 8)
	b.b[div] |= 1 << (8 - 1 - mod)
}

// SetTo sets bit i to value. Panics if i >= b.Len().
func (b BitField) SetTo(i int64, value bool) {
	b.checkIndex(i)
	if value {
		b.Set(i)
	}
	b.Clear(i)
}

// Clear bit i. 0 is the most significant bit. Panics if i >= b.Len().
func (b BitField) Clear(i int64) {
	b.checkIndex(i)
	div, mod := divMod(i, 8)
	b.b[div] &= ^(1 << (8 - 1 - mod))
}

// ClearAll clears all bits.
func (b BitField) ClearAll() {
	for i := range b.b {
		b.b[i] = 0
	}
}

// Test bit i. 0 is the most significant bit. Panics if i >= b.Len().
func (b BitField) Test(i int64) bool {
	b.checkIndex(i)
	div, mod := divMod(i, 8)
	return (b.b[div] & (1 << (8 - 1 - mod))) > 0
}

// Count returns the count of set bits.
func (b BitField) Count() int64 {
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
	var total int64
	for _, v := range b.b {
		total += int64(countCache[v])
	}
	return total
}

// All returns true if all bits are set, false otherwise.
func (b BitField) All() bool {
	return b.Count() == b.length
}

func (b BitField) checkIndex(i int64) {
	if i < 0 || i >= b.Len() {
		panic("index out of bound")
	}
}

func divMod(a, b int64) (int64, uint64)  { return a / b, uint64(a % b) }
func divMod32(a, b int32) (int32, int32) { return a / b, a % b }
