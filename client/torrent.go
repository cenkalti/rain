package client

import (
	"github.com/cenkalti/rain/torrent"
)

type Torrent struct {
	ID       uint64
	Name     string
	InfoHash string
	torrent  *torrent.Torrent
}

func (t *Torrent) Stats() torrent.Stats {
	return t.torrent.Stats()
}

func (t *Torrent) Start() {
	t.torrent.Start()
}

func (t *Torrent) Stop() {
	t.torrent.Stop()
}
