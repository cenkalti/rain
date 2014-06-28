package bitfield

import "testing"

func TestNewBitField(t *testing.T) {
	var v BitField
	var buf = []byte{0x0f}

	v = NewBitField(buf, 8)
	if v.Hex() != "0f" {
		t.Errorf("invalid value: %s", v.Hex())
	}

	v = NewBitField(buf, 7)
	if v.Hex() != "0e" {
		t.Errorf("invalid value: %s", v.Hex())
	}

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic but not found")
			}
		}()
		NewBitField(buf, 9)
	}()

	v = NewBitField(nil, 10)
	if v.Hex() != "0000" {
		t.Errorf("invalid value: %s", v.Hex())
	}

	v.Set(0)
	if v.Hex() != "8000" {
		t.Errorf("invalid value: %s", v.Hex())
	}

	v.Set(9)
	if v.Hex() != "8040" {
		t.Errorf("invalid value: %s", v.Hex())
	}

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic but not found")
			}
		}()
		v.Set(10)
	}()

	v.Clear(0)
	if v.Hex() != "0040" {
		t.Errorf("invalid value: %s", v.Hex())
	}

	if v.Test(2) {
		t.Errorf("test is not correct: %s", v.Hex())
	}

	if !v.Test(9) {
		t.Errorf("test is not correct: %s", v.Hex())
	}
}
