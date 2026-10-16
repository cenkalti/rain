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

func TestTransportRenewsExpiredConnectionID(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		transport, conn := startTestTransport(t)
		started := time.Now()
		req, done := startTestRequest(transport, context.Background())
		_, connect := conn.receive(t)
		conn.connect(t, connect, 11)
		_, original := conn.receive(t)
		require.Equal(t, int64(11), original.ConnectionID)
		for _, elapsed := range []time.Duration{15 * time.Second, 45 * time.Second} {
			packet, retry := conn.receive(t)
			require.Equal(t, elapsed, packet.at.Sub(started))
			require.Equal(t, original, retry)
		}
		packet, reconnect := conn.receive(t)
		require.Equal(t, 105*time.Second, packet.at.Sub(started))
		require.Equal(t, actionConnect, reconnect.Action)
		conn.announce(t, original)
		synctest.Wait()
		select {
		case <-req.done:
			t.Fatal("late announce reply completed the renewed request")
		default:
		}
		conn.connect(t, reconnect, 22)
		_, renewed := conn.receive(t)
		require.Equal(t, actionAnnounce, renewed.Action)
		require.Equal(t, int64(22), renewed.ConnectionID)
		require.NotEqual(t, original.TransactionID, renewed.TransactionID)
		conn.announce(t, renewed)
		require.NoError(t, <-done)
	})
}

func TestTransportAnnounceDeadline(t *testing.T) {
	for _, respondToConnect := range []bool{false, true} {
		name := "silent_connect"
		if respondToConnect {
			name = "silent_announce_across_renewal"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				transport, conn := startTestTransport(t)
				started := time.Now()
				req, done := startTestRequest(transport, context.Background())
				_, connect := conn.receive(t)
				if respondToConnect {
					conn.connect(t, connect, 11)
					for range 3 {
						_, announce := conn.receive(t)
						require.Equal(t, actionAnnounce, announce.Action)
					}
					_, reconnect := conn.receive(t)
					conn.connect(t, reconnect, 22)
					_, announce := conn.receive(t)
					require.Equal(t, int64(22), announce.ConnectionID)
				}
				require.ErrorIs(t, <-done, context.DeadlineExceeded)
				require.Equal(t, announceTimeout, time.Since(started))
				synctest.Wait()
				require.Nil(t, req.transaction)
				require.ErrorIs(t, req.connection.transaction.ctx.Err(), context.Canceled)
			})
		})
	}
}

func TestTransportSharedConnectSurvivesWaiterCancellation(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "cancellation"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				transport, conn := startTestTransport(t)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				first, firstDone := startTestRequest(transport, ctx)
				_, connect := conn.receive(t)
				if deadline {
					time.Sleep(30 * time.Second)
				}
				_, secondDone := startTestRequest(transport, context.Background())
				synctest.Wait()
				if deadline {
					require.ErrorIs(t, <-firstDone, context.DeadlineExceeded)
				} else {
					cancel()
					require.ErrorIs(t, <-firstDone, context.Canceled)
				}
				synctest.Wait()
				require.NoError(t, first.connection.transaction.ctx.Err())
				require.Len(t, first.connection.requests, 1)
				// Ignore the unanswered connect retransmissions recorded while waiting.
				for len(conn.sent) > 0 {
					<-conn.sent
				}
				conn.connect(t, connect, 11)
				_, announce := conn.receive(t)
				conn.announce(t, announce)
				require.NoError(t, <-secondDone)
			})
		})
	}
}

func TestTransportCancellationRemovesTransactions(t *testing.T) {
	for _, connected := range []bool{false, true} {
		name := "connect"
		if connected {
			name = "announce"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				transport, conn := startTestTransport(t)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				req, done := startTestRequest(transport, ctx)
				_, original := conn.receive(t)
				if connected {
					conn.connect(t, original, 11)
					_, original = conn.receive(t)
				}
				cancel()
				require.ErrorIs(t, <-done, context.Canceled)
				synctest.Wait()
				require.Nil(t, req.transaction)
				_, replacementDone := startTestRequest(transport, context.Background())
				_, replacement := conn.receive(t)
				if connected {
					conn.announce(t, original)
				} else {
					conn.connect(t, original, 11)
				}
				synctest.Wait()
				oldDone := req.done
				if !connected {
					oldDone = req.connection.done
				}
				select {
				case <-oldDone:
					t.Fatal("late reply reached a cancelled transaction")
				default:
				}
				if !connected {
					conn.connect(t, replacement, 22)
					_, replacement = conn.receive(t)
				}
				conn.announce(t, replacement)
				require.NoError(t, <-replacementDone)
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
				req, done := startTestRequest(transport, context.Background())
				_, connect := conn.receive(t)
				if connected {
					conn.connect(t, connect, 11)
					conn.receive(t)
				}
				stopped := time.Now()
				stop()
				require.Error(t, <-done)
				require.Equal(t, stopped, time.Now())
				synctest.Wait()
				require.ErrorIs(t, req.connection.transaction.ctx.Err(), context.Canceled)
			})
		})
	}
}

func TestRetryBackoffCarriesAcrossReconnect(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		conn := newTestPacketConn()
		defer conn.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		req := newTransportRequest(ctx, tracker.AnnounceRequest{}, "127.0.0.1:1337", "/announce")
		trx := newTransaction(req)
		defer trx.cancel()
		expired := make(chan expiredTransaction)
		started := time.Now()
		go retryTransaction(trx, conn, conn.LocalAddr(), 0, started.Add(time.Minute), expired)
		for _, elapsed := range []time.Duration{0, 15 * time.Second, 45 * time.Second} {
			packet, _ := conn.receive(t)
			require.Equal(t, elapsed, packet.at.Sub(started))
		}
		renewal := <-expired
		require.Equal(t, 105*time.Second, time.Since(started))
		require.Equal(t, udpBackOff(3), renewal.backoff)
		req.retryBackoff = renewal.backoff
		connection := newConnection(ctx, req)
		connectTransaction := newTransaction(connection)
		defer connectTransaction.cancel()
		connectDone := make(chan udpBackOff, 1)
		go func() {
			_, backoff, _ := sendAndReceiveConnect(connectTransaction, conn, conn.LocalAddr())
			connectDone <- backoff
		}()
		packet, _ := conn.receive(t)
		require.Equal(t, 105*time.Second, packet.at.Sub(started))
		packet, connect := conn.receive(t)
		require.Equal(t, 225*time.Second, packet.at.Sub(started))
		connection.SetResponse(encodeTestResponse(t, connectResponse{
			udpMessageHeader: connect.udpMessageHeader, ConnectionID: 22,
		}), nil)
		backoff := <-connectDone
		require.Equal(t, udpBackOff(4), backoff)
		req.ConnectionID = 22
		announceTransaction := newTransaction(req)
		defer announceTransaction.cancel()
		go retryTransaction(announceTransaction, conn, conn.LocalAddr(), backoff, time.Time{}, nil)
		packet, _ = conn.receive(t)
		require.Equal(t, 225*time.Second, packet.at.Sub(started))
		packet, _ = conn.receive(t)
		require.Equal(t, 465*time.Second, packet.at.Sub(started))
	})
}
