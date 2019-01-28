package rainrpc

type Torrent struct {
	ID        string
	Name      string
	InfoHash  string
	Port      uint16
	CreatedAt Time
}

type Peer struct {
	Addr string
}

type Tracker struct {
	URL      string
	Status   string
	Leechers int
	Seeders  int
	Error    *string
}

type Stats struct {
	Status string
	Error  *string
	Pieces struct {
		Have      uint32
		Missing   uint32
		Available uint32
		Total     uint32
	}
	Bytes struct {
		Total      int64
		Allocated  int64
		Complete   int64
		Incomplete int64
		Downloaded int64
		Uploaded   int64
		Wasted     int64
	}
	Peers struct {
		Total    int
		Incoming int
		Outgoing int
	}
	Handshakes struct {
		Total    int
		Incoming int
		Outgoing int
	}
	Addresses struct {
		Total   int
		Tracker int
		DHT     int
		PEX     int
	}
	Downloads struct {
		Total   int
		Running int
		Snubbed int
		Choked  int
	}
	MetadataDownloads struct {
		Total   int
		Snubbed int
		Running int
	}
	Name        string
	Private     bool
	PieceLength uint32
}

type ListTorrentsRequest struct {
}

type ListTorrentsResponse struct {
	Torrents []Torrent
}

type AddTorrentRequest struct {
	Torrent string
}

type AddTorrentResponse struct {
	Torrent Torrent
}

type AddURIRequest struct {
	URI string
}

type AddURIResponse struct {
	Torrent Torrent
}

type RemoveTorrentRequest struct {
	ID string
}

type RemoveTorrentResponse struct {
}

type GetTorrentStatsRequest struct {
	ID string
}

type GetTorrentStatsResponse struct {
	Stats Stats
}

type GetTorrentTrackersRequest struct {
	ID string
}

type GetTorrentTrackersResponse struct {
	Trackers []Tracker
}

type GetTorrentPeersRequest struct {
	ID string
}

type GetTorrentPeersResponse struct {
	Peers []Peer
}

type StartTorrentRequest struct {
	ID string
}

type StartTorrentResponse struct {
}

type StopTorrentRequest struct {
	ID string
}

type StopTorrentResponse struct {
}
