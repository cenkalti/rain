package torrent

import (
	"errors"
	"math/rand"
	"net"

	"github.com/cenkalti/rain/internal/addrlist"
	"github.com/cenkalti/rain/internal/allocator"
	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/bufferpool"
	"github.com/cenkalti/rain/internal/externalip"
	"github.com/cenkalti/rain/internal/handshaker/incominghandshaker"
	"github.com/cenkalti/rain/internal/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/internal/infodownloader"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/mse"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piecedownloader"
	"github.com/cenkalti/rain/internal/piecewriter"
	"github.com/cenkalti/rain/internal/resumer"
	"github.com/cenkalti/rain/internal/storage"
	"github.com/cenkalti/rain/internal/suspendchan"
	"github.com/cenkalti/rain/internal/tracker"
	"github.com/cenkalti/rain/internal/unchoker"
	"github.com/cenkalti/rain/internal/verifier"
	"github.com/cenkalti/rain/internal/webseedsource"
	"github.com/rcrowley/go-metrics"
)

// options for creating a new Torrent.
type options struct {
	// id is used in logger name
	id string
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
}

// NewTorrent creates a new torrent that downloads the torrent with infoHash and saves the files to the storage.
func (o *options) NewTorrent(s *Session, infoHash []byte, sto storage.Storage) (*torrent, error) {
	if len(infoHash) != 20 {
		return nil, errors.New("invalid infoHash (must be 20 bytes)")
	}
	cfg := s.config
	var ih [20]byte
	copy(ih[:], infoHash)
	t := &torrent{
		session:                   s,
		id:                        o.id,
		infoHash:                  ih,
		trackers:                  o.Trackers,
		name:                      o.Name,
		storage:                   sto,
		port:                      o.Port,
		resume:                    o.Resumer,
		info:                      o.Info,
		bitfield:                  o.Bitfield,
		log:                       logger.New("torrent " + o.id),
		peerDisconnectedC:         make(chan *peer.Peer),
		messages:                  make(chan peer.Message),
		pieceMessagesC:            suspendchan.New(0),
		peers:                     make(map[*peer.Peer]struct{}),
		incomingPeers:             make(map[*peer.Peer]struct{}),
		outgoingPeers:             make(map[*peer.Peer]struct{}),
		pieceDownloaders:          make(map[*peer.Peer]*piecedownloader.PieceDownloader),
		pieceDownloadersSnubbed:   make(map[*peer.Peer]*piecedownloader.PieceDownloader),
		pieceDownloadersChoked:    make(map[*peer.Peer]*piecedownloader.PieceDownloader),
		peerSnubbedC:              make(chan *peer.Peer),
		infoDownloaders:           make(map[*peer.Peer]*infodownloader.InfoDownloader),
		infoDownloadersSnubbed:    make(map[*peer.Peer]*infodownloader.InfoDownloader),
		pieceWriterResultC:        make(chan *piecewriter.PieceWriter),
		completeC:                 make(chan struct{}),
		closeC:                    make(chan chan struct{}),
		startCommandC:             make(chan struct{}),
		stopCommandC:              make(chan struct{}),
		statsCommandC:             make(chan statsRequest),
		trackersCommandC:          make(chan trackersRequest),
		peersCommandC:             make(chan peersRequest),
		webseedsCommandC:          make(chan webseedsRequest),
		notifyErrorCommandC:       make(chan notifyErrorCommand),
		notifyListenCommandC:      make(chan notifyListenCommand),
		addPeersCommandC:          make(chan []*net.TCPAddr),
		addTrackersCommandC:       make(chan []tracker.Tracker),
		addrsFromTrackers:         make(chan []*net.TCPAddr),
		peerIDs:                   make(map[[20]byte]struct{}),
		incomingConnC:             make(chan net.Conn),
		sKeyHash:                  mse.HashSKey(ih[:]),
		infoDownloaderResultC:     make(chan *infodownloader.InfoDownloader),
		incomingHandshakers:       make(map[*incominghandshaker.IncomingHandshaker]struct{}),
		outgoingHandshakers:       make(map[*outgoinghandshaker.OutgoingHandshaker]struct{}),
		incomingHandshakerResultC: make(chan *incominghandshaker.IncomingHandshaker),
		outgoingHandshakerResultC: make(chan *outgoinghandshaker.OutgoingHandshaker),
		allocatorProgressC:        make(chan allocator.Progress),
		allocatorResultC:          make(chan *allocator.Allocator),
		verifierProgressC:         make(chan verifier.Progress),
		verifierResultC:           make(chan *verifier.Verifier),
		connectedPeerIPs:          make(map[string]struct{}),
		bannedPeerIPs:             make(map[string]struct{}),
		announcersStoppedC:        make(chan struct{}),
		dhtPeersC:                 make(chan []*net.TCPAddr, 1),
		resumerStats:              o.Stats,
		externalIP:                externalip.FirstExternalIP(),
		downloadSpeed:             metrics.NewEWMA1(),
		uploadSpeed:               metrics.NewEWMA1(),
		ramNotifyC:                make(chan interface{}),
		webseedPieceResultC:       suspendchan.New(0),
		webseedRetryC:             make(chan *webseedsource.WebseedSource),
		doneC:                     make(chan struct{}),
	}
	t.addrList = addrlist.New(cfg.MaxPeerAddresses, s.blocklist, o.Port, &t.externalIP)
	copy(t.peerID[:], []byte(cfg.PeerIDPrefix))
	if t.info != nil {
		t.piecePool = bufferpool.New(int(t.info.PieceLength))
	}
	_, err := rand.Read(t.peerID[len(cfg.PeerIDPrefix):]) // nolint: gosec
	if err != nil {
		return nil, err
	}
	t.unchoker = unchoker.New(cfg.UnchokedPeers, cfg.OptimisticUnchokedPeers)
	go t.run()
	return t, nil
}

func (t *torrent) getPeersForUnchoker() []unchoker.Peer {
	peers := make([]unchoker.Peer, 0, len(t.peers))
	for pe := range t.peers {
		peers = append(peers, pe)
	}
	return peers
}
