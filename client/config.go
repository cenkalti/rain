package client

import "github.com/cenkalti/rain/torrent"

// Config for Client.
type Config struct {
	// Database file to save resume data.
	Database string
	// DataDir is where files are downloaded.
	DataDir string
	// PortBegin, PortEnd int
	Torrent torrent.Config
}

var DefaultConfig = Config{
	Database: "~/.rain/resume.db",
	DataDir:  "~/rain-downloads",
	// PortBegin: 50000,
	// PortEnd:   60000,
	Torrent: torrent.DefaultConfig,
}
