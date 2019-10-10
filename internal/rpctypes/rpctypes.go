package rpctypes

// Torrent in a Session.
type Torrent struct {
	ID       string
	Name     string
	InfoHash string
	Port     int
	AddedAt  Time
}

// Peer of a Torrent.
type Peer struct {
	ID                 string
	Client             string
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
	DownloadSpeed      int
	UploadSpeed        int
}

// Webseed source of a Torrent.
type Webseed struct {
	URL           string
	Error         string
	DownloadSpeed int
}

// Tracker of a Torrent.
type Tracker struct {
	URL           string
	Status        string
	Leechers      int
	Seeders       int
	Warning       string
	Error         string
	ErrorUnknown  bool
	ErrorInternal string
	LastAnnounce  Time
	NextAnnounce  Time
}

// SessionStats contains statistics about a Session.
type SessionStats struct {
	Uptime         int
	Torrents       int
	Peers          int
	PortsAvailable int

	BlockListRules   int
	BlockListRecency int

	ReadCacheObjects     int
	ReadCacheSize        int64
	ReadCacheUtilization int

	ReadsPerSecond int
	ReadsActive    int
	ReadsPending   int

	WriteCacheObjects     int
	WriteCacheSize        int64
	WriteCachePendingKeys int

	WritesPerSecond int
	WritesActive    int
	WritesPending   int

	SpeedDownload int
	SpeedUpload   int
	SpeedRead     int
	SpeedWrite    int
}

// Stats contains statistics about a Torrent.
type Stats struct {
	InfoHash string
	Port     int
	Status   string
	Error    string
	Pieces   struct {
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
		Download int
		Upload   int
	}
	ETA int
}

// GetMagnetRequest contains request arguments for Session.GetMagnet method.
type GetMagnetRequest struct {
	ID string
}

// GetMagnetResponse contains response arguments for Session.GetMagnet method.
type GetMagnetResponse struct {
	Magnet string
}

// GetTorrentRequest contains request arguments for Session.GetTorrent method.
type GetTorrentRequest struct {
	ID string
}

// GetTorrentResponse contains response arguments for Session.GetTorrent method.
type GetTorrentResponse struct {
	Torrent string
}

// ListTorrentsRequest contains request arguments for Session.ListTorrents method.
type ListTorrentsRequest struct {
}

// ListTorrentsResponse contains response arguments for Session.ListTorrents method.
type ListTorrentsResponse struct {
	Torrents []Torrent
}

// AddTorrentOptions contains options for adding a new torrent.
type AddTorrentOptions struct {
	ID                string
	Stopped           bool
	StopAfterDownload bool
}

// AddTorrentRequest contains request arguments for Session.AddTorrent method.
type AddTorrentRequest struct {
	Torrent string
	AddTorrentOptions
}

// AddTorrentResponse contains response arguments for Session.AddTorrent method.
type AddTorrentResponse struct {
	Torrent Torrent
}

// AddURIRequest contains request arguments for Session.AddURI method.
type AddURIRequest struct {
	URI string
	AddTorrentOptions
}

// AddURIResponse contains response arguments for Session.AddURI method.
type AddURIResponse struct {
	Torrent Torrent
}

// RemoveTorrentRequest contains request arguments for Session.RemoveTorrent method.
type RemoveTorrentRequest struct {
	ID string
}

// RemoveTorrentResponse contains response arguments for Session.RemoveTorrent method.
type RemoveTorrentResponse struct {
}

// CleanDatabaseRequest contains request arguments for Session.CleanDatabase method.
type CleanDatabaseRequest struct {
}

// CleanDatabaseResponse contains response arguments for Session.CleanDatabase method.
type CleanDatabaseResponse struct {
}

// GetSessionStatsRequest contains request arguments for Session.GetSessionStats method.
type GetSessionStatsRequest struct {
}

// GetSessionStatsResponse contains response arguments for Session.GetSessionStats method.
type GetSessionStatsResponse struct {
	Stats SessionStats
}

// GetTorrentStatsRequest contains request arguments for Session.GetTorrentStats method.
type GetTorrentStatsRequest struct {
	ID string
}

// GetTorrentStatsResponse contains response arguments for Session.GetTorrentStats method.
type GetTorrentStatsResponse struct {
	Stats Stats
}

// GetTorrentTrackersRequest contains request arguments for Session.GetTorrentTrackers method.
type GetTorrentTrackersRequest struct {
	ID string
}

// GetTorrentTrackersResponse contains response arguments for Session.GetTorrentTrackers method.
type GetTorrentTrackersResponse struct {
	Trackers []Tracker
}

// GetTorrentPeersRequest contains request arguments for Session.GetTorrentPeers method.
type GetTorrentPeersRequest struct {
	ID string
}

// GetTorrentPeersResponse contains response arguments for Session.GetTorrentPeers method.
type GetTorrentPeersResponse struct {
	Peers []Peer
}

// GetTorrentWebseedsRequest contains request arguments for Session.GetTorrentWebseeds method.
type GetTorrentWebseedsRequest struct {
	ID string
}

// GetTorrentWebseedsResponse contains response arguments for Session.GetTorrentWebseeds method.
type GetTorrentWebseedsResponse struct {
	Webseeds []Webseed
}

// StartTorrentRequest contains request arguments for Session.StartTorrent method.
type StartTorrentRequest struct {
	ID string
}

// StartTorrentResponse contains response arguments for Session.StartTorrent method.
type StartTorrentResponse struct {
}

// StopTorrentRequest contains request arguments for Session.StopTorrent method.
type StopTorrentRequest struct {
	ID string
}

// StopTorrentResponse contains response arguments for Session.StopTorrent method.
type StopTorrentResponse struct {
}

// AnnounceTorrentRequest contains request arguments for Session.AnnounceTorrent method.
type AnnounceTorrentRequest struct {
	ID string
}

// AnnounceTorrentResponse contains response arguments for Session.AnnounceTorrent method.
type AnnounceTorrentResponse struct {
}

// VerifyTorrentRequest contains request arguments for Session.VerifyTorrent method.
type VerifyTorrentRequest struct {
	ID string
}

// VerifyTorrentResponse contains response arguments for Session.VerifyTorrent method.
type VerifyTorrentResponse struct {
}

// MoveTorrentRequest contains request arguments for Session.MoveTorrent method.
type MoveTorrentRequest struct {
	ID     string
	Target string
}

// MoveTorrentResponse contains response arguments for Session.MoveTorrent method.
type MoveTorrentResponse struct {
}

// AddPeerRequest contains request arguments for Session.AddPeer method.
type AddPeerRequest struct {
	ID   string
	Addr string
}

// AddPeerResponse contains response arguments for Session.AddPeer method.
type AddPeerResponse struct {
}

// AddTrackerRequest contains request arguments for Session.AddTracker method.
type AddTrackerRequest struct {
	ID  string
	URL string
}

// AddTrackerResponse contains response arguments for Session.AddTracker method.
type AddTrackerResponse struct {
}

// StartAllTorrentsRequest contains request arguments for Session.StartAllTorrents method.
type StartAllTorrentsRequest struct {
}

// StartAllTorrentsResponse contains response arguments for Session.StartAllTorrents method.
type StartAllTorrentsResponse struct {
}

// StopAllTorrentsRequest contains request arguments for Session.StopAllTorrents method.
type StopAllTorrentsRequest struct {
}

// StopAllTorrentsResponse contains response arguments for Session.StopAllTorrents method.
type StopAllTorrentsResponse struct {
}
