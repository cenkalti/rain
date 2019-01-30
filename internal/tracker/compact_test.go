package tracker

import (
	"testing"
)

func TestCompactPeer(t *testing.T) {
	cp := CompactPeer{
		IP:   [4]byte{1, 2, 3, 4},
		Port: 5,
	}
	b, err := cp.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var cp2 CompactPeer
	err = cp2.UnmarshalBinary(b)
	if err != nil {
		t.Fatal(err)
	}
	if cp != cp2 {
		t.FailNow()
	}
}
