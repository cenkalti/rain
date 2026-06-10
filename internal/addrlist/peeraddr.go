package addrlist

import (
	"net"
	"time"

	"github.com/cenkalti/rain/v2/internal/peerpriority"
	"github.com/cenkalti/rain/v2/internal/peersource"
	"github.com/google/btree"
)

type peerAddr struct {
	addr      *net.TCPAddr
	timestamp time.Time
	source    peersource.Source
	priority  peerpriority.Priority

	// index in AddrList.peerByTime slice
	index int
}

var _ btree.Item = (*peerAddr)(nil)

func (p *peerAddr) Less(than btree.Item) bool {
	return p.priority < than.(*peerAddr).priority
}
