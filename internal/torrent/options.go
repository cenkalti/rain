package torrent

import (
	"errors"
	"math/rand"
	"net"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/torrent/bitfield"
	"github.com/cenkalti/rain/internal/torrent/blocklist"
	"github.com/cenkalti/rain/internal/torrent/dht"
	"github.com/cenkalti/rain/internal/torrent/internal/addrlist"
	"github.com/cenkalti/rain/internal/torrent/internal/allocator"
	"github.com/cenkalti/rain/internal/torrent/internal/announcer"
	"github.com/cenkalti/rain/internal/torrent/internal/handshaker/incominghandshaker"
	"github.com/cenkalti/rain/internal/torrent/internal/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/internal/torrent/internal/infodownloader"
	"github.com/cenkalti/rain/internal/torrent/internal/mse"
	"github.com/cenkalti/rain/internal/torrent/internal/peer"
	"github.com/cenkalti/rain/internal/torrent/internal/piececache"
	"github.com/cenkalti/rain/internal/torrent/internal/piecedownloader"
	"github.com/cenkalti/rain/internal/torrent/internal/piecewriter"
	"github.com/cenkalti/rain/internal/torrent/internal/verifier"
	"github.com/cenkalti/rain/internal/torrent/metainfo"
	"github.com/cenkalti/rain/internal/torrent/resumer"
	"github.com/cenkalti/rain/internal/torrent/storage"
	"github.com/cenkalti/rain/internal/tracker"
)

// Options for creating a new Torrent.
type Options struct {
	// Display name
	Name string
	// Peer listen port. Random port will be picked if zero.
	Port int
	// HTTP and UDP trackers
	Trackers []tracker.Tracker
	// Optional resumer that saves fast resume data.
	Resumer resumer.Resumer
	// Info dict of torrent file. May be nil for magnet links.
	Info *metainfo.Info
	// Marks downloaded pieces for fast resuming. May be nil.
	Bitfield *bitfield.Bitfield
	// Initial stats from previous runs.
	Stats resumer.Stats
	// Config for downloading torrent. DefaultOptions will be used if nil.
	Config *Config
	// Optional DHT node
	DHT dht.DHT
	// Optional blocklist to prevent connection to blocked IP addresses.
	Blocklist blocklist.Blocklist
}

// NewTorrent creates a new torrent that downloads the torrent with infoHash and saves the files to the storage.
func (o *Options) NewTorrent(infoHash []byte, sto storage.Storage) (*Torrent, error) {
	if len(infoHash) != 20 {
		return nil, errors.New("invalid infoHash (must be 20 bytes)")
	}
	cfg := o.Config
	if cfg == nil {
		cfg = &DefaultConfig
	}
	logName := o.Name
	if len(logName) > 20 {
		logName = logName[:20]
	}
	var ih [20]byte
	copy(ih[:], infoHash)
	t := &Torrent{
		config:                    *cfg,
		infoHash:                  ih,
		trackers:                  o.Trackers,
		name:                      o.Name,
		storage:                   sto,
		port:                      o.Port,
		resume:                    o.Resumer,
		info:                      o.Info,
		bitfield:                  o.Bitfield,
		log:                       logger.New("torrent " + logName),
		peerDisconnectedC:         make(chan *peer.Peer),
		messages:                  make(chan peer.Message),
		pieceMessages:             make(chan peer.PieceMessage),
		peers:                     make(map[*peer.Peer]struct{}),
		incomingPeers:             make(map[*peer.Peer]struct{}),
		outgoingPeers:             make(map[*peer.Peer]struct{}),
		peersSnubbed:              make(map[*peer.Peer]struct{}),
		pieceDownloaders:          make(map[*peer.Peer]*piecedownloader.PieceDownloader),
		pieceDownloadersSnubbed:   make(map[*peer.Peer]*piecedownloader.PieceDownloader),
		pieceDownloadersChoked:    make(map[*peer.Peer]*piecedownloader.PieceDownloader),
		peerSnubbedC:              make(chan *peer.Peer),
		infoDownloaders:           make(map[*peer.Peer]*infodownloader.InfoDownloader),
		infoDownloadersSnubbed:    make(map[*peer.Peer]*infodownloader.InfoDownloader),
		pieceWriterResultC:        make(chan *piecewriter.PieceWriter),
		optimisticUnchokedPeers:   make([]*peer.Peer, 0, cfg.OptimisticUnchokedPeers),
		completeC:                 make(chan struct{}),
		closeC:                    make(chan chan struct{}),
		startCommandC:             make(chan struct{}),
		stopCommandC:              make(chan struct{}),
		statsCommandC:             make(chan statsRequest),
		trackersCommandC:          make(chan trackersRequest),
		peersCommandC:             make(chan peersRequest),
		notifyErrorCommandC:       make(chan notifyErrorCommand),
		notifyListenCommandC:      make(chan notifyListenCommand),
		addPeersCommandC:          make(chan []*net.TCPAddr),
		addrsFromTrackers:         make(chan []*net.TCPAddr),
		addrList:                  addrlist.New(cfg.MaxPeerAddresses, o.Blocklist),
		peerIDs:                   make(map[[20]byte]struct{}),
		incomingConnC:             make(chan net.Conn),
		sKeyHash:                  mse.HashSKey(ih[:]),
		infoDownloaderResultC:     make(chan *infodownloader.InfoDownloader),
		incomingHandshakers:       make(map[*incominghandshaker.IncomingHandshaker]struct{}),
		outgoingHandshakers:       make(map[*outgoinghandshaker.OutgoingHandshaker]struct{}),
		incomingHandshakerResultC: make(chan *incominghandshaker.IncomingHandshaker),
		outgoingHandshakerResultC: make(chan *outgoinghandshaker.OutgoingHandshaker),
		announcerRequestC:         make(chan *announcer.Request),
		allocatorProgressC:        make(chan allocator.Progress),
		allocatorResultC:          make(chan *allocator.Allocator),
		verifierProgressC:         make(chan verifier.Progress),
		verifierResultC:           make(chan *verifier.Verifier),
		connectedPeerIPs:          make(map[string]struct{}),
		announcersStoppedC:        make(chan struct{}),
		dhtNode:                   o.DHT,
		pieceCache:                piececache.New(cfg.PieceCacheSize, cfg.PieceCacheTTL),
		byteStats:                 o.Stats,
		blocklist:                 o.Blocklist,
	}
	copy(t.peerID[:], []byte(cfg.PeerIDPrefix))
	t.piecePool.New = func() interface{} {
		return make([]byte, t.info.PieceLength)
	}
	_, err := rand.Read(t.peerID[len(cfg.PeerIDPrefix):]) // nolint: gosec
	if err != nil {
		return nil, err
	}
	if t.dhtNode != nil {
		t.dhtPeersC = t.dhtNode.Peers()
	}
	go t.run()
	return t, nil
}
