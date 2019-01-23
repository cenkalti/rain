package btconn

import (
	"net"
	"testing"
	"time"

	"github.com/cenkalti/rain/internal/mse"
)

var (
	ext1     = [8]byte{0x0A}
	ext2     = [8]byte{0x0B}
	id1      = [20]byte{0x0C}
	id2      = [20]byte{0x0D}
	infoHash = [20]byte{0x0E}
	sKeyHash = mse.HashSKey(infoHash[:])
)

func TestUnencrypted(t *testing.T) {
	l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(0, 0, 0, 0), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	done := make(chan struct{})
	var gerr error
	go func() {
		defer close(done)
		conn, cipher, ext, id, err2 := Dial(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}, 10*time.Second, 10*time.Second, false, false, ext1, infoHash, id1, nil)
		if err2 != nil {
			gerr = err2
			return
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
	}()
	conn, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	_, cipher, ext, id, ih, err := Accept(conn, 10*time.Second, nil, false, func(ih [20]byte) bool { return ih == infoHash }, ext2, id2)
	if err != nil {
		t.Fatal(err)
	}
	<-done
	if gerr != nil {
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

func TestEncrypted(t *testing.T) {
	l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(0, 0, 0, 0), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	done := make(chan struct{})
	var gerr error
	go func() {
		defer close(done)
		conn, cipher, ext, id, err2 := Dial(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}, 10*time.Second, 10*time.Second, true, true, ext1, infoHash, id1, nil)
		if err2 != nil {
			gerr = err2
			return
		}
		if conn == nil {
			t.Errorf("conn: %s", conn)
		}
		if cipher != mse.RC4 {
			t.Errorf("cipher: %d", cipher)
		}
		if ext != ext2 {
			t.Errorf("ext: %s", ext)
		}
		if id != id2 {
			t.Errorf("id: %s", id)
		}
		_, err2 = conn.Write([]byte("hello out"))
		if err2 != nil {
			t.Fail()
		}
		b := make([]byte, 10)
		n, err2 := conn.Read(b)
		if err2 != nil {
			t.Error(err2)
		}
		if n != 8 {
			t.Fail()
		}
		if string(b[:8]) != "hello in" {
			t.Fail()
		}
	}()
	conn, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	encConn, cipher, ext, id, ih, err := Accept(
		conn,
		10*time.Second,
		func(h [20]byte) (sKey []byte) {
			if h == sKeyHash {
				return infoHash[:]
			}
			return nil
		},
		false,
		func(ih [20]byte) bool { return ih == infoHash },
		ext2, id2)
	if err != nil {
		conn.Close()
		<-done
		t.Fatal(err)
	}
	if cipher != mse.RC4 {
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
	b := make([]byte, 10)
	n, err := encConn.Read(b)
	if err != nil {
		t.Error(err)
	}
	if n != 9 {
		t.Fail()
	}
	if string(b[:9]) != "hello out" {
		t.Fail()
	}
	_, err = encConn.Write([]byte("hello in"))
	if err != nil {
		t.Fail()
	}
	<-done
	if gerr != nil {
		t.Fatal(err)
	}
}
