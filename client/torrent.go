package client

import (
	"github.com/cenkalti/rain/torrent"
)

type Torrent struct {
	ID uint64
	*torrent.Torrent
}
