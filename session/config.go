package session

import (
	"github.com/cenkalti/rain/internal/torrent"
	"time"
)

// Config for Session.
type Config struct {
	// Database file to save resume data.
	Database string
	// DataDir is where files are downloaded.
	DataDir string
	// New torrents will be listened at selected port in this range.
	PortBegin, PortEnd uint16
	// DHT node will listen on this IP.
	DHTAddress string
	// DHT node will listen on this UDP port.
	DHTPort uint16
	// At start, client will set max open files limit to this number. (like "ulimit -n" command)
	MaxOpenFiles uint64
	// URL to the blocklist file in CIDR format.
	BlocklistURL string
	// When to refresh blocklist
	BlocklistUpdateInterval time.Duration
	// Host to listen for RPC server
	RPCHost string
	// Listen port for RPC server
	RPCPort int
	// Time to wait for ongoing requests before shutting down RPC HTTP server.
	RPCShutdownTimeout time.Duration

	Torrent torrent.Config
}

var DefaultConfig = Config{
	Database:           "~/.rain/resume.db",
	DataDir:            "~/rain-downloads",
	PortBegin:          50000,
	PortEnd:            60000,
	DHTAddress:         "0.0.0.0",
	DHTPort:            7246,
	MaxOpenFiles:       1024 * 1024,
	RPCHost:            "127.0.0.1",
	RPCPort:            7246,
	RPCShutdownTimeout: 5 * time.Second,
	Torrent:            torrent.DefaultConfig,
}
