package connection_test

import (
	"net"
	"testing"

	"github.com/cenkalti/rain/internal/connection"
	"github.com/cenkalti/rain/internal/protocol"
)

var addr = &net.TCPAddr{
	IP:   net.IPv4(127, 0, 0, 1),
	Port: 5000,
}

var (
	ext1     = [8]byte{0x0A}
	ext2     = [8]byte{0x0B}
	id1      = protocol.PeerID{0x0C}
	id2      = protocol.PeerID{0x0D}
	infoHash = protocol.InfoHash{0x0E}
)

func TestUnencrypted(t *testing.T) {
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		conn, cipher, ext, id, err := connection.Dial(addr, false, false, ext1, infoHash, id1)
		if err != nil {
			t.Fatal(err)
		}
		if conn == nil {
			t.Errorf("conn: %s", conn)
		}
		if cipher != 0 {
			t.Errorf("cipher: %d", cipher)
		}
		if ext != ext2 {
			t.Errorf("ext: %s", ext)
		}
		if id != id2 {
			t.Errorf("id: %s", id)
		}
		close(done)
	}()
	conn, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	cipher, ext, ih, id, err := connection.Accept(conn, false, nil, func(ih protocol.InfoHash) bool { return ih == infoHash }, ext2, id2)
	<-done
	if err != nil {
		t.Fatal(err)
	}
	if cipher != 0 {
		t.Errorf("cipher: %d", cipher)
	}
	if ext != ext1 {
		t.Errorf("ext: %s", ext)
	}
	if ih != infoHash {
		t.Errorf("ih: %s", ih)
	}
	if id != id1 {
		t.Errorf("id: %s", id)
	}
}
