package torrent

import (
	"crypto/rand"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/cenkalti/rain/internal/acceptor"
	"github.com/cenkalti/rain/internal/addrlist"
	"github.com/cenkalti/rain/internal/allocator"
	"github.com/cenkalti/rain/internal/announcer"
	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/blocklist"
	"github.com/cenkalti/rain/internal/bufferpool"
	"github.com/cenkalti/rain/internal/externalip"
	"github.com/cenkalti/rain/internal/handshaker/incominghandshaker"
	"github.com/cenkalti/rain/internal/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/internal/infodownloader"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/mse"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/pexlist"
	"github.com/cenkalti/rain/internal/piece"
	"github.com/cenkalti/rain/internal/piecedownloader"
	"github.com/cenkalti/rain/internal/piecepicker"
	"github.com/cenkalti/rain/internal/piecewriter"
	"github.com/cenkalti/rain/internal/resumer"
	"github.com/cenkalti/rain/internal/storage"
	"github.com/cenkalti/rain/internal/suspendchan"
	"github.com/cenkalti/rain/internal/tracker"
	"github.com/cenkalti/rain/internal/unchoker"
	"github.com/cenkalti/rain/internal/urldownloader"
	"github.com/cenkalti/rain/internal/verifier"
	"github.com/cenkalti/rain/internal/webseedsource"
	"github.com/rcrowley/go-metrics"
)

// torrent connects to peers and downloads files from swarm.
type torrent struct {
	session *Session
	id      string
	addedAt time.Time

	// Identifies the torrent being downloaded.
	infoHash [20]byte

	// List of addresses to announce this torrent.
	trackers    []tracker.Tracker
	rawTrackers [][]string

	// Peers added from magnet URLS with x.pe parameter.
	fixedPeers []string

	// Name of the torrent.
	name string

	// Storage implementation to save the files in torrent.
	storage storage.Storage

	// TCP Port to listen for peer connections.
	port int

	// Contains info about files in torrent. This can be nil at start for magnet downloads.
	info *metainfo.Info

	// Bitfield for pieces we have. It is created after we got info.
	// Bits are set only after data is written to file.
	bitfield *bitfield.Bitfield

	// Protects bitfield writing from torrent loop and reading from announcer loop.
	mBitfield sync.RWMutex

	// Unique peer ID is generated per downloader.
	peerID [20]byte

	files  []allocator.File
	pieces []piece.Piece

	piecePicker *piecepicker.PiecePicker

	// Peers are sent to this channel when they are disconnected.
	peerDisconnectedC chan *peer.Peer

	// Piece messages coming from peers are sent this channel.
	pieceMessagesC *suspendchan.Chan[peer.PieceMessage]

	// Other messages coming from peers are sent to this channel.
	messages chan peer.Message

	// We keep connected peers in this map after they complete handshake phase.
	peers map[*peer.Peer]struct{}

	// Also keep a reference to incoming and outgoing peers separately to count them quickly.
	incomingPeers map[*peer.Peer]struct{}
	outgoingPeers map[*peer.Peer]struct{}

	// Keep recently seen peers to fill underpopulated PEX lists.
	recentlySeen pexlist.RecentlySeen

	// Unchoker implements an algorithm to select peers to unchoke based on their download speed.
	unchoker *unchoker.Unchoker

	// Active piece downloads are kept in this map.
	pieceDownloaders        map[*peer.Peer]*piecedownloader.PieceDownloader
	pieceDownloadersSnubbed map[*peer.Peer]*piecedownloader.PieceDownloader
	pieceDownloadersChoked  map[*peer.Peer]*piecedownloader.PieceDownloader

	// When a peer has snubbed us, a message sent to this channel.
	peerSnubbedC chan *peer.Peer

	// Active metadata downloads are kept in this map.
	infoDownloaders        map[*peer.Peer]*infodownloader.InfoDownloader
	infoDownloadersSnubbed map[*peer.Peer]*infodownloader.InfoDownloader

	pieceWriterResultC chan *piecewriter.PieceWriter

	// This channel is closed once all torrent pieces are downloaded and verified.
	completeC chan struct{}

	// This channel is closed once all metadata pieces are downloaded and verified.
	completeMetadataC chan struct{}

	// True after all pieces are download, verified and written to disk.
	completed bool

	// If any unrecoverable error occurs, it will be sent to this channel and download will be stopped.
	errC chan error

	// After listener has started, port will be sent to this channel.
	portC chan int

	// Contains the last error sent to errC.
	lastError error

	// When Stop() is called, it will close this channel to signal run() function to stop.
	closeC chan struct{}

	// Close() blocks until doneC is closed.
	doneC chan struct{}

	// These are the channels for sending a message to run() loop.
	statsCommandC        chan statsRequest        // Stats()
	trackersCommandC     chan trackersRequest     // Trackers()
	peersCommandC        chan peersRequest        // Peers()
	webseedsCommandC     chan webseedsRequest     // Webseeds()
	startCommandC        chan struct{}            // Start()
	stopCommandC         chan struct{}            // Stop()
	announceCommandC     chan struct{}            // Announce()
	verifyCommandC       chan struct{}            // Verify()
	notifyErrorCommandC  chan notifyErrorCommand  // NotifyError()
	notifyListenCommandC chan notifyListenCommand // NotifyListen()
	addPeersCommandC     chan []*net.TCPAddr      // AddPeers()
	addTrackersCommandC  chan []tracker.Tracker   // AddTrackers()

	// Trackers send announce responses to this channel.
	addrsFromTrackers chan []*net.TCPAddr

	// Keeps a list of peer addresses to connect.
	addrList *addrlist.AddrList

	// New raw connections created by OutgoingHandshaker are sent to here.
	incomingConnC chan net.Conn

	// Keep a set of peer IDs to block duplicate connections.
	peerIDs map[[20]byte]struct{}

	// Listens for incoming peer connections.
	acceptor *acceptor.Acceptor

	// Special hash of info hash for encypted connection handshake.
	sKeyHash [20]byte

	// Announces the status of torrent to trackers to get peer addresses periodically.
	announcers []*announcer.PeriodicalAnnouncer

	// This announcer announces Stopped event to the trackers after
	// all periodical trackers are closed.
	stoppedEventAnnouncer *announcer.StopAnnouncer

	// If not nil, torrent is announced to DHT periodically.
	dhtAnnouncer *announcer.DHTAnnouncer
	dhtPeersC    chan []*net.TCPAddr

	// List of peers in handshake state.
	incomingHandshakers map[*incominghandshaker.IncomingHandshaker]struct{}
	outgoingHandshakers map[*outgoinghandshaker.OutgoingHandshaker]struct{}

	// Handshake results are sent to these channels by handshakers.
	incomingHandshakerResultC chan *incominghandshaker.IncomingHandshaker
	outgoingHandshakerResultC chan *outgoinghandshaker.OutgoingHandshaker

	// When metadata of the torrent downloaded completely, a message is sent to this channel.
	infoDownloaderResultC chan *infodownloader.InfoDownloader

	// A ticker that ticks periodically to keep a certain number of peers unchoked.
	unchokeTicker *time.Ticker

	// A worker that opens and allocates files on the disk.
	allocator          *allocator.Allocator
	allocatorProgressC chan allocator.Progress
	allocatorResultC   chan *allocator.Allocator
	bytesAllocated     int64

	// A worker that does hash check of files on the disk.
	verifier          *verifier.Verifier
	verifierProgressC chan verifier.Progress
	verifierResultC   chan *verifier.Verifier
	checkedPieces     uint32

	// Metrics
	downloadSpeed   metrics.Meter
	uploadSpeed     metrics.Meter
	bytesDownloaded metrics.Counter
	bytesUploaded   metrics.Counter
	bytesWasted     metrics.Counter
	seededFor       metrics.Counter

	seedDurationUpdatedAt time.Time
	seedDurationTicker    *time.Ticker

	// Holds connected peer IPs so we don't dial/accept multiple connections to/from same IP.
	connectedPeerIPs map[string]struct{}

	// Peers that are sending corrupt data are banned.
	bannedPeerIPs map[string]struct{}

	// A signal sent to run() loop when announcers are stopped.
	announcersStoppedC chan struct{}

	// Piece buffers that are being downloaded are pooled to reduce load on GC.
	piecePool *bufferpool.Pool

	// Used to calculate canonical peer priority (BEP 40).
	// Initialized with value found in network interfaces.
	// Then, updated from "yourip" field in BEP 10 extension handshake message.
	externalIP net.IP

	ramNotifyC chan *peer.Peer

	webseedClient          *http.Client
	webseedSources         []*webseedsource.WebseedSource
	rawWebseedSources      []string
	webseedPieceResultC    *suspendchan.Chan[*urldownloader.PieceResult]
	webseedRetryC          chan *webseedsource.WebseedSource
	webseedActiveDownloads int

	// Set to true when manual verification is requested
	doVerify bool

	// If true, the torrent is stopped automatically when all torrent pieces are downloaded.
	stopAfterDownload bool

	// If true, the torrent is stopped automatically when all metadata pieces are downloaded.
	stopAfterMetadata bool

	// True means that completeCmd has run before.
	completeCmdRun bool

	log logger.Logger
}

// newTorrent2 is a constructor for torrent struct.
// loadExistingTorrents, addTorrentStopped and addMagnet ultimately calls this method.
func newTorrent2(
	s *Session,
	id string,
	addedAt time.Time,
	infoHash []byte,
	sto storage.Storage,
	name string, // display name
	port int, // tcp peer port
	trackers []tracker.Tracker,
	fixedPeers []string,
	info *metainfo.Info,
	bf *bitfield.Bitfield,
	stats resumer.Stats, // initial stats from previous run
	ws []*webseedsource.WebseedSource,
	stopAfterDownload bool,
	stopAfterMetadata bool,
	completeCmdRun bool,
) (*torrent, error) {
	if len(infoHash) != 20 {
		return nil, errors.New("invalid infoHash (must be 20 bytes)")
	}
	cfg := s.config
	var ih [20]byte
	copy(ih[:], infoHash)
	t := &torrent{
		session:                   s,
		id:                        id,
		addedAt:                   addedAt,
		infoHash:                  ih,
		trackers:                  trackers,
		fixedPeers:                fixedPeers,
		name:                      name,
		storage:                   sto,
		port:                      port,
		info:                      info,
		bitfield:                  bf,
		log:                       logger.New("torrent " + id),
		peerDisconnectedC:         make(chan *peer.Peer),
		messages:                  make(chan peer.Message),
		pieceMessagesC:            suspendchan.New[peer.PieceMessage](0),
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
		completeMetadataC:         make(chan struct{}),
		closeC:                    make(chan struct{}),
		startCommandC:             make(chan struct{}),
		stopCommandC:              make(chan struct{}),
		announceCommandC:          make(chan struct{}),
		verifyCommandC:            make(chan struct{}),
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
		externalIP:                externalip.FirstExternalIP(),
		downloadSpeed:             metrics.NilMeter{},
		uploadSpeed:               metrics.NilMeter{},
		bytesDownloaded:           metrics.NewCounter(),
		bytesUploaded:             metrics.NewCounter(),
		bytesWasted:               metrics.NewCounter(),
		seededFor:                 metrics.NewCounter(),
		ramNotifyC:                make(chan *peer.Peer),
		webseedClient:             &s.webseedClient,
		webseedSources:            ws,
		webseedPieceResultC:       suspendchan.New[*urldownloader.PieceResult](0),
		webseedRetryC:             make(chan *webseedsource.WebseedSource),
		doneC:                     make(chan struct{}),
		stopAfterDownload:         stopAfterDownload,
		stopAfterMetadata:         stopAfterMetadata,
		completeCmdRun:            completeCmdRun,
	}
	if len(t.webseedSources) > s.config.WebseedMaxSources {
		t.webseedSources = t.webseedSources[:10]
	}
	t.bytesDownloaded.Inc(stats.BytesDownloaded)
	t.bytesUploaded.Inc(stats.BytesUploaded)
	t.bytesWasted.Inc(stats.BytesWasted)
	t.seededFor.Inc(stats.SeededFor)
	var blocklistForOutgoingConns *blocklist.Blocklist
	if cfg.BlocklistEnabledForOutgoingConnections {
		blocklistForOutgoingConns = s.blocklist
	}
	t.addrList = addrlist.New(cfg.MaxPeerAddresses, blocklistForOutgoingConns, port, &t.externalIP)
	if t.info != nil {
		t.piecePool = bufferpool.New(int(t.info.PieceLength))
	}
	n := t.copyPeerIDPrefix()
	_, err := rand.Read(t.peerID[n:])
	if err != nil {
		return nil, err
	}
	t.unchoker = unchoker.New(cfg.UnchokedPeers, cfg.OptimisticUnchokedPeers)
	go t.run()
	return t, nil
}

func (t *torrent) copyPeerIDPrefix() int {
	if t.info != nil && t.info.Private {
		return copy(t.peerID[:], t.session.config.PrivatePeerIDPrefix)
	}
	return copy(t.peerID[:], publicPeerIDPrefix)
}

func (t *torrent) getPeersForUnchoker() []unchoker.Peer {
	peers := make([]unchoker.Peer, 0, len(t.peers))
	for pe := range t.peers {
		peers = append(peers, pe)
	}
	return peers
}

func (t *torrent) Name() string {
	return t.name
}

func (t *torrent) InfoHash() []byte {
	b := make([]byte, 20)
	copy(b, t.infoHash[:])
	return b
}

func (t *torrent) RootDirectory() string {
	return t.storage.RootDir()
}

func (t *torrent) FilePaths() ([]string, error) {
	if t.info == nil {
		return nil, errors.New("torrent metadata not ready")
	}

	var filePaths []string
	for _, f := range t.info.Files {
		if !f.Padding {
			filePaths = append(filePaths, f.Path)
		}
	}
	return filePaths, nil
}

func (t *torrent) Files() ([]File, error) {
	if t.info == nil || len(t.pieces) == 0 {
		return nil, errors.New("torrent not running so file stats unavailable")
	}

	fileComp := make(map[string]int64)
	for _, p := range t.pieces {
		if p.Done {
			for _, d := range p.Data {
				if !d.Padding {
					fileComp[d.Name] += d.Length
				}
			}
		}
	}

	var files []File
	for _, f := range t.info.Files {
		if !f.Padding {
			files = append(files,
				File{
					path: f.Path,
					stats: FileStats{
						BytesTotal:     f.Length,
						BytesCompleted: fileComp[f.Path],
					},
				})
		}
	}

	return files, nil
}

type FileStats struct {
	BytesTotal     int64
	BytesCompleted int64
}

type File struct {
	path  string
	stats FileStats
}

func (f File) Path() string {
	return f.path
}

func (f File) Stats() FileStats {
	return f.stats
}

func (t *torrent) announceDHT() {
	t.session.mPeerRequests.Lock()
	t.session.dhtPeerRequests[t] = struct{}{}
	t.session.mPeerRequests.Unlock()
}

// DisableLogging disables all log messages printed to console.
// This function needs to be called before creating a Session.
func DisableLogging() {
	logger.Disable()
}
