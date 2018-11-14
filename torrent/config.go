package torrent

import "time"

type Config struct {
	// Number of unchoked peers.
	UnchokedPeers int
	// Number of optimistic unchoked peers.
	OptimisticUnchokedPeers int
	// Max number of blocks requested from a peer but not received yet
	RequestQueueLength int
	// Time to wait for a requested block to be received before marking peer as snubbed
	RequestTimeout time.Duration
	// Max number of running downloads on piece in endgame mode, snubbed and choed peers don't count
	EndgameParallelDownloadsPerPiece int
	// Max number of outgoing connections to dial
	MaxPeerDial int
	// Max number of incoming connections to accept
	MaxPeerAccept int
	// Running piece downloads, snubbed and choked peers don't count
	ParallelPieceDownloads int
	// Running metadata downloads, snubbed peers don't count
	ParallelMetadataDownloads int
	// Time to wait for TCP connection to open.
	PeerConnectTimeout time.Duration
	// Time to wait for BitTorrent handshake to complete.
	PeerHandshakeTimeout time.Duration
	// When peer has started to send piece block, if it does not send any bytes in PieceTimeout, the connection is closed.
	PieceTimeout time.Duration
	// When the client want to connect a peer, first it tries to do encrypted handshake.
	// If it does not work, it connects to same peer again and does unencrypted handshake.
	// This behavior can be changed via this variable.
	DisableOutgoingEncryption bool
	// Dial only encrypted connections.
	ForceOutgoingEncryption bool
	// Do not accept unencrypted connections.
	ForceIncomingEncryption bool
	// Number of peer addresses to request in announce request.
	TrackerNumWant int
	// Time to wait for announcing stopped event.
	// Stopped event is sent to the tracker when torrent is stopped.
	TrackerStopTimeout time.Duration
	// When the client needs new peer addresses to connect, it ask to the tracker.
	// To prevent spamming the tracker an interval is set to wait before the next announce.
	MinAnnounceInterval time.Duration
	// Total time to wait for response to be read.
	// This includes ConnectTimeout and TLSHandshakeTimeout.
	HTTPTrackerTimeout time.Duration
	// DHT announce interval
	DHTAnnounceInterval    time.Duration
	MinDHTAnnounceInterval time.Duration
}

var DefaultConfig = Config{
	UnchokedPeers:                    3,
	OptimisticUnchokedPeers:          1,
	RequestQueueLength:               50,
	RequestTimeout:                   20 * time.Second,
	EndgameParallelDownloadsPerPiece: 2,
	MaxPeerDial:                      25,
	MaxPeerAccept:                    25,
	ParallelPieceDownloads:           50,
	ParallelMetadataDownloads:        2,
	PeerConnectTimeout:               5 * time.Second,
	PeerHandshakeTimeout:             10 * time.Second,
	PieceTimeout:                     30 * time.Second,
	TrackerNumWant:                   100,
	TrackerStopTimeout:               5 * time.Second,
	MinAnnounceInterval:              time.Minute,
	HTTPTrackerTimeout:               10 * time.Second,
	DHTAnnounceInterval:              30 * time.Minute,
	MinDHTAnnounceInterval:           time.Minute,
}
