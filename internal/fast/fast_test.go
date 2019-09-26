package fast

import (
	"encoding/hex"
	"net"
	"reflect"
	"testing"
)

func TestGenerateFastSet(t *testing.T) {
	b, _ := hex.DecodeString("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	var ih [20]byte
	copy(ih[:], b)
	a := GenerateFastSet(7, 1313, ih, net.IPv4(80, 4, 4, 200))
	expected := []uint32{1059, 431, 808, 1217, 287, 376, 1188}
	if !reflect.DeepEqual(a, expected) {
		t.Log(expected)
		t.Log(a)
		t.FailNow()
	}

	a = GenerateFastSet(9, 1313, ih, net.IPv4(80, 4, 4, 200))
	expected = []uint32{1059, 431, 808, 1217, 287, 376, 1188, 353, 508}
	if !reflect.DeepEqual(a, expected) {
		t.Log(expected)
		t.Log(a)
		t.FailNow()
	}

	a = GenerateFastSet(1, 1313, ih, net.IPv4(80, 4, 4, 200))
	expected = []uint32{1059}
	if !reflect.DeepEqual(a, expected) {
		t.Log(expected)
		t.Log(a)
		t.FailNow()
	}

	a = GenerateFastSet(10, 1, ih, net.IPv4(80, 4, 4, 200))
	expected = []uint32{0}
	if !reflect.DeepEqual(a, expected) {
		t.Log(expected)
		t.Log(a)
		t.FailNow()
	}

	a = GenerateFastSet(4, 2, ih, net.IPv4(80, 4, 4, 200))
	expected = []uint32{1, 0}
	if !reflect.DeepEqual(a, expected) {
		t.Log(expected)
		t.Log(a)
		t.FailNow()
	}
}
