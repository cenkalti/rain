package peerwriter

import (
	"bytes"
	"testing"

	"github.com/cenkalti/rain/v2/internal/peerprotocol"
)

func BenchmarkRead(b *testing.B) {
	buf := make([]byte, 10)
	buf2 := make([]byte, 25)
	r := bytes.NewReader(buf)
	p := Piece{
		Data: r,
		RequestMessage: peerprotocol.RequestMessage{
			Begin:  2,
			Length: 5,
		},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		p.Read(buf2)
	}
}
