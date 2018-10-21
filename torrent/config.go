package torrent

import "time"

var Config struct {
	Download struct {
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
	}
	Peer struct {
		// Time to wait for TCP connection to open.
		ConnectTimeout time.Duration
		// Time to wait for BitTorrent handshake to complete.
		HandshakeTimeout time.Duration
		// When peer has started to send piece block, if it does not send any bytes in PieceTimeout, the connection is closed.
		PieceTimeout time.Duration
		// When the client want to connect a peer, first it tries to do encrypted handshake.
		// If it does not work, it connects to same peer again and does unencrypted handshake.
		// This behavior can be changed via config.
		Encryption struct {
			// Do not dial encrypted connections.
			DisableOutgoing bool
			// Dial only encrypted connections.
			ForceOutgoing bool
			// Do not accept unencrypted connections.
			ForceIncoming bool
		}
	}
	Tracker struct {
		// Number of peer addresses to request in announce request.
		NumWant int
		// Time to wait for announcing stopped event.
		// Stopped event is sent to the tracker when torrent is stopped.
		StoppedEventTimeout time.Duration
		// When the client needs new peer addresses to connect, it ask to the tracker.
		// To prevent spamming the tracker an interval is set to wait before the next announce.
		MinAnnounceInterval time.Duration
		// HTTP tracker options. These values does not apply to UDP trackers.
		HTTP struct {
			// TCP connect timeout
			ConnectTimeout time.Duration
			// Time to wait for establishing a HTTP connection.
			TLSHandshakeTimeout time.Duration
			// Total time to wait for response to be read.
			// This includes ConnectTimeout and TLSHandshakeTimeout.
			ClientTimeout time.Duration
		}
	}
}

func init() {
	Config.Download.RequestQueueLength = 50
	Config.Download.RequestTimeout = 20 * time.Second
	Config.Download.EndgameParallelDownloadsPerPiece = 2
	Config.Download.MaxPeerDial = 25
	Config.Download.MaxPeerAccept = 25
	Config.Download.ParallelPieceDownloads = 50
	Config.Download.ParallelMetadataDownloads = 2
	Config.Peer.ConnectTimeout = 5 * time.Second
	Config.Peer.HandshakeTimeout = 10 * time.Second
	Config.Peer.PieceTimeout = 30 * time.Second
	Config.Tracker.NumWant = 100
	Config.Tracker.StoppedEventTimeout = 5 * time.Second
	Config.Tracker.MinAnnounceInterval = time.Minute
	Config.Tracker.HTTP.ConnectTimeout = 5 * time.Second
	Config.Tracker.HTTP.TLSHandshakeTimeout = 10 * time.Second
	Config.Tracker.HTTP.ClientTimeout = 30 * time.Second
}
