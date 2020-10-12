package piecepicker

import (
	"testing"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piece"
	"github.com/stretchr/testify/assert"
)

const (
	numPieces = 7
	numPeers  = 3
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
	pp := New(pieces, 2, nil)
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

func newPiece(i int) piece.Piece {
	return piece.Piece{Index: uint32(i)}
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
