package rpctypes

type Torrent struct {
	ID       string
	Name     string
	InfoHash string
	Port     uint16
	AddedAt  Time
}

type Peer struct {
	ID                 string
	Addr               string
	Source             string
	ConnectedAt        Time
	Downloading        bool
	ClientInterested   bool
	ClientChoking      bool
	PeerInterested     bool
	PeerChoking        bool
	OptimisticUnchoked bool
	Snubbed            bool
	EncryptedHandshake bool
	EncryptedStream    bool
	DownloadSpeed      uint
	UploadSpeed        uint
}

type Tracker struct {
	URL      string
	Status   string
	Leechers int
	Seeders  int
	Error    *string
}

type SessionStats struct {
	Torrents                      int
	AvailablePorts                int
	BlockListRules                int
	BlockListLastSuccessfulUpdate *Time
	PieceCacheItems               int
	PieceCacheSize                int64
	PieceCacheUtilization         int
	ReadsPerSecond                int
	ReadsPending                  int
	ReadBytesPerSecond            int
	ActivePieceBytes              int64
	TorrentsPendingRAM            int
	Uptime                        int
}

type Stats struct {
	Status string
	Error  *string
	Pieces struct {
		Checked   uint32
		Have      uint32
		Missing   uint32
		Available uint32
		Total     uint32
	}
	Bytes struct {
		Total      int64
		Allocated  int64
		Completed  int64
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
	SeededFor   uint
	Speed       struct {
		Download uint
		Upload   uint
	}
	ETA *uint
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

type GetSessionStatsRequest struct {
}

type GetSessionStatsResponse struct {
	Stats SessionStats
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

type AddPeerRequest struct {
	ID   string
	Addr string
}

type AddPeerResponse struct {
}

type AddTrackerRequest struct {
	ID  string
	URL string
}

type AddTrackerResponse struct {
}

type StartAllTorrentsRequest struct {
}

type StartAllTorrentsResponse struct {
}

type StopAllTorrentsRequest struct {
}

type StopAllTorrentsResponse struct {
}
