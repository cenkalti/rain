package torrent

import "time"

type SessionStats struct {
	Torrents                      int
	AvailablePorts                int
	BlockListRules                int
	BlockListLastSuccessfulUpdate time.Time
	PieceCacheItems               int
	PieceCacheSize                int64
	ActivePieceBytes              int64
	TorrentsPendingRAM            int
}
