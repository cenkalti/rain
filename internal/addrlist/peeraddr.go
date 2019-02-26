package addrlist

import (
	"net"

	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peerpriority"
	"github.com/google/btree"
)

type peerAddr struct {
	addr     *net.TCPAddr
	source   peer.Source
	priority peerpriority.Priority
}

var _ btree.Item = (*peerAddr)(nil)

func (p *peerAddr) Less(than btree.Item) bool {
	return p.priority < than.(*peerAddr).priority
}
