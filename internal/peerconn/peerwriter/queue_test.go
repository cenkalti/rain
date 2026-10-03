package peerwriter

import (
	"bytes"
	"container/list"
	"net"
	"testing"
	"time"

	"github.com/cenkalti/rain/v2/internal/logger"
	"github.com/cenkalti/rain/v2/internal/peerprotocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newQueueOnlyWriter returns a writer whose Run loop is never started, so
// queueMessage and cancelRequest can be driven directly. The queue logic is
// synchronous, and exercising it through the run loop would mean racing the
// handoff to messageWriter for no extra confidence.
func newQueueOnlyWriter(t *testing.T, maxQueued int, fastEnabled bool) *PeerWriter {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() {
		server.Close()
		client.Close()
	})
	return New(server, logger.New("test"), maxQueued, fastEnabled, nil)
}

func queuedTypes(w *PeerWriter) []string {
	var out []string
	for e := w.writeQueue.Front(); e != nil; e = e.Next() {
		switch e.Value.(type) {
		case Piece:
			out = append(out, "piece")
		case peerprotocol.RejectMessage:
			out = append(out, "reject")
		case peerprotocol.ChokeMessage:
			out = append(out, "choke")
		case peerprotocol.HaveMessage:
			out = append(out, "have")
		default:
			out = append(out, "other")
		}
	}
	return out
}

func testPiece(index, begin uint32) Piece {
	return Piece{
		Data: bytes.NewReader(make([]byte, 64)),
		RequestMessage: peerprotocol.RequestMessage{
			Index: index, Begin: begin, Length: 16,
		},
	}
}

func TestQueueMessageOrdersMessages(t *testing.T) {
	w := newQueueOnlyWriter(t, 10, true)

	w.queueMessage(peerprotocol.HaveMessage{Index: 1})
	w.queueMessage(testPiece(1, 0))
	w.queueMessage(peerprotocol.HaveMessage{Index: 2})

	assert.Equal(t, []string{"have", "piece", "have"}, queuedTypes(w))
	assert.Equal(t, 1, w.currentQueuedRequests, "only pieces count toward the request budget")
}

// A peer that asks for more blocks than we are willing to buffer gets told so
// when the fast extension is available, because an unanswered request would
// otherwise leave it waiting.
func TestQueueMessageRejectsOverLimitWithFastExtension(t *testing.T) {
	w := newQueueOnlyWriter(t, 2, true)

	w.queueMessage(testPiece(1, 0))
	w.queueMessage(testPiece(2, 0))
	require.Equal(t, 2, w.currentQueuedRequests)

	w.queueMessage(testPiece(3, 0))

	assert.Equal(t, []string{"piece", "piece", "reject"}, queuedTypes(w))
	assert.Equal(t, 2, w.currentQueuedRequests, "a rejected request does not consume budget")

	// The reject must name the request it refuses.
	back := w.writeQueue.Back().Value.(peerprotocol.RejectMessage)
	assert.Equal(t, uint32(3), back.Index)
	assert.Equal(t, uint32(16), back.Length)
}

// Without the fast extension there is no reject message, so the request is
// dropped and the peer is left to time out.
func TestQueueMessageDropsOverLimitWithoutFastExtension(t *testing.T) {
	w := newQueueOnlyWriter(t, 2, false)

	w.queueMessage(testPiece(1, 0))
	w.queueMessage(testPiece(2, 0))
	w.queueMessage(testPiece(3, 0))

	assert.Equal(t, []string{"piece", "piece"}, queuedTypes(w))
	assert.Equal(t, 2, w.currentQueuedRequests)
}

// Choking a peer means we stop serving it, so anything still queued for it is
// discarded rather than sent after the choke.
func TestChokeCancelsQueuedPieces(t *testing.T) {
	w := newQueueOnlyWriter(t, 10, true)

	w.queueMessage(peerprotocol.HaveMessage{Index: 1})
	w.queueMessage(testPiece(1, 0))
	w.queueMessage(testPiece(2, 0))
	w.queueMessage(peerprotocol.HaveMessage{Index: 2})
	require.Equal(t, 2, w.currentQueuedRequests)

	w.queueMessage(peerprotocol.ChokeMessage{})

	assert.Equal(t, []string{"have", "have", "choke"}, queuedTypes(w),
		"the pieces are gone but the other messages keep their order")
	assert.Zero(t, w.currentQueuedRequests, "the budget is released")
}

func TestChokeWithNothingQueued(t *testing.T) {
	w := newQueueOnlyWriter(t, 10, true)
	w.queueMessage(peerprotocol.ChokeMessage{})
	assert.Equal(t, []string{"choke"}, queuedTypes(w))
	assert.Zero(t, w.currentQueuedRequests)
}

func TestCancelRequestRemovesMatchingPiece(t *testing.T) {
	w := newQueueOnlyWriter(t, 10, true)
	w.queueMessage(testPiece(1, 0))
	w.queueMessage(testPiece(2, 0))
	w.queueMessage(testPiece(3, 0))
	require.Equal(t, 3, w.currentQueuedRequests)

	w.cancelRequest(peerprotocol.CancelMessage{
		RequestMessage: peerprotocol.RequestMessage{Index: 2, Begin: 0, Length: 16},
	})

	assert.Len(t, queuedTypes(w), 2)
	assert.Equal(t, 2, w.currentQueuedRequests)
	for e := w.writeQueue.Front(); e != nil; e = e.Next() {
		assert.NotEqual(t, uint32(2), e.Value.(Piece).Index, "the cancelled piece is gone")
	}
}

// All three fields have to match: a cancel is for one specific block, and
// dropping the wrong one would stall the transfer.
func TestCancelRequestIgnoresPartialMatches(t *testing.T) {
	cases := []struct {
		name  string
		req   peerprotocol.RequestMessage
		match bool
	}{
		{"exact", peerprotocol.RequestMessage{Index: 1, Begin: 32, Length: 16}, true},
		{"wrong index", peerprotocol.RequestMessage{Index: 9, Begin: 32, Length: 16}, false},
		{"wrong begin", peerprotocol.RequestMessage{Index: 1, Begin: 64, Length: 16}, false},
		{"wrong length", peerprotocol.RequestMessage{Index: 1, Begin: 32, Length: 8}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newQueueOnlyWriter(t, 10, true)
			w.queueMessage(Piece{
				Data:           bytes.NewReader(make([]byte, 64)),
				RequestMessage: peerprotocol.RequestMessage{Index: 1, Begin: 32, Length: 16},
			})

			w.cancelRequest(peerprotocol.CancelMessage{RequestMessage: c.req})

			if c.match {
				assert.Empty(t, queuedTypes(w))
				assert.Zero(t, w.currentQueuedRequests)
			} else {
				assert.Equal(t, []string{"piece"}, queuedTypes(w))
				assert.Equal(t, 1, w.currentQueuedRequests)
			}
		})
	}
}

// Only the first match is removed, so two identical queued requests need two
// cancels.
func TestCancelRequestRemovesOnlyOneMatch(t *testing.T) {
	w := newQueueOnlyWriter(t, 10, true)
	w.queueMessage(testPiece(1, 0))
	w.queueMessage(testPiece(1, 0))

	w.cancelRequest(peerprotocol.CancelMessage{
		RequestMessage: peerprotocol.RequestMessage{Index: 1, Begin: 0, Length: 16},
	})

	assert.Equal(t, []string{"piece"}, queuedTypes(w))
	assert.Equal(t, 1, w.currentQueuedRequests)
}

func TestCancelRequestWithEmptyQueue(t *testing.T) {
	w := newQueueOnlyWriter(t, 10, true)
	w.cancelRequest(peerprotocol.CancelMessage{})
	assert.Empty(t, queuedTypes(w))
	assert.Zero(t, w.currentQueuedRequests)
}

// A cancel must not disturb non-piece messages sitting in the queue.
func TestCancelRequestLeavesOtherMessages(t *testing.T) {
	w := newQueueOnlyWriter(t, 10, true)
	w.queueMessage(peerprotocol.HaveMessage{Index: 1})
	w.queueMessage(peerprotocol.HaveMessage{Index: 2})

	w.cancelRequest(peerprotocol.CancelMessage{
		RequestMessage: peerprotocol.RequestMessage{Index: 1},
	})

	assert.Equal(t, []string{"have", "have"}, queuedTypes(w))
}

// countUploadBytes reports payload only: the 13 bytes of length prefix, id and
// piece header are protocol overhead, not uploaded content.
func TestCountUploadBytesExcludesOverhead(t *testing.T) {
	w := newQueueOnlyWriter(t, 10, true)

	go w.countUploadBytes(13 + 100)

	// Messages is unbuffered, so receive directly rather than racing a relay
	// goroutine's handoff.
	select {
	case msg := <-w.Messages():
		assert.Equal(t, BlockUploaded{Length: 100}, msg)
	case <-time.After(5 * time.Second):
		t.Fatal("no BlockUploaded reported")
	}
}

// A write shorter than the overhead means no payload went out, so there is
// nothing to report and the send must be skipped rather than underflow.
func TestCountUploadBytesSkipsWhenNoPayload(t *testing.T) {
	for _, n := range []int{0, 5, 13} {
		w := newQueueOnlyWriter(t, 10, true)
		done := make(chan struct{})
		go func() {
			w.countUploadBytes(n)
			close(done)
		}()
		select {
		case <-done:
		case msg := <-w.Messages():
			t.Fatalf("n=%d reported %v with no payload written", n, msg)
		}
	}
}

func TestWriteQueueStartsEmpty(t *testing.T) {
	w := newQueueOnlyWriter(t, 10, true)
	assert.Equal(t, list.New().Len(), w.writeQueue.Len())
	assert.Zero(t, w.currentQueuedRequests)
}
