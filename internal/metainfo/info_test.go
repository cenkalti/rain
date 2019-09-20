package metainfo

import "testing"

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
