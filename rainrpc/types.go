// Package rainrpc provides a RPC client implementation for communicating with Rain session.
package rainrpc

type Torrent struct {
	ID       uint64 `json:"id"`
	Name     string `json:"name"`
	InfoHash string `json:"info_hash"`
	Port     uint16 `json:"port"`
}

type Peer struct {
	Addr string `json:"addr"`
}

type Tracker struct {
	URL      string  `json:"url"`
	Status   string  `json:"status"`
	Leechers int     `json:"leechers"`
	Seeders  int     `json:"seeders"`
	Error    *string `json:"error"`
}

type Stats struct {
	Status string  `json:"status"`
	Error  *string `json:"error"`
	Pieces struct {
		Have      uint32 `json:"have"`
		Missing   uint32 `json:"missing"`
		Available uint32 `json:"available"`
		Total     uint32 `json:"total"`
	} `json:"pieces"`
	Bytes struct {
		Complete   int64 `json:"complete"`
		Incomplete int64 `json:"incomplete"`
		Total      int64 `json:"total"`
		Downloaded int64 `json:"downloaded"`
		Uploaded   int64 `json:"uploaded"`
		Wasted     int64 `json:"wasted"`
	} `json:"bytes"`
	Peers struct {
		Total    int `json:"total"`
		Incoming int `json:"incoming"`
		Outgoing int `json:"outgoing"`
	} `json:"peers"`
	Handshakes struct {
		Total    int `json:"total"`
		Incoming int `json:"incoming"`
		Outgoing int `json:"outgoing"`
	} `json:"handshakes"`
	ReadyAddresses int `json:"ready_addresses"`
	Downloads      struct {
		Total   int `json:"total"`
		Running int `json:"running"`
		Snubbed int `json:"snubbed"`
		Choked  int `json:"choked"`
	} `json:"downloads"`
	MetadataDownloads struct {
		Total   int `json:"total"`
		Snubbed int `json:"snubbed"`
		Running int `json:"running"`
	} `json:"metadata_downloads"`
	Name        string `json:"name"`
	Private     bool   `json:"private"`
	PieceLength uint32 `json:"piece_length"`
}

type ListTorrentsRequest struct {
}

type ListTorrentsResponse struct {
	Torrents []Torrent `json:"torrents"`
}

type AddTorrentRequest struct {
	Torrent string `json:"torrent"`
}

type AddTorrentResponse struct {
	Torrent Torrent `json:"torrent"`
}

type AddURIRequest struct {
	URI string `json:"uri"`
}

type AddURIResponse struct {
	Torrent Torrent `json:"torrent"`
}

type RemoveTorrentRequest struct {
	ID uint64 `json:"id"`
}

type RemoveTorrentResponse struct {
}

type GetTorrentStatsRequest struct {
	ID uint64 `json:"id"`
}

type GetTorrentStatsResponse struct {
	Stats Stats `json:"stats"`
}

type GetTorrentTrackersRequest struct {
	ID uint64 `json:"id"`
}

type GetTorrentTrackersResponse struct {
	Trackers []Tracker `json:"trackers"`
}

type GetTorrentPeersRequest struct {
	ID uint64 `json:"id"`
}

type GetTorrentPeersResponse struct {
	Peers []Peer `json:"peers"`
}

type StartTorrentRequest struct {
	ID uint64 `json:"id"`
}

type StartTorrentResponse struct {
}

type StopTorrentRequest struct {
	ID uint64 `json:"id"`
}

type StopTorrentResponse struct {
}
