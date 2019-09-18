package boltdbresumer

import (
	"bytes"
	"testing"
)

func TestMarshalUnmarshalSpec(t *testing.T) {
	s := Spec{
		Info: []byte{1, 2, 3},
		Name: "foo",
	}
	b, err := s.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	s2 := Spec{}
	err = s2.UnmarshalJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(s.Info, s2.Info) {
		t.FailNow()
	}
	if s.Name != s2.Name {
		t.FailNow()
	}
}
