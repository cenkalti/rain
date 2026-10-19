package udptracker

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"net/url"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cenkalti/rain/v2/internal/tracker"
	"github.com/stretchr/testify/require"
)

// Channel-backed packets let synctest advance retry timers without real sockets.
type testPacketConn struct {
	sent      chan testPacket
	replies   chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

type testPacket struct {
	data []byte
	addr net.Addr
	at   time.Time
}

func newTestPacketConn() *testPacketConn {
	return &testPacketConn{
		sent:    make(chan testPacket, 32),
		replies: make(chan []byte, 32),
		closed:  make(chan struct{}),
	}
}

func (c *testPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case data := <-c.replies:
		return copy(b, data), c.LocalAddr(), nil
	case <-c.closed:
		return 0, nil, net.ErrClosed
	}
}

func (c *testPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	packet := testPacket{data: bytes.Clone(b), addr: addr, at: time.Now()}
	select {
	case c.sent <- packet:
		return len(b), nil
	case <-c.closed:
		return 0, net.ErrClosed
	}
}

func (c *testPacketConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *testPacketConn) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1337}
}
func (c *testPacketConn) SetDeadline(time.Time) error      { return nil }
func (c *testPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (c *testPacketConn) SetWriteDeadline(time.Time) error { return nil }

func (c *testPacketConn) receive(t *testing.T) (testPacket, udpRequestHeader) {
	t.Helper()
	select {
	case packet := <-c.sent:
		var header udpRequestHeader
		require.NoError(t, binary.Read(bytes.NewReader(packet.data), binary.BigEndian, &header))
		return packet, header
	case <-time.After(2 * time.Hour):
		t.Fatal("no UDP request")
		return testPacket{}, udpRequestHeader{}
	}
}

func encodeTestResponse(t *testing.T, response any) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, binary.Write(&buf, binary.BigEndian, response))
	return buf.Bytes()
}

func (c *testPacketConn) connect(t *testing.T, header udpRequestHeader, id int64) {
	t.Helper()
	require.Equal(t, actionConnect, header.Action)
	c.replies <- encodeTestResponse(t, connectResponse{udpMessageHeader: header.udpMessageHeader, ConnectionID: id})
}

func (c *testPacketConn) announce(t *testing.T, header udpRequestHeader) {
	t.Helper()
	require.Equal(t, actionAnnounce, header.Action)
	c.replies <- encodeTestResponse(t, udpAnnounceResponse{udpMessageHeader: header.udpMessageHeader, Interval: 60})
}

func startTestTransport(t *testing.T) (*Transport, *testPacketConn) {
	t.Helper()
	conn := newTestPacketConn()
	transport := NewTransport(nil, time.Second)
	go transport.run(conn, nil)
	t.Cleanup(transport.Close)
	return transport, conn
}

func startTestRequest(transport *Transport, ctx context.Context) (*transportRequest, <-chan error) {
	req := newTransportRequest(ctx, tracker.AnnounceRequest{}, "127.0.0.1:1337", "/announce")
	done := make(chan error, 1)
	go func() {
		_, err := transport.Do(req)
		done <- err
	}()
	return req, done
}

func TestTransportAnnounceDeadline(t *testing.T) {
	for _, respondToConnect := range []bool{false, true} {
		name := "silent_connect"
		if respondToConnect {
			name = "silent_announce"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				transport, conn := startTestTransport(t)
				started := time.Now()
				_, done := startTestRequest(transport, context.Background())
				_, connect := conn.receive(t)
				if respondToConnect {
					conn.connect(t, connect, 11)
					for _, elapsed := range []time.Duration{0, 15 * time.Second, 45 * time.Second} {
						packet, announce := conn.receive(t)
						require.Equal(t, actionAnnounce, announce.Action)
						require.Equal(t, elapsed, packet.at.Sub(started))
					}
				}
				require.ErrorIs(t, <-done, context.DeadlineExceeded)
				require.Equal(t, announceTimeout, time.Since(started))
				if respondToConnect {
					// The connection ID expired at 60 seconds, so the retry at 105 seconds is not sent.
					require.Empty(t, conn.sent)
				}
			})
		})
	}
}

func TestTransportTimeoutAdvancesTrackerTier(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		transport, conn := startTestTransport(t)
		primary, err := url.Parse("udp://127.0.0.1:1337/announce")
		require.NoError(t, err)
		secondary, err := url.Parse("udp://127.0.0.1:1338/announce")
		require.NoError(t, err)
		tier := &tracker.Tier{Trackers: []tracker.Tracker{
			New(primary.String(), primary, transport),
			New(secondary.String(), secondary, transport),
		}}
		_, err = tier.Announce(context.Background(), tracker.AnnounceRequest{})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, secondary.String(), tier.URL())
		synctest.Wait()
		for len(conn.sent) > 0 {
			<-conn.sent
		}
		done := make(chan error, 1)
		go func() {
			_, err := tier.Announce(context.Background(), tracker.AnnounceRequest{})
			done <- err
		}()
		packet, connect := conn.receive(t)
		require.Equal(t, secondary.Host, packet.addr.String())
		conn.connect(t, connect, 22)
		_, announce := conn.receive(t)
		conn.announce(t, announce)
		require.NoError(t, <-done)
	})
}

func TestTransportHonorsCallerDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		transport, _ := startTestTransport(t)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		started := time.Now()
		_, done := startTestRequest(transport, ctx)
		require.ErrorIs(t, <-done, context.DeadlineExceeded)
		require.Equal(t, 5*time.Second, time.Since(started))
	})
}

func TestTransportCloseStopsRequests(t *testing.T) {
	for _, connected := range []bool{false, true} {
		name := "connect"
		if connected {
			name = "announce"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				conn := newTestPacketConn()
				transport := NewTransport(nil, time.Second)
				stop := sync.OnceFunc(transport.Close)
				defer stop()
				go transport.run(conn, nil)
				_, done := startTestRequest(transport, context.Background())
				_, connect := conn.receive(t)
				if connected {
					conn.connect(t, connect, 11)
					conn.receive(t)
				}
				stopped := time.Now()
				stop()
				require.Error(t, <-done)
				require.Equal(t, stopped, time.Now())
			})
		})
	}
}
