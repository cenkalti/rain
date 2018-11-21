package torrent

import (
	"errors"
	"math/rand"
	"net"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent/bitfield"
	"github.com/cenkalti/rain/torrent/internal/addrlist"
	"github.com/cenkalti/rain/torrent/internal/allocator"
	"github.com/cenkalti/rain/torrent/internal/announcer"
	"github.com/cenkalti/rain/torrent/internal/handshaker/incominghandshaker"
	"github.com/cenkalti/rain/torrent/internal/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/torrent/internal/infodownloader"
	"github.com/cenkalti/rain/torrent/internal/mse"
	"github.com/cenkalti/rain/torrent/internal/peer"
	"github.com/cenkalti/rain/torrent/internal/piecedownloader"
	"github.com/cenkalti/rain/torrent/internal/piecewriter"
	"github.com/cenkalti/rain/torrent/internal/verifier"
	"github.com/cenkalti/rain/torrent/metainfo"
	"github.com/cenkalti/rain/torrent/resumer"
	"github.com/cenkalti/rain/torrent/storage"
)

// Options for creating a new Torrent.
type Options struct {
	// Display name
	Name string
	// Peer listen port. Random port will be picked if zero.
	Port int
	// HTTP and UDP trackers
	Trackers []string
	// Optional resumer that saves fast resume data.
	Resumer resumer.Resumer
	// Info dict of torrent file. May be nil for magnet links.
	Info []byte
	// Marks downloaded pieces for fast resuming. May be nil.
	Bitfield []byte
	// Config for downloading torrent. DefaultOptions will be used if nil.
	Config *Config
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
	var info *metainfo.Info
	var bf *bitfield.Bitfield
	var err error
	if o.Info != nil {
		info, err = metainfo.NewInfo(o.Info)
		if err != nil {
			return nil, err
		}
		if o.Bitfield != nil {
			bf = bitfield.NewBytes(o.Bitfield, info.NumPieces)
		}
	}
	t := &Torrent{
		config:                    *cfg,
		infoHash:                  ih,
		trackers:                  o.Trackers,
		name:                      o.Name,
		storage:                   sto,
		port:                      o.Port,
		resume:                    o.Resumer,
		info:                      info,
		bitfield:                  bf,
		log:                       logger.New("torrent " + logName),
		peerDisconnectedC:         make(chan *peer.Peer),
		messages:                  make(chan peer.Message),
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
		pieceWriters:              make(map[*piecewriter.PieceWriter]struct{}),
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
		addrList:                  addrlist.New(),
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
	}
	copy(t.peerID[:], peerIDPrefix)
	t.piecePool.New = func() interface{} {
		return make([]byte, t.info.PieceLength)
	}
	_, err = rand.Read(t.peerID[len(peerIDPrefix):]) // nolint: gosec
	if err != nil {
		return nil, err
	}
	t.trackersInstances, err = parseTrackers(t.trackers, t.log, cfg.HTTPTrackerTimeout)
	if err != nil {
		return nil, err
	}
	err = t.writeResume()
	if err != nil {
		return nil, err
	}
	go t.run()
	return t, nil
}
