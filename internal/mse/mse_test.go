package mse_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"testing"

	"github.com/cenkalti/rain/internal/mse"
)

// Pipe2 is a bidirectional io.Pipe.
type Pipe2 struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func (p *Pipe2) Read(b []byte) (n int, err error) {
	return p.r.Read(b)
}

func (p *Pipe2) Write(b []byte) (n int, err error) {
	return p.w.Write(b)
}

func (p *Pipe2) Close() error {
	p.r.Close()
	p.w.Close()
	return nil
}

func NewPipe2() (*Pipe2, *Pipe2) {
	var a, b Pipe2
	a.r, b.w = io.Pipe()
	b.r, a.w = io.Pipe()
	return &a, &b
}

func TestPipe2(t *testing.T) {
	a, b := NewPipe2()

	err := testRws(a, b)
	if err != nil {
		t.Fatal(err)
	}
}

func TestStream(t *testing.T) {
	conn1, conn2 := NewPipe2()

	a := mse.NewStream(conn1)
	b := mse.NewStream(conn2)
	sKey := []byte("1234")
	payloadA := []byte("payloadA")
	payloadB := []byte("payloadB")
	payloadARead := make([]byte, 8)
	payloadBRead := make([]byte, 8)

	done := make(chan struct{})
	var gerr error
	go func() {
		defer close(done)
		_, gerr = a.HandshakeOutgoing(sKey, mse.RC4, payloadA)
		if gerr != nil {
			return
		}
		_, gerr = io.ReadFull(a, payloadBRead)
		if gerr != nil {
			return
		}
	}()
	err := b.HandshakeIncoming(
		func(sKeyHash [20]byte) []byte {
			if sKeyHash == mse.HashSKey(sKey) {
				return sKey
			}
			return nil
		},
		func(provided mse.CryptoMethod) (selected mse.CryptoMethod) {
			if provided == mse.RC4 {
				selected = mse.RC4
			}
			return
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadFull(b, payloadARead)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payloadARead, payloadA) {
		t.Fatal("invalid payload A")
	}
	_, err = b.Write(payloadB)
	if err != nil {
		t.Fatal(err)
	}
	<-done
	if gerr != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payloadB, payloadBRead) {
		t.Fatal("invalid payload B")
	}

	err = testRws(a, b)
	if err != nil {
		t.Fatal(err)
	}
}

func testRws(a io.Writer, b io.Reader) error {
	data := []byte("ABCD")
	go func() { _, _ = a.Write(data) }()

	buf := make([]byte, 10)
	n, err := b.Read(buf)
	if err != nil {
		return err
	}
	if n != 4 {
		return fmt.Errorf("n must be 4, not %d", n)
	}
	if !bytes.Equal(buf[:n], data) {
		return fmt.Errorf("invalid data received: %s", hex.EncodeToString(buf[:n]))
	}

	return nil
}
