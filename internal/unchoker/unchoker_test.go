package unchoker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTickUnchoke(t *testing.T) {
	testPeers := []*TestPeer{
		{
			interested: true,
			choking:    true,
		},
		{
			interested:    true,
			choking:       true,
			downloadSpeed: 2,
		},
		{
			interested:    true,
			choking:       true,
			downloadSpeed: 4,
		},
		{
			choking: true,
		},
	}
	getPeers := func() []Peer {
		peers := make([]Peer, len(testPeers))
		for i := range peers {
			peers[i] = testPeers[i]
		}
		return peers
	}
	u := New(getPeers, new(bool), 2, 1)

	// Must unchoke fastest downloading 2 peers
	u.TickUnchoke()
	assert.Equal(t, []*TestPeer{
		{
			interested: true,
			choking:    true,
		},
		{
			interested:    true,
			downloadSpeed: 2,
		},
		{
			interested:    true,
			downloadSpeed: 4,
		},
		{
			choking: true,
		},
	}, testPeers)

	// Nothing has changed. Same peers stays unchoked.
	u.TickUnchoke()
	assert.Equal(t, []*TestPeer{
		{
			interested: true,
			choking:    true,
		},
		{
			interested:    true,
			downloadSpeed: 2,
		},
		{
			interested:    true,
			downloadSpeed: 4,
		},
		{
			choking: true,
		},
	}, testPeers)

	// First choked peer must be unchoked optimistically.
	u.TickOptimisticUnchoke()
	assert.Equal(t, []*TestPeer{
		{
			interested: true,
			optimistic: true,
		},
		{
			interested:    true,
			downloadSpeed: 2,
		},
		{
			interested:    true,
			downloadSpeed: 4,
		},
		{
			choking: true,
		},
	}, testPeers)

	// Optimistically unchoked peer has started downloading,
	// regular unchoke must be applied on that peer.
	testPeers[0].downloadSpeed = 3
	u.TickUnchoke()
	assert.Equal(t, []*TestPeer{
		{
			downloadSpeed: 3,
			interested:    true,
		},
		{
			interested:    true,
			downloadSpeed: 2,
			choking:       true,
		},
		{
			interested:    true,
			downloadSpeed: 4,
		},
		{
			choking: true,
		},
	}, testPeers)
}

type TestPeer struct {
	interested    bool
	choking       bool
	optimistic    bool
	downloadSpeed uint
	uploadSpeed   uint
}

func (p *TestPeer) Choke()                   { p.choking = true }
func (p *TestPeer) Unchoke()                 { p.choking = false }
func (p *TestPeer) Choking() bool            { return p.choking }
func (p *TestPeer) Interested() bool         { return p.interested }
func (p *TestPeer) Optimistic() bool         { return p.optimistic }
func (p *TestPeer) SetOptimistic(value bool) { p.optimistic = value }
func (p *TestPeer) DownloadSpeed() uint      { return p.downloadSpeed }
func (p *TestPeer) UploadSpeed() uint        { return p.uploadSpeed }
