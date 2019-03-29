package peerwriter

import (
	"bytes"
	"testing"
)

func BenchmarkRead(b *testing.B) {
	buf := make([]byte, 10)
	buf2 := make([]byte, 25)
	r := bytes.NewReader(buf)
	p := Piece{
		Piece:  r,
		Begin:  2,
		Length: 5,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Read(buf2)
	}
}
