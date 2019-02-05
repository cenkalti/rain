package piecepicker_test

import (
	"math/rand"
	"testing"

	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piece"
	"github.com/cenkalti/rain/internal/piecepicker"
)

const endgameParallelDownloadsPerPiece = 4
const allowedFast = 10
const snubRatio = 0.1
const seedRatio = 0.5
const haveRatio = 0.5

func benchmarkPick(numPieces uint32, numPeers int, b *testing.B) {
	pp := newPiecePicker(numPieces, numPeers)
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		pp.Pick()
	}
}

func newPiecePicker(numPieces uint32, numPeers int) *piecepicker.PiecePicker {
	pieces := make([]piece.Piece, numPieces)
	var id [20]byte
	var ext [8]byte
	pp := piecepicker.New(pieces, endgameParallelDownloadsPerPiece, nil)
	for i := 0; i < numPeers; i++ {
		pe := peer.New(nil, 0, id, ext, 0, 0)
		if prob(snubRatio) {
			pe.Snubbed = true
		}
		if prob(seedRatio) {
			for j := uint32(0); j < numPieces; j++ {
				pp.HandleHave(pe, j)
			}
		} else {
			for j := uint32(0); j < numPieces; j++ {
				if prob(haveRatio) {
					pp.HandleHave(pe, j)
				}
			}
		}
		for j := 0; j < allowedFast; j++ {
			pi := rand.Intn(int(numPieces))
			pp.HandleAllowedFast(pe, uint32(pi))
		}
	}
	return pp
}

func BenchmarkPick1000Pieces50Peers(b *testing.B) {
	benchmarkPick(1000, 50, b)
}

func prob(ratio float64) bool {
	n := rand.Float64()
	return n < ratio
}
