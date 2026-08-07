package piecepicker

import (
	"testing"

	"github.com/cenkalti/rain/v2/internal/bitfield"
	"github.com/cenkalti/rain/v2/internal/filesection"
	"github.com/cenkalti/rain/v2/internal/peer"
	"github.com/cenkalti/rain/v2/internal/piece"
	"github.com/stretchr/testify/assert"
)

const (
	numPieces   = 7
	numPeers    = 3
	pieceLength = 16 * 1024
)

func TestPiecePicker(t *testing.T) {
	pieces := make([]piece.Piece, numPieces)
	for i := range pieces {
		pieces[i] = newPiece(i)
	}
	peers := make([]*peer.Peer, numPeers)
	for i := range peers {
		peers[i] = newPeer(i)
	}
	pieces[0].Done = true
	pieces[2].Done = true
	pieces[3].Done = true
	pp := New(pieces, 2, nil, false)
	pp.HandleHave(peers[0], 1)
	pp.HandleHave(peers[0], 3)
	pp.HandleHave(peers[0], 4)
	pp.HandleHave(peers[1], 1)
	pp.HandleHave(peers[2], 5)

	assert.Equal(t, &pieces[4], pp.pickFor(peers[0]))
	assert.False(t, pp.endgame)

	assert.Equal(t, &pieces[1], pp.pickFor(peers[1]))
	assert.False(t, pp.endgame)

	assert.Equal(t, &pieces[5], pp.pickFor(peers[2]))
	assert.False(t, pp.endgame)

	peers = append(peers, newPeer(3))
	pp.HandleHave(peers[3], 5)
	assert.Nil(t, pp.pickFor(peers[3]))
	assert.False(t, pp.endgame)

	pp.HandleSnubbed(peers[2], 5)
	assert.Equal(t, &pieces[5], pp.pickFor(peers[3]))
	assert.False(t, pp.endgame)

	peers = append(peers, newPeer(4))
	pp.HandleHave(peers[4], 6)
	assert.Equal(t, &pieces[6], pp.pickFor(peers[4]))
	assert.False(t, pp.endgame)

	peers = append(peers, newPeer(5))
	pp.HandleHave(peers[5], 0)
	pp.HandleHave(peers[5], 5)
	pp.HandleHave(peers[5], 6)
	assert.Equal(t, &pieces[6], pp.pickFor(peers[5]))
	assert.True(t, pp.endgame)

	peers = append(peers, newPeer(6))
	pp.HandleHave(peers[6], 6)
	assert.Nil(t, pp.pickFor(peers[6]))
	assert.True(t, pp.endgame)
}

func TestPiecePickerSequential(t *testing.T) {
	newPicker := func(sequential bool) (*PiecePicker, []piece.Piece, []*peer.Peer) {
		pieces := make([]piece.Piece, numPieces)
		for i := range pieces {
			pieces[i] = newPiece(i)
		}
		peers := make([]*peer.Peer, numPeers)
		for i := range peers {
			peers[i] = newPeer(i)
		}
		pieces[0].Done = true
		pp := New(pieces, 2, nil, sequential)
		for _, pe := range peers {
			pp.HandleHave(pe, 1)
			pp.HandleHave(pe, 2)
			pp.HandleHave(pe, 3)
		}
		// Piece 6 is the rarest piece that peers[0] can download.
		pp.HandleHave(peers[0], 6)
		return pp, pieces, peers
	}

	// Rarest-first prefers the rare high indexed piece.
	pp, pieces, peers := newPicker(false)
	assert.Equal(t, &pieces[6], pp.pickFor(peers[0]))

	// Sequential mode ignores rarity. Piece 0 is done, so the last piece comes first,
	// then the remaining pieces in index order.
	pp, pieces, peers = newPicker(true)
	assert.Equal(t, &pieces[6], pp.pickFor(peers[0]))
	assert.Equal(t, &pieces[1], pp.pickFor(peers[1]))
	assert.Equal(t, &pieces[2], pp.pickFor(peers[2]))
	assert.False(t, pp.endgame)

	// Pieces 4 and 5 are unavailable, so piece 3 is all that is left for peers[0].
	assert.Equal(t, &pieces[3], pp.pickFor(peers[0]))
	assert.False(t, pp.endgame)
}

func TestPiecePickerSequentialOrder(t *testing.T) {
	pieces := make([]piece.Piece, numPieces)
	for i := range pieces {
		pieces[i] = newPiece(i)
	}
	pe := newPeer(0)
	pp := New(pieces, 2, nil, true)
	for i := range pieces {
		pp.HandleHave(pe, uint32(i))
	}
	// First piece, then last piece, then the rest in index order.
	for _, i := range []int{0, 6, 1, 2, 3, 4, 5} {
		assert.Equal(t, &pieces[i], pp.pickFor(pe))
	}
}

func TestPiecePickerSequentialOrderMultiFile(t *testing.T) {
	// File "a" spans pieces 0-2 and file "b" spans pieces 3-6.
	pieces := make([]piece.Piece, numPieces)
	for i := range pieces {
		if i < 3 {
			pieces[i] = newFilePiece(i, "a", int64(i)*pieceLength)
		} else {
			pieces[i] = newFilePiece(i, "b", int64(i-3)*pieceLength)
		}
	}
	pe := newPeer(0)
	pp := New(pieces, 2, nil, true)
	for i := range pieces {
		pp.HandleHave(pe, uint32(i))
	}
	// Both ends of both files, then the rest in index order.
	for _, i := range []int{0, 2, 3, 6, 1, 4, 5} {
		assert.Equal(t, &pieces[i], pp.pickFor(pe))
	}
}

func TestPiecePickerSequentialFileEdges(t *testing.T) {
	// The file is 3 MiB long, so 1% of it spans two pieces at both ends.
	const n = 200
	pieces := make([]piece.Piece, n)
	for i := range pieces {
		pieces[i] = newPiece(i)
	}
	pe := &peer.Peer{Bitfield: bitfield.New(n)}
	pp := New(pieces, 2, nil, true)
	for i := range pieces {
		pp.HandleHave(pe, uint32(i))
	}
	for _, i := range []int{0, 1, 198, 199, 2, 3} {
		assert.Equal(t, &pieces[i], pp.pickFor(pe))
	}
}

func TestPiecePickerSequentialAllowedFast(t *testing.T) {
	pieces := make([]piece.Piece, numPieces)
	for i := range pieces {
		pieces[i] = newPiece(i)
	}
	pe := newPeer(0)
	pp := New(pieces, 2, nil, true)
	for i := range pieces {
		pp.HandleHave(pe, uint32(i))
	}
	// Piece 3 is in the middle of the file. The peer allows us to request it while choking.
	pp.HandleAllowedFast(pe, 3)

	// While choked, the allowed-fast piece is the only piece we can request.
	pe.PeerChoking = true
	pi, allowedFast := pp.PickFor(pe)
	assert.Equal(t, &pieces[3], pi)
	assert.True(t, allowedFast)

	// After the peer unchokes us, the pieces at file edges come first.
	pe.PeerChoking = false
	pi, allowedFast = pp.PickFor(pe)
	assert.Equal(t, &pieces[0], pi)
	assert.False(t, allowedFast)
}

func TestFileEdgeSize(t *testing.T) {
	assert.Equal(t, int64(1), fileEdgeSize(0))
	assert.Equal(t, int64(1), fileEdgeSize(99))
	assert.Equal(t, int64(1024), fileEdgeSize(100*1024))
	assert.Equal(t, int64(maxFileEdgeSize), fileEdgeSize(10*1024*1024*1024))
}

func TestMarkFileEdges(t *testing.T) {
	// Piece 1 holds the end of file "a", a padding file and the beginning of file "b".
	pieces := []piece.Piece{
		newFilePiece(0, "a", 0),
		{Index: 1, Length: pieceLength, Data: filesection.Piece{
			{Name: "a", Offset: pieceLength, Length: pieceLength / 2},
			{Name: "pad", Length: pieceLength / 4, Padding: true},
			{Name: "b", Length: pieceLength / 4},
		}},
		newFilePiece(2, "b", pieceLength/4),
	}
	pp := New(pieces, 2, nil, true)
	assert.Equal(t, []bool{true, true, false}, []bool{pp.pieces[0].FileHead, pp.pieces[1].FileHead, pp.pieces[2].FileHead})
	assert.Equal(t, []bool{false, true, true}, []bool{pp.pieces[0].FileTail, pp.pieces[1].FileTail, pp.pieces[2].FileTail})
}

// newPiece returns a piece of a torrent that contains a single file.
func newPiece(i int) piece.Piece {
	return newFilePiece(i, "file", int64(i)*pieceLength)
}

// newFilePiece returns a piece that holds a single section of the named file.
func newFilePiece(i int, name string, offset int64) piece.Piece {
	return piece.Piece{
		Index:  uint32(i),
		Length: pieceLength,
		Data:   filesection.Piece{{Name: name, Offset: offset, Length: pieceLength}},
	}
}

func newPeer(i int) *peer.Peer {
	return &peer.Peer{
		ID:       [20]byte{byte(i)},
		Bitfield: bitfield.New(numPieces),
	}
}

func (p *PiecePicker) pickFor(pe *peer.Peer) *piece.Piece {
	pi, _ := p.PickFor(pe)
	return pi
}
