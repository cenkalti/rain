package torrent

import (
	"log"
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
	"github.com/cenkalti/rain/internal/handshaker/incominghandshaker"
	"github.com/cenkalti/rain/internal/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/internal/infodownloader"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piece"
	"github.com/cenkalti/rain/internal/piececache"
	"github.com/cenkalti/rain/internal/piecedownloader"
	"github.com/cenkalti/rain/internal/piecepicker"
	"github.com/cenkalti/rain/internal/piecewriter"
	"github.com/cenkalti/rain/internal/resourcemanager"
	"github.com/cenkalti/rain/internal/resumer"
	"github.com/cenkalti/rain/internal/storage"
	"github.com/cenkalti/rain/internal/suspendchan"
	"github.com/cenkalti/rain/internal/tracker"
	"github.com/cenkalti/rain/internal/unchoker"
	"github.com/cenkalti/rain/internal/verifier"
	"github.com/cenkalti/rain/internal/webseeddownloader"
	"github.com/rcrowley/go-metrics"
)

var (
	// We send this in handshake tell supported extensions.
	ourExtensions [8]byte
)

func init() {
	bf, err := bitfield.NewBytes(ourExtensions[:], 64)
	if err != nil {
		log.Fatal(err)
	}
	bf.Set(61) // Fast Extension (BEP 6)
	bf.Set(43) // Extension Protocol (BEP 10)
}

// torrent connects to peers and downloads files from swarm.
type torrent struct {
	config Config

	// Identifies the torrent being downloaded.
	infoHash [20]byte

	// List of addresses to announce this torrent.
	trackers []tracker.Tracker

	// Name of the torrent.
	name string

	// Storage implementation to save the files in torrent.
	storage storage.Storage

	// TCP Port to listen for peer connections.
	port int

	// Optional DB implementation to save resume state of the torrent.
	resume resumer.Resumer

	// Contains info about files in torrent. This can be nil at start for magnet downloads.
	info *metainfo.Info

	// Bitfield for pieces we have. It is created after we got info.
	bitfield *bitfield.Bitfield

	// Protects bitfield writing from torrent loop and reading from announcer loop.
	mBitfield sync.RWMutex

	// Unique peer ID is generated per downloader.
	peerID [20]byte

	files  []storage.File
	pieces []piece.Piece

	piecePicker *piecepicker.PiecePicker

	// Peers are sent to this channel when they are disconnected.
	peerDisconnectedC chan *peer.Peer

	// Piece messages coming from peers are sent this channel.
	pieceMessagesC *suspendchan.Chan

	// Other messages coming from peers are sent to this channel.
	messages chan peer.Message

	// We keep connected peers in this map after they complete handshake phase.
	peers map[*peer.Peer]struct{}

	// Also keep a reference to incoming and outgoing peers separately to count them quickly.
	incomingPeers map[*peer.Peer]struct{}
	outgoingPeers map[*peer.Peer]struct{}

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

	// This channel is closed once all pieces are downloaded and verified.
	completeC chan struct{}

	// True after all pieces are download, verified and written to disk.
	completed bool

	// If any unrecoverable error occurs, it will be sent to this channel and download will be stopped.
	errC chan error

	// After listener has started, port will be sent to this channel.
	portC chan int

	// Contains the last error sent to errC.
	lastError error

	// When Stop() is called, it will close this channel to signal run() function to stop.
	closeC chan chan struct{}

	// Close() blocks until doneC is closed.
	doneC chan struct{}

	// These are the channels for sending a message to run() loop.
	statsCommandC        chan statsRequest        // Stats()
	trackersCommandC     chan trackersRequest     // Trackers()
	peersCommandC        chan peersRequest        // Peers()
	startCommandC        chan struct{}            // Start()
	stopCommandC         chan struct{}            // Stop()
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
	dhtNode      *dhtAnnouncer
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

	resumerStats          resumer.Stats
	seedDurationUpdatedAt time.Time
	seedDurationTicker    *time.Ticker

	// Holds connected peer IPs so we don't dial/accept multiple connections to/from same IP.
	connectedPeerIPs map[string]struct{}

	// A signal sent to run() loop when announcers are stopped.
	announcersStoppedC chan struct{}

	// Piece buffers that are being downloaded are pooled to reduce load on GC.
	piecePool *bufferpool.Pool

	// Keep a timer to write bitfield at interval to reduce IO.
	resumeWriteTimer  *time.Timer
	resumeWriteTimerC <-chan time.Time

	// Keeps blocks read from disk in memory.
	pieceCache *piececache.Cache

	// Optional list of IP addresses to block.
	blocklist *blocklist.Blocklist

	// Used to calculate canonical peer priority (BEP 40).
	// Initialized with value found in network interfaces.
	// Then, updated from "yourip" field in BEP 10 extension handshake message.
	externalIP net.IP

	// Rate counters for download and upload speeds.
	downloadSpeed      metrics.EWMA
	uploadSpeed        metrics.EWMA
	speedCounterTicker *time.Ticker

	ram        *resourcemanager.ResourceManager
	ramNotifyC chan interface{}

	webseedClient     *http.Client
	webseedSources    []string
	webseedDownloader *webseeddownloader.WebseedDownloader

	log logger.Logger
}

// Name of the torrent.
// For magnet downloads name can change after metadata is downloaded but this method still returns the initial name.
// Use Stats() method to get name in info dictionary.
func (t *torrent) Name() string {
	return t.name
}

// InfoHash is a 20-bytes value that identifies the files in torrent.
func (t *torrent) InfoHash() []byte {
	b := make([]byte, 20)
	copy(b, t.infoHash[:])
	return b
}
