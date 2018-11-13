// Package torrent provides a BitTorrent client implementation for downlaoding a single torrent.
package torrent

import (
	"encoding/hex"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/clientversion"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent/dht"
	"github.com/cenkalti/rain/torrent/internal/acceptor"
	"github.com/cenkalti/rain/torrent/internal/addrlist"
	"github.com/cenkalti/rain/torrent/internal/allocator"
	"github.com/cenkalti/rain/torrent/internal/announcer"
	"github.com/cenkalti/rain/torrent/internal/bitfield"
	"github.com/cenkalti/rain/torrent/internal/handshaker/incominghandshaker"
	"github.com/cenkalti/rain/torrent/internal/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/torrent/internal/infodownloader"
	"github.com/cenkalti/rain/torrent/internal/peer"
	"github.com/cenkalti/rain/torrent/internal/piece"
	"github.com/cenkalti/rain/torrent/internal/piecedownloader"
	"github.com/cenkalti/rain/torrent/internal/piecepicker"
	"github.com/cenkalti/rain/torrent/internal/piecewriter"
	"github.com/cenkalti/rain/torrent/internal/torrentdata"
	"github.com/cenkalti/rain/torrent/internal/tracker"
	"github.com/cenkalti/rain/torrent/internal/tracker/trackermanager"
	"github.com/cenkalti/rain/torrent/internal/verifier"
	"github.com/cenkalti/rain/torrent/metainfo"
	"github.com/cenkalti/rain/torrent/resumer"
	"github.com/cenkalti/rain/torrent/storage"
)

var (
	// http://www.bittorrent.org/beps/bep_0020.html
	peerIDPrefix = []byte("-RN" + clientversion.Version + "-")

	// We send this in handshake tell supported extensions.
	ourExtensions = bitfield.New(64)
)

func init() {
	ourExtensions.Set(61) // Fast Extension (BEP 6)
	ourExtensions.Set(43) // Extension Protocol (BEP 10)
}

// Torrent connects to peers and downloads files from swarm.
type Torrent struct {
	config Config

	// Identifies the torrent being downloaded.
	infoHash [20]byte

	// List of addresses to announce this torrent.
	trackers []string

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

	// Unique peer ID is generated per downloader.
	peerID [20]byte

	// Data provides IO access to pieces in torrent.
	data *torrentdata.Data

	// Contains state about the pieces in torrent.
	pieces []*piece.Piece

	piecePicker *piecepicker.PiecePicker

	// Peers are sent to this channel when they are disconnected.
	peerDisconnectedC chan *peer.Peer

	// All messages coming from peers are sent to this channel.
	messages chan peer.Message

	// We keep connected peers in this map after they complete handshake phase.
	peers map[*peer.Peer]struct{}

	// Also keep a reference to incoming and outgoing peers seperately to count them quickly.
	incomingPeers map[*peer.Peer]struct{}
	outgoingPeers map[*peer.Peer]struct{}
	peersSnubbed  map[*peer.Peer]struct{}

	// Active piece downloads are kept in this map.
	pieceDownloaders        map[*peer.Peer]*piecedownloader.PieceDownloader
	pieceDownloadersSnubbed map[*peer.Peer]*piecedownloader.PieceDownloader
	pieceDownloadersChoked  map[*peer.Peer]*piecedownloader.PieceDownloader

	// When a peer has snubbed us, a message sent to this channel.
	peerSnubbedC chan *peer.Peer

	// Active metadata downloads are kept in this map.
	infoDownloaders        map[*peer.Peer]*infodownloader.InfoDownloader
	infoDownloadersSnubbed map[*peer.Peer]*infodownloader.InfoDownloader

	pieceWriters       map[*piecewriter.PieceWriter]struct{}
	pieceWriterResultC chan *piecewriter.PieceWriter

	// Some peers are optimistically unchoked regardless of their download rate.
	optimisticUnchokedPeers []*peer.Peer

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

	// These are the channels for sending a message to run() loop.
	statsCommandC        chan statsRequest        // Stats()
	trackersCommandC     chan trackersRequest     // Trackers()
	peersCommandC        chan peersRequest        // Peers()
	startCommandC        chan struct{}            // Start()
	stopCommandC         chan struct{}            // Stop()
	notifyErrorCommandC  chan notifyErrorCommand  // NotifyError()
	notifyListenCommandC chan notifyListenCommand // NotifyListen()
	addPeersCommandC     chan []*net.TCPAddr      // AddPeers()

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

	// Tracker implementations for giving to announcers.
	trackersInstances []tracker.Tracker

	// Announces the status of torrent to trackers to get peer addresses periodically.
	announcers []*announcer.PeriodicalAnnouncer

	// This announcer announces Stopped event to the trackers after
	// all periodical trackers are closed.
	stoppedEventAnnouncer *announcer.StopAnnouncer

	// If not nil, torrent is announced to DHT periodically.
	dhtNode      dht.DHT
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

	// Announcers send a request to this channel to get information about the torrent.
	announcerRequestC chan *announcer.Request

	// A timer that ticks periodically to keep a certain number of peers unchoked.
	unchokeTimer  *time.Ticker
	unchokeTimerC <-chan time.Time

	// A timer that ticks periodically to keep a random peer unchoked regardless of its upload rate.
	optimisticUnchokeTimer  *time.Ticker
	optimisticUnchokeTimerC <-chan time.Time

	// A worker that opens and allocates files on the disk.
	allocator          *allocator.Allocator
	allocatorProgressC chan allocator.Progress
	allocatorResultC   chan *allocator.Allocator

	// A worker that does hash check of files on the disk.
	verifier          *verifier.Verifier
	verifierProgressC chan verifier.Progress
	verifierResultC   chan *verifier.Verifier

	// Byte stats
	bytesDownloaded int64
	bytesUploaded   int64
	bytesWasted     int64

	// Holds connected peer IPs so we don't dial/accept multiple connections to/from same IP.
	connectedPeerIPs map[string]struct{}

	// A signal sent to run() loop when announcers are stopped.
	announcersStoppedC chan struct{}

	log logger.Logger
}

// Name of the torrent.
// For magnet downloads name can change after metadata is downloaded but this method still returns the initial name.
// Use Stats() method to get name in info dictionary.
func (t *Torrent) Name() string {
	return t.name
}

// InfoHash string encoded in hex as 40 charachters.
// InfoHash is a unique value that identifies the files in torrent.
func (t *Torrent) InfoHash() string {
	return hex.EncodeToString(t.infoHash[:])
}

// InfoHashBytes return info hash as 20 bytes.
func (t *Torrent) InfoHashBytes() []byte {
	b := make([]byte, 20)
	copy(b, t.infoHash[:])
	return b
}

func (t *Torrent) SetDHT(node dht.DHT) {
	t.dhtNode = node
	t.dhtPeersC = node.Peers()
}

func (t *Torrent) writeResume() error {
	if t.resume == nil {
		return nil
	}
	rspec := &resumer.Spec{
		InfoHash:    t.infoHash[:],
		Port:        t.port,
		Name:        t.name,
		Trackers:    t.trackers,
		StorageType: t.storage.Type(),
		StorageArgs: t.storage.Args(),
	}
	if t.info != nil {
		rspec.Info = t.info.Bytes
	}
	if t.bitfield != nil {
		rspec.Bitfield = t.bitfield.Bytes()
	}
	return t.resume.Write(rspec)
}

func parseTrackers(trackers []string, log logger.Logger, httpTimeout time.Duration) ([]tracker.Tracker, error) {
	var ret []tracker.Tracker
	for _, s := range trackers {
		t, err := trackermanager.DefaultTrackerManager.Get(s, httpTimeout)
		if err != nil {
			log.Warningln("cannot parse tracker url:", err)
			continue
		}
		ret = append(ret, t)
	}
	return ret, nil
}
