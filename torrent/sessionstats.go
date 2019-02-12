package torrent

import "time"

type SessionStats struct {
	Torrents                      int
	AvailablePorts                int
	BlockListRules                int
	BlockListLastSuccessfulUpdate time.Time
	PieceCacheItems               int
	PieceCacheSize                int
	ActivePieceBytes              int
}
