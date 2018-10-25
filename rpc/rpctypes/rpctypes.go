package rpctypes

import (
	"github.com/cenkalti/rain/torrent"
)

type Torrent struct {
	ID       uint64
	Name     string
	InfoHash string
}

type ListTorrentsRequest struct{}

type ListTorrentsResponse struct {
	Torrents []Torrent
}

type AddTorrentRequest struct {
	Torrent string
}

type AddMagnetRequest struct {
	Magnet string
}

type AddTorrentResponse struct {
	Torrent Torrent
}

type RemoveTorrentRequest struct {
	ID uint64
}

type RemoveTorrentResponse struct{}

type GetTorrentStatsRequest struct {
	ID uint64
}

type GetTorrentStatsResponse struct {
	Stats torrent.Stats
}

type StartTorrentRequest struct {
	ID uint64
}

type StartTorrentResponse struct {
}

type StopTorrentRequest struct {
	ID uint64
}

type StopTorrentResponse struct {
}
