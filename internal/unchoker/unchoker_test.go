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
	u := New(2, 1)

	// Must unchoke fastest downloading 2 peers
	u.round = 1
	u.TickUnchoke(getPeers(), false)
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
	u.round = 1
	u.TickUnchoke(getPeers(), false)
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
	u.round = 0
	u.TickUnchoke(getPeers(), false)
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
	testPeers[0].downloadSpeed = 3
	u.round = 1
	u.TickUnchoke(getPeers(), false)
	assert.Equal(t, []*TestPeer{
		{
			interested:    true,
			optimistic:    true,
			downloadSpeed: 3,
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

	u.round = 0
	u.TickUnchoke(getPeers(), false)
	assert.Equal(t, []*TestPeer{
		{
			interested:    true,
			downloadSpeed: 3,
		},
		{
			interested:    true,
			downloadSpeed: 2,
			optimistic:    true,
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
	downloadSpeed int
	uploadSpeed   int
}

func (p *TestPeer) Choke()                   { p.choking = true }
func (p *TestPeer) Unchoke()                 { p.choking = false }
func (p *TestPeer) Choking() bool            { return p.choking }
func (p *TestPeer) Interested() bool         { return p.interested }
func (p *TestPeer) Optimistic() bool         { return p.optimistic }
func (p *TestPeer) SetOptimistic(value bool) { p.optimistic = value }
func (p *TestPeer) DownloadSpeed() int       { return p.downloadSpeed }
func (p *TestPeer) UploadSpeed() int         { return p.uploadSpeed }
