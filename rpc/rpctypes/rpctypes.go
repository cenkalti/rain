package rpctypes

type Torrent struct {
	ID       uint64
	Name     string
	InfoHash string
}

type ListTorrentsRequest struct{}

type ListTorrentsResponse struct {
	Torrents map[uint64]Torrent
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
