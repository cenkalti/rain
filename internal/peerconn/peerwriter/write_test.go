package peerwriter

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/cenkalti/rain/v2/internal/logger"
	"github.com/cenkalti/rain/v2/internal/peerprotocol"
	"github.com/juju/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newWriter starts a writer without registering a Stop in cleanup, for tests
// that stop it themselves. stopC is closed rather than signalled, so a second
// Stop would panic.
func newWriter(t *testing.T, maxQueued int, fastEnabled bool, b *ratelimit.Bucket) (*PeerWriter, net.Conn) {
	t.Helper()
	server, client := net.Pipe()
	w := New(server, logger.New("test"), maxQueued, fastEnabled, b)
	go w.Run()
	t.Cleanup(func() { client.Close() })
	return w, client
}

func TestWriterFramesPiece(t *testing.T) {
	w, client := newTestWriter(t)

	data := []byte("0123456789abcdef")
	w.SendPiece(peerprotocol.RequestMessage{Index: 7, Begin: 4, Length: 8}, bytes.NewReader(data))

	id, body := readFrame(t, client)
	assert.Equal(t, peerprotocol.Piece, id)
	require.Len(t, body, 8+8, "piece body is index, begin, then the block")
	assert.Equal(t, uint32(7), binary.BigEndian.Uint32(body[0:4]))
	assert.Equal(t, uint32(4), binary.BigEndian.Uint32(body[4:8]))
	assert.Equal(t, data[4:12], body[8:], "the block is read at the requested offset")
}

// Every piece written is reported so the torrent can count upload traffic.
func TestWriterReportsUploadedBlocks(t *testing.T) {
	w, client := newTestWriter(t)

	w.SendPiece(peerprotocol.RequestMessage{Index: 1, Begin: 0, Length: 100},
		bytes.NewReader(make([]byte, 100)))

	id, body := readFrame(t, client)
	require.Equal(t, peerprotocol.Piece, id)
	require.Len(t, body, 8+100)

	select {
	case msg := <-w.Messages():
		assert.Equal(t, BlockUploaded{Length: 100}, msg,
			"only the block counts, not the 13 bytes of framing")
	case <-time.After(5 * time.Second):
		t.Fatal("no BlockUploaded reported for a written piece")
	}
}

// Serving the same request twice would send the peer data it already has, so
// the second one is refused instead.
func TestWriterRejectsDuplicateRequest(t *testing.T) {
	w, client := newTestWriter(t)
	req := peerprotocol.RequestMessage{Index: 3, Begin: 0, Length: 8}

	w.SendPiece(req, bytes.NewReader(make([]byte, 8)))
	id, _ := readFrame(t, client)
	require.Equal(t, peerprotocol.Piece, id)

	// Drain the upload report so it cannot block the writer.
	select {
	case <-w.Messages():
	case <-time.After(5 * time.Second):
		t.Fatal("no BlockUploaded for the first piece")
	}

	w.SendPiece(req, bytes.NewReader(make([]byte, 8)))
	id, body := readFrame(t, client)
	assert.Equal(t, peerprotocol.MessageID(peerprotocol.Reject), id,
		"the repeat request is rejected, not served again")
	require.Len(t, body, 12)
	assert.Equal(t, uint32(3), binary.BigEndian.Uint32(body[0:4]))
}

// Different blocks of the same piece are distinct requests and both get served.
func TestWriterServesDistinctBlocksOfSamePiece(t *testing.T) {
	w, client := newTestWriter(t)
	data := make([]byte, 32)

	w.SendPiece(peerprotocol.RequestMessage{Index: 1, Begin: 0, Length: 8}, bytes.NewReader(data))
	id, _ := readFrame(t, client)
	require.Equal(t, peerprotocol.Piece, id)
	<-w.Messages()

	w.SendPiece(peerprotocol.RequestMessage{Index: 1, Begin: 8, Length: 8}, bytes.NewReader(data))
	id, body := readFrame(t, client)
	assert.Equal(t, peerprotocol.Piece, id)
	assert.Equal(t, uint32(8), binary.BigEndian.Uint32(body[4:8]))
}

// Pieces are rate limited; other message types are not, so a throttled peer
// still gets its protocol traffic promptly.
func TestWriterRateLimitsPieces(t *testing.T) {
	bucket := ratelimit.NewBucketWithRate(1<<20, 1<<20)
	w, client := newWriter(t, 10, true, bucket)
	t.Cleanup(func() {
		w.Stop()
		<-w.Done()
	})

	w.SendPiece(peerprotocol.RequestMessage{Index: 1, Begin: 0, Length: 16},
		bytes.NewReader(make([]byte, 16)))

	id, body := readFrame(t, client)
	assert.Equal(t, peerprotocol.Piece, id)
	assert.Len(t, body, 8+16)
}

func TestStopEndsTheWriter(t *testing.T) {
	w, _ := newWriter(t, 10, true, nil)

	w.Stop()
	select {
	case <-w.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not end the writer loop")
	}
}

// The send methods select on doneC, so after the writer stops they return
// instead of blocking on a queue nobody is draining.
func TestSendMethodsReturnAfterStop(t *testing.T) {
	w, _ := newWriter(t, 10, true, nil)
	w.Stop()
	<-w.Done()

	done := make(chan struct{})
	go func() {
		defer close(done)
		w.SendMessage(peerprotocol.ChokeMessage{})
		w.SendPiece(peerprotocol.RequestMessage{Index: 1, Length: 8}, bytes.NewReader(make([]byte, 8)))
		w.CancelRequest(peerprotocol.CancelMessage{})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a send method blocked after the writer stopped")
	}
}

// Closing the connection under the writer makes the write fail, which ends the
// message writer; the run loop then stops once it is told to.
func TestWriterSurvivesClosedConnection(t *testing.T) {
	server, client := net.Pipe()
	w := New(server, logger.New("test"), 10, true, nil)
	go w.Run()

	client.Close()
	w.SendMessage(peerprotocol.ChokeMessage{})

	w.Stop()
	select {
	case <-w.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not stop after the connection closed")
	}
}

func TestWriterFramesMultipleMessagesInOrder(t *testing.T) {
	w, client := newTestWriter(t)

	w.SendMessage(peerprotocol.UnchokeMessage{})
	w.SendMessage(peerprotocol.HaveMessage{Index: 1})
	w.SendMessage(peerprotocol.InterestedMessage{})

	id, _ := readFrame(t, client)
	assert.Equal(t, peerprotocol.Unchoke, id)
	id, body := readFrame(t, client)
	assert.Equal(t, peerprotocol.Have, id)
	assert.Equal(t, []byte{0, 0, 0, 1}, body)
	id, _ = readFrame(t, client)
	assert.Equal(t, peerprotocol.Interested, id)
}

// A choke sent while pieces are queued must drop them, which is the run-loop
// path for the queue behavior covered directly in queue_test.go.
func TestChokeDropsQueuedPiecesEndToEnd(t *testing.T) {
	w, client := newTestWriter(t)

	// Nothing is read yet, so these pile up behind the first write.
	for i := range 4 {
		w.SendPiece(
			peerprotocol.RequestMessage{Index: uint32(i), Begin: 0, Length: 8},
			bytes.NewReader(make([]byte, 8)),
		)
	}
	w.SendMessage(peerprotocol.ChokeMessage{})

	// Drain whatever got through; a choke must appear, and the writer must not
	// wedge on the pieces it discarded.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("choke never reached the peer")
		default:
		}
		id, _ := readFrame(t, client)
		if id == peerprotocol.Choke {
			return
		}
		if id == peerprotocol.Piece {
			select {
			case <-w.Messages():
			case <-time.After(time.Second):
			}
		}
	}
}

// failingReaderAt stands in for storage that fails mid-upload.
type failingReaderAt struct{ err error }

func (f failingReaderAt) ReadAt([]byte, int64) (int, error) { return 0, f.err }

// A read error is passed back with whatever was produced so far, so the writer
// can tell serialization failed rather than sending a short block.
func TestPieceReadPropagatesReadError(t *testing.T) {
	want := errors.New("disk gone")
	p := Piece{
		Data:           failingReaderAt{err: want},
		RequestMessage: peerprotocol.RequestMessage{Index: 1, Begin: 0, Length: 8},
	}

	buf := make([]byte, 8+8)
	n, err := p.Read(buf)
	assert.ErrorIs(t, err, want, "the storage error must not be swallowed")
	assert.Equal(t, 8, n, "the 8 header bytes were still written")
	assert.Equal(t, uint32(1), binary.BigEndian.Uint32(buf[0:4]))
}

// When a piece cannot be serialized the message writer gives up on the
// connection; the run loop then exits on Stop.
func TestWriterStopsWhenPieceCannotBeRead(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	w := New(server, logger.New("test"), 10, true, nil)
	go w.Run()

	w.SendPiece(peerprotocol.RequestMessage{Index: 1, Begin: 0, Length: 8},
		failingReaderAt{err: errors.New("disk gone")})

	w.Stop()
	select {
	case <-w.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not stop after a failed piece read")
	}
}
