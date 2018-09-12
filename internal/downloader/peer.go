package downloader

import (
	"github.com/cenkalti/rain/internal/peer"
)

type Peer struct {
	*peer.Peer
	amChoking                    bool
	amInterested                 bool
	peerChoking                  bool
	peerInterested               bool
	bytesDownlaodedInChokePeriod int64
	optimisticUnhoked            bool
}

type ByDownloadRate []*Peer

func (a ByDownloadRate) Len() int      { return len(a) }
func (a ByDownloadRate) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByDownloadRate) Less(i, j int) bool {
	return a[i].bytesDownlaodedInChokePeriod > a[j].bytesDownlaodedInChokePeriod
}
