// Package torrent provides a torrent client implementation for downlaoding a single torrent.
package torrent

import (
	"encoding/hex"
	"errors"
	"io"
	"math/rand"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/clientversion"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent/internal/acceptor"
	"github.com/cenkalti/rain/torrent/internal/addrlist"
	"github.com/cenkalti/rain/torrent/internal/allocator"
	"github.com/cenkalti/rain/torrent/internal/announcer"
	"github.com/cenkalti/rain/torrent/internal/bitfield"
	"github.com/cenkalti/rain/torrent/internal/handshaker/incominghandshaker"
	"github.com/cenkalti/rain/torrent/internal/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/torrent/internal/infodownloader"
	"github.com/cenkalti/rain/torrent/internal/magnet"
	"github.com/cenkalti/rain/torrent/internal/metainfo"
	"github.com/cenkalti/rain/torrent/internal/mse"
	"github.com/cenkalti/rain/torrent/internal/peer"
	"github.com/cenkalti/rain/torrent/internal/piece"
	"github.com/cenkalti/rain/torrent/internal/piecedownloader"
	"github.com/cenkalti/rain/torrent/internal/torrentdata"
	"github.com/cenkalti/rain/torrent/internal/tracker"
	"github.com/cenkalti/rain/torrent/internal/tracker/trackermanager"
	"github.com/cenkalti/rain/torrent/internal/verifier"
	"github.com/cenkalti/rain/torrent/resumer"
	"github.com/cenkalti/rain/torrent/storage"
	"github.com/cenkalti/rain/torrent/storage/filestorage"
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
	*downloadSpec
	config Config

	// Unique peer ID is generated per downloader.
	peerID [20]byte

	// Data provides IO access to pieces in torrent.
	data *torrentdata.Data

	// Contains state about the pieces in torrent.
	pieces []*piece.Piece

	// Contains pieces in sorted order for piece selection function.
	sortedPieces []*piece.Piece

	// Peers are sent to this channel when they are disconnected.
	peerDisconnectedC chan *peer.Peer

	// All messages coming from peers are sent to this channel.
	messages chan peer.Message

	// We keep connected peers in this map after they complete handshake phase.
	peers         map[*peer.Peer]struct{}
	incomingPeers map[*peer.Peer]struct{}
	outgoingPeers map[*peer.Peer]struct{}
	peersSnubbed  map[*peer.Peer]struct{}

	// Active piece downloads are kept in this map.
	pieceDownloaders        map[*peer.Peer]*piecedownloader.PieceDownloader
	pieceDownloadersSnubbed map[*peer.Peer]*piecedownloader.PieceDownloader
	pieceDownloadersChoked  map[*peer.Peer]*piecedownloader.PieceDownloader

	// When piece downloaders detects that a peer has snubbed us, it will send a signal to this channel.
	snubbedPieceDownloaderC chan *piecedownloader.PieceDownloader
	snubbedInfoDownloaderC  chan *infodownloader.InfoDownloader

	// Active metadata downloads are kept in this map.
	infoDownloaders        map[*peer.Peer]*infodownloader.InfoDownloader
	infoDownloadersSnubbed map[*peer.Peer]*infodownloader.InfoDownloader

	// A peer is optimistically unchoked regardless of their download rate.
	optimisticUnchokedPeer *peer.Peer

	// This channel is closed once all pieces are downloaded and verified.
	completeC chan struct{}

	// True after all pieces are download, verified and written to disk.
	completed bool

	// If any unrecoverable error occurs, it will be sent to this channel and download will be stopped.
	errC chan error

	// Contains the last error sent to errC.
	lastError error

	// When Stop() is called, it will close this channel to signal run() function to stop.
	closeC chan struct{}

	// This channel will be closed after run loop exists.
	doneC chan struct{}

	// These are the channels for sending a message to run() loop.
	statsCommandC       chan statsRequest       // Stats()
	startCommandC       chan struct{}           // Start()
	stopCommandC        chan struct{}           // Stop()
	notifyErrorCommandC chan notifyErrorCommand // NotifyError()

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

	// List of peers in handshake state.
	incomingHandshakers map[*incominghandshaker.IncomingHandshaker]struct{}
	outgoingHandshakers map[*outgoinghandshaker.OutgoingHandshaker]struct{}

	// Handshake results are sent to these channels by handshakers.
	incomingHandshakerResultC chan *incominghandshaker.IncomingHandshaker
	outgoingHandshakerResultC chan *outgoinghandshaker.OutgoingHandshaker

	// When metadata of the torrent downloaded completely, a message is sent to this channel.
	infoDownloaderResultC chan *infodownloader.InfoDownloader

	// When a piece is downloaded completely a message is sent to this channel.
	pieceDownloaderResultC chan *piecedownloader.PieceDownloader

	// Announcers send a request to this channel to get information about the torrent.
	announcerRequestC chan *announcer.Request

	// A timer that ticks periodically to keep a certain number of peers unchoked.
	unchokeTimer  *time.Ticker
	unchokeTimerC <-chan time.Time

	// A timer that ticks periodically to keep a random peer unchoked regardless of its upload rate.
	optimisticUnchokeTimer  *time.Ticker
	optimisticUnchokeTimerC <-chan time.Time

	allocator          *allocator.Allocator
	allocatorProgressC chan allocator.Progress
	allocatorResultC   chan *allocator.Allocator

	verifier          *verifier.Verifier
	verifierProgressC chan verifier.Progress
	verifierResultC   chan *verifier.Verifier

	bytesDownloaded int64
	bytesUploaded   int64
	bytesWasted     int64

	// Holds connected peer IPs so we don't dial/accept multiple connections to/from same IP.
	connectedPeerIPs map[string]struct{}

	// A signal sent to run() loop when announcers are stopped.
	announcersStoppedC chan struct{}

	uploadByteCounterC chan int64

	log logger.Logger
}

// New returns a new torrent by reading a torrent metainfo file.
func New(r io.Reader, port int, sto storage.Storage, cfg Config) (*Torrent, error) {
	m, err := metainfo.New(r)
	if err != nil {
		return nil, err
	}
	spec := &downloadSpec{
		infoHash: m.Info.Hash,
		trackers: m.GetTrackers(),
		name:     m.Info.Name,
		port:     port,
		storage:  sto,
		info:     m.Info,
	}
	return newTorrent(spec, cfg)
}

// NewMagnet returns a new torrent by parsing a magnet link.
func NewMagnet(link string, port int, sto storage.Storage, cfg Config) (*Torrent, error) {
	m, err := magnet.New(link)
	if err != nil {
		return nil, err
	}
	spec := &downloadSpec{
		infoHash: m.InfoHash,
		trackers: m.Trackers,
		name:     m.Name,
		port:     port,
		storage:  sto,
	}
	return newTorrent(spec, cfg)
}

// NewResume returns a new torrent by loading all info from a resume.DB.
func NewResume(res resumer.Resumer, cfg Config) (*Torrent, error) {
	spec, err := res.Read()
	if err != nil {
		return nil, err
	}
	if spec == nil {
		return nil, errors.New("no resume info")
	}
	return loadResumeSpec(spec, res, cfg)
}

// Name of the torrent.
// For magnet downloads name can change after metadata is downloaded but this method still returns the initial name.
func (t *Torrent) Name() string {
	return t.name
}

// InfoHash string encoded in hex.
// InfoHash is a unique value that identifies the files in torrent.
func (t *Torrent) InfoHash() string {
	return hex.EncodeToString(t.infoHash[:])
}

// SetResume adds resume capability to the torrent.
// It must be called before Start() is called.
func (t *Torrent) SetResume(res resumer.Resumer) error {
	spec, err := res.Read()
	if err != nil {
		return err
	}
	if spec == nil {
		return t.writeResume(res)
	}
	t2, err := loadResumeSpec(spec, res, t.config)
	if err != nil {
		return err
	}
	if t.InfoHash() != t2.InfoHash() {
		t2.Close()
		return errors.New("invalid resume file (info hashes does not match)")
	}
	t.Close()
	*t = *t2
	return nil
}

func loadResumeSpec(spec *resumer.Spec, res resumer.Resumer, cfg Config) (*Torrent, error) {
	var err error
	dspec := &downloadSpec{
		port:     spec.Port,
		trackers: spec.Trackers,
		name:     spec.Name,
		resume:   res,
	}
	copy(dspec.infoHash[:], spec.InfoHash)
	if len(spec.Info) > 0 {
		dspec.info, err = metainfo.NewInfo(spec.Info)
		if err != nil {
			return nil, err
		}
		if len(spec.Bitfield) > 0 {
			dspec.bitfield = bitfield.New(dspec.info.NumPieces)
			copy(dspec.bitfield.Bytes(), spec.Bitfield)
		}
	}
	switch spec.StorageType {
	case filestorage.StorageType:
		dspec.storage = &filestorage.FileStorage{}
	default:
		return nil, errors.New("unknown storage type: " + spec.StorageType)
	}
	err = dspec.storage.Load(spec.StorageArgs)
	if err != nil {
		return nil, err
	}
	return newTorrent(dspec, cfg)
}

func (t *Torrent) writeResume(res resumer.Resumer) error {
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
	return res.Write(rspec)
}

func newTorrent(spec *downloadSpec, cfg Config) (*Torrent, error) {
	logName := spec.name
	if len(logName) > 8 {
		logName = logName[:8]
	}
	d := &Torrent{
		downloadSpec:              spec,
		config:                    cfg,
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
		snubbedPieceDownloaderC:   make(chan *piecedownloader.PieceDownloader),
		snubbedInfoDownloaderC:    make(chan *infodownloader.InfoDownloader),
		infoDownloaders:           make(map[*peer.Peer]*infodownloader.InfoDownloader),
		infoDownloadersSnubbed:    make(map[*peer.Peer]*infodownloader.InfoDownloader),
		completeC:                 make(chan struct{}),
		closeC:                    make(chan struct{}),
		doneC:                     make(chan struct{}),
		startCommandC:             make(chan struct{}),
		stopCommandC:              make(chan struct{}),
		statsCommandC:             make(chan statsRequest),
		notifyErrorCommandC:       make(chan notifyErrorCommand),
		addrsFromTrackers:         make(chan []*net.TCPAddr),
		addrList:                  addrlist.New(),
		peerIDs:                   make(map[[20]byte]struct{}),
		incomingConnC:             make(chan net.Conn),
		sKeyHash:                  mse.HashSKey(spec.infoHash[:]),
		infoDownloaderResultC:     make(chan *infodownloader.InfoDownloader),
		pieceDownloaderResultC:    make(chan *piecedownloader.PieceDownloader),
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
		uploadByteCounterC:        make(chan int64),
	}
	copy(d.peerID[:], peerIDPrefix)
	_, err := rand.Read(d.peerID[len(peerIDPrefix):]) // nolint: gosec
	if err != nil {
		return nil, err
	}
	d.trackersInstances, err = parseTrackers(d.trackers, d.log)
	if err != nil {
		return nil, err
	}
	go d.run()
	return d, nil
}

func parseTrackers(trackers []string, log logger.Logger) ([]tracker.Tracker, error) {
	var ret []tracker.Tracker
	for _, s := range trackers {
		t, err := trackermanager.DefaultTrackerManager.Get(s)
		if err != nil {
			log.Warningln("cannot parse tracker url:", err)
			continue
		}
		ret = append(ret, t)
	}
	if len(ret) == 0 {
		return nil, errors.New("no tracker found")
	}
	return ret, nil
}
