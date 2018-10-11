// Package torrent provides a torrent client implementation.
package torrent

import (
	"encoding/hex"
	"errors"
	"io"
	"math/rand"
	"net"
	"net/url"
	"time"

	"github.com/cenkalti/rain/internal/clientversion"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/resume"
	"github.com/cenkalti/rain/storage"
	"github.com/cenkalti/rain/storage/filestorage"
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
	"github.com/cenkalti/rain/torrent/internal/peerconn"
	"github.com/cenkalti/rain/torrent/internal/piece"
	"github.com/cenkalti/rain/torrent/internal/piecedownloader"
	"github.com/cenkalti/rain/torrent/internal/piecewriter"
	"github.com/cenkalti/rain/torrent/internal/torrentdata"
	"github.com/cenkalti/rain/torrent/internal/tracker"
	"github.com/cenkalti/rain/torrent/internal/tracker/httptracker"
	"github.com/cenkalti/rain/torrent/internal/tracker/udptracker"
	"github.com/cenkalti/rain/torrent/internal/verifier"
)

const (
	parallelInfoDownloads  = 4
	parallelPieceDownloads = 4
	parallelPieceWrites    = 4 // TODO remove this
	maxPeerDial            = 40
	maxPeerAccept          = 40
)

var (
	// http://www.bittorrent.org/beps/bep_0020.html
	peerIDPrefix = []byte("-RN" + clientversion.Version + "-")
)

// Torrent connects to peers and downloads files from swarm.
type Torrent struct {
	*downloadSpec

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
	connectedPeers map[*peerconn.Conn]*peer.Peer
	incomingPeers  map[*peerconn.Conn]struct{}
	outgoingPeers  map[*peerconn.Conn]struct{}

	// Active piece downloads are kept in this map.
	pieceDownloads map[*peerconn.Conn]*piecedownloader.PieceDownloader

	// Active metadata downloads are kept in this map.
	infoDownloads map[*peerconn.Conn]*infodownloader.InfoDownloader

	// Downloader run loop sends a message to this channel for writing a piece to disk.
	writeRequestC chan piecewriter.Request

	// When a piece is written to the disk, a message is sent to this channel.
	writeResponseC chan piecewriter.Response

	// A peer is optimistically unchoked regardless of their download rate.
	optimisticUnchokedPeer *peer.Peer

	// This channel is closed once all pieces are downloaded and verified.
	completeC chan struct{}

	// If any unrecoverable error occurs, it will be sent to this channel and download will be stopped.
	errC chan error

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
	newInConnC chan net.Conn

	// Keep a set of peer IDs to block duplicate connections.
	peerIDs map[[20]byte]struct{}

	// Listens for incoming peer connections.
	acceptor *acceptor.Acceptor

	// Special hash of info hash for encypted connection handshake.
	sKeyHash [20]byte

	// Responsible for writing downloaded pieces to disk.
	pieceWriters []*piecewriter.PieceWriter

	// Tracker implementations for giving to announcers.
	trackersInstances []tracker.Tracker

	// Announces the status of torrent to trackers to get peer addresses.
	announcers []*announcer.Announcer

	// List of peers in handshake state.
	incomingHandshakers map[string]*incominghandshaker.IncomingHandshaker
	outgoingHandshakers map[string]*outgoinghandshaker.OutgoingHandshaker

	// Handshake results are sent to these channels by handshakers.
	incomingHandshakerResultC chan incominghandshaker.Result
	outgoingHandshakerResultC chan outgoinghandshaker.Result

	// When metadata of the torrent downloaded completely, a message is sent to this channel.
	infoDownloaderResultC chan infodownloader.Result

	// When a piece is downloaded completely a message is sent to this channel.
	pieceDownloaderResultC chan piecedownloader.Result

	// Announcers send a request to this channel to get information about the torrent.
	announcerRequests chan *announcer.Request

	// A timer that ticks periodically to keep a certain number of peers unchoked.
	unchokeTimer  *time.Ticker
	unchokeTimerC <-chan time.Time

	// A timer that ticks periodically to keep a random peer unchoked regardless of its upload rate.
	optimisticUnchokeTimer  *time.Ticker
	optimisticUnchokeTimerC <-chan time.Time

	allocator          *allocator.Allocator
	allocatorProgressC chan allocator.Progress
	allocatorResultC   chan allocator.Result

	verifier          *verifier.Verifier
	verifierProgressC chan verifier.Progress
	verifierResultC   chan verifier.Result

	log logger.Logger
}

// New returns a new torrent by reading a torrent metainfo file.
func New(r io.Reader, port int, sto storage.Storage) (*Torrent, error) {
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
	return newTorrent(spec)
}

// NewMagnet returns a new torrent by parsing a magnet link.
func NewMagnet(link string, port int, sto storage.Storage) (*Torrent, error) {
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
	return newTorrent(spec)
}

// NewResume returns a new torrent by loading all info from a resume.DB.
func NewResume(res resume.DB) (*Torrent, error) {
	spec, err := res.Read()
	if err != nil {
		return nil, err
	}
	if spec == nil {
		return nil, errors.New("no resume info")
	}
	return loadResumeSpec(spec, res)
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
func (t *Torrent) SetResume(res resume.DB) error {
	spec, err := res.Read()
	if err != nil {
		return err
	}
	if spec == nil {
		return t.writeResume(res)
	}
	t2, err := loadResumeSpec(spec, res)
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

func loadResumeSpec(spec *resume.Spec, res resume.DB) (*Torrent, error) {
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
	return newTorrent(dspec)
}

func (t *Torrent) writeResume(res resume.DB) error {
	rspec := &resume.Spec{
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

func newTorrent(spec *downloadSpec) (*Torrent, error) {
	logName := spec.name
	if len(logName) > 8 {
		logName = logName[:8]
	}
	d := &Torrent{
		downloadSpec:              spec,
		log:                       logger.New("download " + logName),
		peerDisconnectedC:         make(chan *peer.Peer),
		messages:                  make(chan peer.Message),
		connectedPeers:            make(map[*peerconn.Conn]*peer.Peer),
		incomingPeers:             make(map[*peerconn.Conn]struct{}),
		outgoingPeers:             make(map[*peerconn.Conn]struct{}),
		pieceDownloads:            make(map[*peerconn.Conn]*piecedownloader.PieceDownloader),
		infoDownloads:             make(map[*peerconn.Conn]*infodownloader.InfoDownloader),
		writeRequestC:             make(chan piecewriter.Request),
		writeResponseC:            make(chan piecewriter.Response),
		completeC:                 make(chan struct{}),
		closeC:                    make(chan struct{}),
		doneC:                     make(chan struct{}),
		statsCommandC:             make(chan statsRequest),
		notifyErrorCommandC:       make(chan notifyErrorCommand),
		addrsFromTrackers:         make(chan []*net.TCPAddr),
		addrList:                  addrlist.New(),
		peerIDs:                   make(map[[20]byte]struct{}),
		newInConnC:                make(chan net.Conn),
		sKeyHash:                  mse.HashSKey(spec.infoHash[:]),
		infoDownloaderResultC:     make(chan infodownloader.Result),
		pieceDownloaderResultC:    make(chan piecedownloader.Result),
		incomingHandshakers:       make(map[string]*incominghandshaker.IncomingHandshaker),
		outgoingHandshakers:       make(map[string]*outgoinghandshaker.OutgoingHandshaker),
		incomingHandshakerResultC: make(chan incominghandshaker.Result),
		outgoingHandshakerResultC: make(chan outgoinghandshaker.Result),
		startCommandC:             make(chan struct{}),
		stopCommandC:              make(chan struct{}),
		announcerRequests:         make(chan *announcer.Request),
		allocatorProgressC:        make(chan allocator.Progress),
		allocatorResultC:          make(chan allocator.Result),
		verifierProgressC:         make(chan verifier.Progress),
		verifierResultC:           make(chan verifier.Result),
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
		u, err := url.Parse(s)
		if err != nil {
			log.Warningln("cannot parse tracker url:", err)
			continue
		}
		switch u.Scheme {
		case "http", "https":
			ret = append(ret, httptracker.New(u))
		case "udp":
			ret = append(ret, udptracker.New(u))
		default:
			log.Warningln("unsupported tracker scheme: %s", u.Scheme)
		}
	}
	if len(ret) == 0 {
		return nil, errors.New("no tracker found")
	}
	return ret, nil
}
