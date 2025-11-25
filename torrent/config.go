package torrent

import (
	"io/fs"
	"time"

	"github.com/cenkalti/log"
	"github.com/cenkalti/rain/v2/internal/metainfo"
	"github.com/cenkalti/rain/v2/internal/storage"
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
	Database string `yaml:"database"`
	// DataDir is where files are downloaded.
	// Effective only when default storage provider is used.
	DataDir string `yaml:"data-dir"`
	// If true, torrent files are saved into <data_dir>/<torrent_id>/<torrent_name>.
	// Useful if downloading the same torrent from multiple sources.
	// Effective only when default storage provider is used.
	DataDirIncludesTorrentID bool `yaml:"data-dir-includes-torrent-id"`
	// Host to listen for TCP Acceptor. Port is computed automatically
	Host string `yaml:"host"`
	// New torrents will be listened at selected port in this range.
	PortBegin uint16 `yaml:"port-begin"`
	PortEnd   uint16 `yaml:"port-end"`
	// At start, client will set max open files limit to this number. (like "ulimit -n" command)
	MaxOpenFiles uint64 `yaml:"max-open-files"`
	// Enable peer exchange protocol.
	PEXEnabled bool `yaml:"pex-enabled"`
	// Resume data (bitfield & stats) are saved to disk at interval to keep IO lower.
	ResumeWriteInterval time.Duration `yaml:"resume-write-interval"`
	// Peer id is prefixed with this string. See BEP 20. Remaining bytes of peer id will be randomized.
	// Only applies to private torrents.
	PrivatePeerIDPrefix string `yaml:"private-peer-id-prefix"`
	// Client version that is sent in BEP 10 handshake message.
	// Only applies to private torrents.
	PrivateExtensionHandshakeClientVersion string `yaml:"private-extension-handshake-client-version"`
	// URL to the blocklist file in CIDR format.
	BlocklistURL string `yaml:"blocklist-url"`
	// When to refresh blocklist
	BlocklistUpdateInterval time.Duration `yaml:"blocklist-update-interval"`
	// HTTP timeout for downloading blocklist
	BlocklistUpdateTimeout time.Duration `yaml:"blocklist-update-timeout"`
	// Do not contact tracker if it's IP is blocked
	BlocklistEnabledForTrackers bool `yaml:"blocklist-enabled-for-trackers"`
	// Do not connect to peer if it's IP is blocked
	BlocklistEnabledForOutgoingConnections bool `yaml:"blocklist-enabled-for-outgoing-connections"`
	// Do not accept connections from peer if it's IP is blocked
	BlocklistEnabledForIncomingConnections bool `yaml:"blocklist-enabled-for-incoming-connections"`
	// Do not accept response larger than this size
	BlocklistMaxResponseSize int64 `yaml:"blocklist-max-response-size"`
	// Time to wait when adding torrent with AddURI().
	TorrentAddHTTPTimeout time.Duration `yaml:"torrent-add-http-timeout"`
	// Maximum allowed size to be received by metadata extension.
	MaxMetadataSize uint `yaml:"max-metadata-size"`
	// Maximum allowed size to be read when adding torrent.
	MaxTorrentSize uint `yaml:"max-torrent-size"`
	// Maximum allowed number of pieces in a torrent.
	MaxPieces uint32 `yaml:"max-pieces"`
	// Time to wait when resolving host names for trackers and peers.
	DNSResolveTimeout time.Duration `yaml:"dns-resolve-timeout"`
	// Global download speed limit in KB/s.
	SpeedLimitDownload int64 `yaml:"speed-limit-download"`
	// Global upload speed limit in KB/s.
	SpeedLimitUpload int64 `yaml:"speed-limit-upload"`
	// Start torrent automatically if it was running when previous session was closed.
	ResumeOnStartup bool `yaml:"resume-on-startup"`
	// Check each torrent loop for aliveness. Helps to detect bugs earlier.
	HealthCheckInterval time.Duration `yaml:"health-check-interval"`
	// If torrent loop is stuck for more than this duration. Program crashes with stacktrace.
	HealthCheckTimeout time.Duration `yaml:"health-check-timeout"`
	// The unix permission of created files, execute bit is removed for files.
	// Effective only when default storage provider is used.
	FilePermissions fs.FileMode `yaml:"file-permissions"`

	// Enable RPC server
	RPCEnabled bool `yaml:"rpc-enabled"`
	// Host to listen for RPC server
	RPCHost string `yaml:"rpc-host"`
	// Listen port for RPC server
	RPCPort int `yaml:"rpc-port"`
	// Time to wait for ongoing requests before shutting down RPC HTTP server.
	RPCShutdownTimeout time.Duration `yaml:"rpc-shutdown-timeout"`

	// Enable DHT node.
	DHTEnabled bool `yaml:"dht-enabled"`
	// DHT node will listen on this IP.
	DHTHost string `yaml:"dht-host"`
	// DHT node will listen on this UDP port.
	DHTPort uint16 `yaml:"dht-port"`
	// DHT announce interval
	DHTAnnounceInterval time.Duration `yaml:"dht-announce-interval"`
	// Minimum announce interval when announcing to DHT.
	DHTMinAnnounceInterval time.Duration `yaml:"dht-min-announce-interval"`
	// Known routers to bootstrap local DHT node.
	DHTBootstrapNodes []string `yaml:"dht-bootstrap-nodes"`

	// Number of peer addresses to request in announce request.
	TrackerNumWant int `yaml:"tracker-num-want"`
	// Time to wait for announcing stopped event.
	// Stopped event is sent to the tracker when torrent is stopped.
	TrackerStopTimeout time.Duration `yaml:"tracker-stop-timeout"`
	// When the client needs new peer addresses to connect, it ask to the tracker.
	// To prevent spamming the tracker an interval is set to wait before the next announce.
	TrackerMinAnnounceInterval time.Duration `yaml:"tracker-min-announce-interval"`
	// Total time to wait for response to be read.
	// This includes ConnectTimeout and TLSHandshakeTimeout.
	TrackerHTTPTimeout time.Duration `yaml:"tracker-http-timeout"`
	// User agent sent when communicating with HTTP trackers.
	// Only applies to private torrents.
	TrackerHTTPPrivateUserAgent string `yaml:"tracker-http-private-user-agent"`
	// Max number of bytes in a tracker response.
	TrackerHTTPMaxResponseSize uint `yaml:"tracker-http-max-response-size"`
	// Check and validate TLS ceritificates.
	TrackerHTTPVerifyTLS bool `yaml:"tracker-http-verify-tls"`

	// Number of unchoked peers.
	UnchokedPeers int `yaml:"unchoked-peers"`
	// Number of optimistic unchoked peers.
	OptimisticUnchokedPeers int `yaml:"optimistic-unchoked-peers"`
	// Max number of blocks allowed to be queued without dropping any.
	MaxRequestsIn int `yaml:"max-requests-in"`
	// Max number of blocks requested from a peer but not received yet.
	// `rreq` value from extended handshake cannot exceed this limit.
	MaxRequestsOut int `yaml:"max-requests-out"`
	// Number of bloks requested from peer if it does not send `rreq` value in extended handshake.
	DefaultRequestsOut int `yaml:"default-requests-out"`
	// Time to wait for a requested block to be received before marking peer as snubbed
	RequestTimeout time.Duration `yaml:"request-timeout"`
	// Max number of running downloads on piece in endgame mode, snubbed and choed peers don't count
	EndgameMaxDuplicateDownloads int `yaml:"endgame-max-duplicate-downloads"`
	// Max number of outgoing connections to dial
	MaxPeerDial int `yaml:"max-peer-dial"`
	// Max number of incoming connections to accept
	MaxPeerAccept int `yaml:"max-peer-accept"`
	// Running metadata downloads, snubbed peers don't count
	ParallelMetadataDownloads int `yaml:"parallel-metadata-downloads"`
	// Time to wait for TCP connection to open.
	PeerConnectTimeout time.Duration `yaml:"peer-connect-timeout"`
	// Time to wait for BitTorrent handshake to complete.
	PeerHandshakeTimeout time.Duration `yaml:"peer-handshake-timeout"`
	// When peer has started to send piece block, if it does not send any bytes in PieceReadTimeout, the connection is closed.
	PieceReadTimeout time.Duration `yaml:"piece-read-timeout"`
	// Max number of peer addresses to keep in connect queue.
	MaxPeerAddresses int `yaml:"max-peer-addresses"`
	// Number of allowed-fast messages to send after handshake.
	AllowedFastSet int `yaml:"allowed-fast-set"`

	// Number of bytes to read when a piece is requested by a peer.
	ReadCacheBlockSize int64 `yaml:"read-cache-block-size"`
	// Number of cached bytes for piece read requests.
	ReadCacheSize int64 `yaml:"read-cache-size"`
	// Read bytes for a piece part expires after duration.
	ReadCacheTTL time.Duration `yaml:"read-cache-ttl"`
	// Number of read operations to do in parallel.
	ParallelReads uint `yaml:"parallel-reads"`
	// Number of write operations to do in parallel.
	ParallelWrites uint `yaml:"parallel-writes"`
	// Number of bytes allocated in memory for downloading piece data.
	WriteCacheSize int64 `yaml:"write-cache-size"`

	// When the client want to connect a peer, first it tries to do encrypted handshake.
	// If it does not work, it connects to same peer again and does unencrypted handshake.
	// This behavior can be changed via this variable.
	DisableOutgoingEncryption bool `yaml:"disable-outgoing-encryption"`
	// Dial only encrypted connections.
	ForceOutgoingEncryption bool `yaml:"force-outgoing-encryption"`
	// Do not accept unencrypted connections.
	ForceIncomingEncryption bool `yaml:"force-incoming-encryption"`

	// TCP connect timeout for WebSeed sources
	WebseedDialTimeout time.Duration `yaml:"webseed-dial-timeout"`
	// TLS handshake timeout for WebSeed sources
	WebseedTLSHandshakeTimeout time.Duration `yaml:"webseed-tls-handshake-timeout"`
	// HTTP header timeout for WebSeed sources
	WebseedResponseHeaderTimeout time.Duration `yaml:"webseed-response-header-timeout"`
	// HTTP body read timeout for Webseed sources
	WebseedResponseBodyReadTimeout time.Duration `yaml:"webseed-response-body-read-timeout"`
	// Retry interval for restarting failed downloads
	WebseedRetryInterval time.Duration `yaml:"webseed-retry-interval"`
	// Verify TLS certificate for WebSeed URLs
	WebseedVerifyTLS bool `yaml:"webseed-verify-tls"`
	// Limit the number of WebSeed sources in torrent.
	WebseedMaxSources int `yaml:"webseed-max-sources"`
	// Number of maximum simulateous downloads from WebSeed sources.
	WebseedMaxDownloads int `yaml:"webseed-max-downloads"`

	// Shell command to execute on torrent completion.
	OnCompleteCmd []string `yaml:"on-complete-cmd"`

	// Replace default log handler
	CustomLogHandler log.Handler `yaml:"-"`
	// Replace default storage provider
	CustomStorage storage.Provider `yaml:"-"`

	// Enable debugging
	Debug bool `yaml:"debug"`
}

// DefaultConfig for Session. Do not pass zero value Config to NewSession. Copy this struct and modify instead.
var DefaultConfig = Config{
	// Session
	Database:                               "$HOME/rain/session.db",
	DataDir:                                "$HOME/rain/data",
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
