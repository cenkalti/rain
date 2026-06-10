package torrent

import "testing"

func TestValidPieceRequest(t *testing.T) {
	const pieceLength = 256 * 1024 // 262144

	cases := []struct {
		name        string
		begin       uint32
		length      uint32
		pieceLength uint32
		want        bool
	}{
		{"normal block at start", 0, 16384, pieceLength, true},
		{"normal block mid piece", 16384, 16384, pieceLength, true},
		{"exact end of piece", pieceLength - 16384, 16384, pieceLength, true},
		{"whole piece", 0, pieceLength, pieceLength, true},
		{"zero length", 0, 0, pieceLength, false},
		{"runs one byte past end", pieceLength - 16384, 16385, pieceLength, false},
		{"begin past end", pieceLength, 1, pieceLength, false},
		// begin+length overflows uint32 and wraps to a small value; the old
		// check (begin+length > pieceLength in uint32) would let this through.
		{"overflowing begin", 0xFFFFFFFF, 16384, pieceLength, false},
		{"overflowing begin small wrap", 0xFFFFF000, 0x4000, pieceLength, false},
	}

	for _, tc := range cases {
		if got := validPieceRequest(tc.begin, tc.length, tc.pieceLength); got != tc.want {
			t.Errorf("%s: validPieceRequest(%#x, %#x, %d) = %v, want %v",
				tc.name, tc.begin, tc.length, tc.pieceLength, got, tc.want)
		}
	}
}
