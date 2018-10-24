package rpctypes

type Torrent struct {
	ID uint64
}

type ListTorrentsResponse struct {
	Torrents map[uint64]Torrent
}
