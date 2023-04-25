package torrent

import (
	"io/fs"
	"time"

	"github.com/cenkalti/log"
	"github.com/cenkalti/rain/internal/metainfo"
)

var (
	publicPeerIDPrefix                    = "-RN" + Version + "-"
	publicExtensionHandshakeClientVersion = "Rain " + Version
	trackerHTTPPublicUserAgent            = "Rain/" + Version
)

func init() {
	metainfo.Creator = publicExtensionHandshakeClientVersion
}

// Config for Session.
type Config struct {
	// Database file to save resume data.
	Database string
	// DataDir is where files are downloaded.
	DataDir string
	// If true, torrent files are saved into <data_dir>/<torrent_id>/<torrent_name>.
	// Useful if downloading the same torrent from multiple sources.
	DataDirIncludesTorrentID bool
	// Host to listen for TCP Acceptor. Port is computed automatically
	Host string
	// New torrents will be listened at selected port in this range.
	PortBegin, PortEnd uint16
	// At start, client will set max open files limit to this number. (like "ulimit -n" command)
	MaxOpenFiles uint64
	// Enable peer exchange protocol.
	PEXEnabled bool
	// Resume data (bitfield & stats) are saved to disk at interval to keep IO lower.
	ResumeWriteInterval time.Duration
	// Peer id is prefixed with this string. See BEP 20. Remaining bytes of peer id will be randomized.
	// Only applies to private torrents.
	PrivatePeerIDPrefix string
	// Client version that is sent in BEP 10 handshake message.
	// Only applies to private torrents.
	PrivateExtensionHandshakeClientVersion string
	// URL to the blocklist file in CIDR format.
	BlocklistURL string
	// When to refresh blocklist
	BlocklistUpdateInterval time.Duration
	// HTTP timeout for downloading blocklist
	BlocklistUpdateTimeout time.Duration
	// Do not contact tracker if it's IP is blocked
	BlocklistEnabledForTrackers bool
	// Do not connect to peer if it's IP is blocked
	BlocklistEnabledForOutgoingConnections bool
	// Do not accept connections from peer if it's IP is blocked
	BlocklistEnabledForIncomingConnections bool
	// Do not accept response larger than this size
	BlocklistMaxResponseSize int64
	// Time to wait when adding torrent with AddURI().
	TorrentAddHTTPTimeout time.Duration
	// Maximum allowed size to be received by metadata extension.
	MaxMetadataSize uint
	// Maximum allowed size to be read when adding torrent.
	MaxTorrentSize uint
	// Maximum allowed number of pieces in a torrent.
	MaxPieces uint32
	// Time to wait when resolving host names for trackers and peers.
	DNSResolveTimeout time.Duration
	// Global download speed limit in KB/s.
	SpeedLimitDownload int64
	// Global upload speed limit in KB/s.
	SpeedLimitUpload int64
	// Start torrent automatically if it was running when previous session was closed.
	ResumeOnStartup bool
	// Check each torrent loop for aliveness. Helps to detect bugs earlier.
	HealthCheckInterval time.Duration
	// If torrent loop is stuck for more than this duration. Program crashes with stacktrace.
	HealthCheckTimeout time.Duration
	// The unix permission of created files, execute bit is removed for files
	FilePermissions fs.FileMode

	// Enable RPC server
	RPCEnabled bool
	// Host to listen for RPC server
	RPCHost string
	// Listen port for RPC server
	RPCPort int
	// Time to wait for ongoing requests before shutting down RPC HTTP server.
	RPCShutdownTimeout time.Duration

	// Enable DHT node.
	DHTEnabled bool
	// DHT node will listen on this IP.
	DHTHost string
	// DHT node will listen on this UDP port.
	DHTPort uint16
	// DHT announce interval
	DHTAnnounceInterval time.Duration
	// Minimum announce interval when announcing to DHT.
	DHTMinAnnounceInterval time.Duration
	// Known routers to bootstrap local DHT node.
	DHTBootstrapNodes []string

	// Number of peer addresses to request in announce request.
	TrackerNumWant int
	// Time to wait for announcing stopped event.
	// Stopped event is sent to the tracker when torrent is stopped.
	TrackerStopTimeout time.Duration
	// When the client needs new peer addresses to connect, it ask to the tracker.
	// To prevent spamming the tracker an interval is set to wait before the next announce.
	TrackerMinAnnounceInterval time.Duration
	// Total time to wait for response to be read.
	// This includes ConnectTimeout and TLSHandshakeTimeout.
	TrackerHTTPTimeout time.Duration
	// User agent sent when communicating with HTTP trackers.
	// Only applies to private torrents.
	TrackerHTTPPrivateUserAgent string
	// Max number of bytes in a tracker response.
	TrackerHTTPMaxResponseSize uint
	// Check and validate TLS ceritificates.
	TrackerHTTPVerifyTLS bool

	// Number of unchoked peers.
	UnchokedPeers int
	// Number of optimistic unchoked peers.
	OptimisticUnchokedPeers int
	// Max number of blocks allowed to be queued without dropping any.
	MaxRequestsIn int
	// Max number of blocks requested from a peer but not received yet.
	// `rreq` value from extended handshake cannot exceed this limit.
	MaxRequestsOut int
	// Number of bloks requested from peer if it does not send `rreq` value in extended handshake.
	DefaultRequestsOut int
	// Time to wait for a requested block to be received before marking peer as snubbed
	RequestTimeout time.Duration
	// Max number of running downloads on piece in endgame mode, snubbed and choed peers don't count
	EndgameMaxDuplicateDownloads int
	// Max number of outgoing connections to dial
	MaxPeerDial int
	// Max number of incoming connections to accept
	MaxPeerAccept int
	// Running metadata downloads, snubbed peers don't count
	ParallelMetadataDownloads int
	// Time to wait for TCP connection to open.
	PeerConnectTimeout time.Duration
	// Time to wait for BitTorrent handshake to complete.
	PeerHandshakeTimeout time.Duration
	// When peer has started to send piece block, if it does not send any bytes in PieceReadTimeout, the connection is closed.
	PieceReadTimeout time.Duration
	// Max number of peer addresses to keep in connect queue.
	MaxPeerAddresses int
	// Number of allowed-fast messages to send after handshake.
	AllowedFastSet int

	// Number of bytes to read when a piece is requested by a peer.
	ReadCacheBlockSize int64
	// Number of cached bytes for piece read requests.
	ReadCacheSize int64
	// Read bytes for a piece part expires after duration.
	ReadCacheTTL time.Duration
	// Number of read operations to do in parallel.
	ParallelReads uint
	// Number of write operations to do in parallel.
	ParallelWrites uint
	// Number of bytes allocated in memory for downloading piece data.
	WriteCacheSize int64

	// When the client want to connect a peer, first it tries to do encrypted handshake.
	// If it does not work, it connects to same peer again and does unencrypted handshake.
	// This behavior can be changed via this variable.
	DisableOutgoingEncryption bool
	// Dial only encrypted connections.
	ForceOutgoingEncryption bool
	// Do not accept unencrypted connections.
	ForceIncomingEncryption bool

	// TCP connect timeout for WebSeed sources
	WebseedDialTimeout time.Duration
	// TLS handshake timeout for WebSeed sources
	WebseedTLSHandshakeTimeout time.Duration
	// HTTP header timeout for WebSeed sources
	WebseedResponseHeaderTimeout time.Duration
	// HTTP body read timeout for Webseed sources
	WebseedResponseBodyReadTimeout time.Duration
	// Retry interval for restarting failed downloads
	WebseedRetryInterval time.Duration
	// Verify TLS certificate for WebSeed URLs
	WebseedVerifyTLS bool
	// Limit the number of WebSeed sources in torrent.
	WebseedMaxSources int
	// Number of maximum simulateous downloads from WebSeed sources.
	WebseedMaxDownloads int

	// Shell command to execute on torrent completion.
	OnCompleteCmd []string

	// Replace default log handler
	CustomLogHandler log.Handler
	// Enable debugging
	Debug bool
}

// DefaultConfig for Session. Do not pass zero value Config to NewSession. Copy this struct and modify instead.
var DefaultConfig = Config{
	// Session
	Database:                               "~/rain/session.db",
	DataDir:                                "~/rain/data",
	DataDirIncludesTorrentID:               true,
	Host:                                   "0.0.0.0",
	PortBegin:                              20000,
	PortEnd:                                30000,
	MaxOpenFiles:                           10240,
	PEXEnabled:                             true,
	ResumeWriteInterval:                    30 * time.Second,
	PrivatePeerIDPrefix:                    "-RN" + Version + "-",
	PrivateExtensionHandshakeClientVersion: "Rain " + Version,
	BlocklistUpdateInterval:                24 * time.Hour,
	BlocklistUpdateTimeout:                 10 * time.Minute,
	BlocklistEnabledForTrackers:            true,
	BlocklistEnabledForOutgoingConnections: true,
	BlocklistEnabledForIncomingConnections: true,
	BlocklistMaxResponseSize:               100 << 20,
	TorrentAddHTTPTimeout:                  30 * time.Second,
	MaxMetadataSize:                        30 << 20,
	MaxTorrentSize:                         10 << 20,
	MaxPieces:                              64 << 10,
	DNSResolveTimeout:                      5 * time.Second,
	ResumeOnStartup:                        true,
	HealthCheckInterval:                    10 * time.Second,
	HealthCheckTimeout:                     60 * time.Second,
	FilePermissions:                        0o750,

	// RPC Server
	RPCEnabled:         true,
	RPCHost:            "127.0.0.1",
	RPCPort:            7246,
	RPCShutdownTimeout: 5 * time.Second,

	// Tracker
	TrackerNumWant:              200,
	TrackerStopTimeout:          5 * time.Second,
	TrackerMinAnnounceInterval:  time.Minute,
	TrackerHTTPTimeout:          10 * time.Second,
	TrackerHTTPPrivateUserAgent: "Rain/" + Version,
	TrackerHTTPMaxResponseSize:  2 << 20,
	TrackerHTTPVerifyTLS:        true,

	// DHT node
	DHTEnabled:             true,
	DHTHost:                "0.0.0.0",
	DHTPort:                7246,
	DHTAnnounceInterval:    30 * time.Minute,
	DHTMinAnnounceInterval: time.Minute,
	DHTBootstrapNodes: []string{
		"router.bittorrent.com:6881",
		"dht.transmissionbt.com:6881",
		"router.utorrent.com:6881",
		"dht.libtorrent.org:25401",
		"dht.aelitis.com:6881",
	},

	// Peer
	UnchokedPeers:                3,
	OptimisticUnchokedPeers:      1,
	MaxRequestsIn:                250,
	MaxRequestsOut:               250,
	DefaultRequestsOut:           50,
	RequestTimeout:               20 * time.Second,
	EndgameMaxDuplicateDownloads: 20,
	MaxPeerDial:                  80,
	MaxPeerAccept:                20,
	ParallelMetadataDownloads:    2,
	PeerConnectTimeout:           5 * time.Second,
	PeerHandshakeTimeout:         10 * time.Second,
	PieceReadTimeout:             30 * time.Second,
	MaxPeerAddresses:             2000,
	AllowedFastSet:               10,

	// IO
	ReadCacheBlockSize: 128 << 10,
	ReadCacheSize:      256 << 20,
	ReadCacheTTL:       1 * time.Minute,
	ParallelReads:      1,
	ParallelWrites:     1,
	WriteCacheSize:     1 << 30,

	// Webseed settings
	WebseedDialTimeout:             10 * time.Second,
	WebseedTLSHandshakeTimeout:     10 * time.Second,
	WebseedResponseHeaderTimeout:   10 * time.Second,
	WebseedResponseBodyReadTimeout: 10 * time.Second,
	WebseedRetryInterval:           time.Minute,
	WebseedVerifyTLS:               true,
	WebseedMaxSources:              10,
	WebseedMaxDownloads:            4,
}
