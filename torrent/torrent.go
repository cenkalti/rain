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

	"github.com/cenkalti/rain/resume"
	"github.com/cenkalti/rain/storage"
	"github.com/cenkalti/rain/storage/filestorage"
	"github.com/cenkalti/rain/torrent/internal/acceptor"
	"github.com/cenkalti/rain/torrent/internal/addrlist"
	"github.com/cenkalti/rain/torrent/internal/announcer"
	"github.com/cenkalti/rain/torrent/internal/bitfield"
	"github.com/cenkalti/rain/torrent/internal/clientversion"
	"github.com/cenkalti/rain/torrent/internal/handshaker/incominghandshaker"
	"github.com/cenkalti/rain/torrent/internal/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/torrent/internal/infodownloader"
	"github.com/cenkalti/rain/torrent/internal/logger"
	"github.com/cenkalti/rain/torrent/internal/magnet"
	"github.com/cenkalti/rain/torrent/internal/metainfo"
	"github.com/cenkalti/rain/torrent/internal/mse"
	"github.com/cenkalti/rain/torrent/internal/peer"
	"github.com/cenkalti/rain/torrent/internal/peerconn"
	"github.com/cenkalti/rain/torrent/internal/piece"
	"github.com/cenkalti/rain/torrent/internal/piecedownloader"
	"github.com/cenkalti/rain/torrent/internal/piecewriter"
	"github.com/cenkalti/rain/torrent/internal/semaphore"
	"github.com/cenkalti/rain/torrent/internal/torrentdata"
	"github.com/cenkalti/rain/torrent/internal/tracker"
	"github.com/cenkalti/rain/torrent/internal/tracker/httptracker"
	"github.com/cenkalti/rain/torrent/internal/tracker/udptracker"
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
	spec *downloadSpec

	// Identifies the torrent being downloaded.
	infoHash [20]byte

	// Unique peer ID is generated per downloader.
	peerID [20]byte

	// TCP Port to listen for peer connections.
	port int

	// List of addresses to announce this torrent.
	trackers []string

	// Storage implementation to save the files in torrent.
	storage storage.Storage

	// Optional DB implementation to save resume state of the torrent.
	resume resume.DB

	// Contains info about files in torrent. This can be nil at start for magnet downloads.
	info *metainfo.Info

	// Bitfield for pieces we have. It is created after we got info.
	bitfield *bitfield.Bitfield

	// Data provides IO access to pieces in torrent.
	data *torrentdata.Data

	// Boolean state to to tell if all pieces are downloaded.
	completed bool

	// Contains state about the pieces in torrent.
	pieces []piece.Piece

	// Contains pieces in sorted order for piece selection function.
	sortedPieces []*piece.Piece

	// Peers are sent to this channel when they are disconnected.
	peerDisconnectedC chan *peer.Peer

	// All messages coming from peers are sent to this channel.
	messages chan peer.Message

	// We keep connected peers in this map after they complete handshake phase.
	connectedPeers map[*peerconn.Peer]*peer.Peer

	// Active piece downloads are kept in this map.
	pieceDownloads map[*peerconn.Peer]*piecedownloader.PieceDownloader

	// Active metadata downloads are kept in this map.
	infoDownloads map[*peerconn.Peer]*infodownloader.InfoDownloader

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
	closedC chan struct{}

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

	// We keep connected and handshake completed peers here.
	incomingPeers []*peer.Peer
	outgoingPeers []*peer.Peer

	// When metadata of the torrent downloaded completely, a message is sent to this channel.
	infoDownloaderResultC chan infodownloader.Result

	// When a piece is downloaded completely a message is sent to this channel.
	pieceDownloaderResultC chan piecedownloader.Result

	// True after downloader is started with Start() method, false after Stop() is called.
	running bool

	// Announcers send a request to this channel to get information about the torrent.
	announcerRequests chan *announcer.Request

	// A timer that ticks periodically to keep a certain number of peers unchoked.
	unchokeTimer  *time.Ticker
	unchokeTimerC <-chan time.Time

	// A timer that ticks periodically to keep a random peer unchoked regardless of its upload rate.
	optimisticUnchokeTimer  *time.Ticker
	optimisticUnchokeTimerC <-chan time.Time

	// To limit the max number of peers to connect to.
	dialLimit *semaphore.Semaphore

	// To limit the max number of parallel piece downloads.
	pieceDownloaders *semaphore.Semaphore

	// To limit the max number of parallel metadata downloads.
	infoDownloaders *semaphore.Semaphore

	log logger.Logger
}

// New returns a new torrent by reading a torrent metainfo file.
func New(r io.Reader, port int, sto storage.Storage) (*Torrent, error) {
	m, err := metainfo.New(r)
	if err != nil {
		return nil, err
	}
	spec := &downloadSpec{
		InfoHash: m.Info.Hash,
		Trackers: m.GetTrackers(),
		Name:     m.Info.Name,
		Port:     port,
		Storage:  sto,
		Info:     m.Info,
	}
	return newTorrent(spec)
}

// NewMagnet returns a new torrent by parsing a magnet link.
func NewMagnet(magnetLink string, port int, sto storage.Storage) (*Torrent, error) {
	m, err := magnet.New(magnetLink)
	if err != nil {
		return nil, err
	}
	spec := &downloadSpec{
		InfoHash: m.InfoHash,
		Trackers: m.Trackers,
		Name:     m.Name,
		Port:     port,
		Storage:  sto,
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
	return t.spec.Name
}

// InfoHash string encoded in hex.
// InfoHash is a unique value that identifies the files in torrent.
func (t *Torrent) InfoHash() string {
	return hex.EncodeToString(t.spec.InfoHash[:])
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
		Port:     spec.Port,
		Trackers: spec.Trackers,
		Name:     spec.Name,
		Resume:   res,
	}
	copy(dspec.InfoHash[:], spec.InfoHash)
	if len(spec.Info) > 0 {
		dspec.Info, err = metainfo.NewInfo(spec.Info)
		if err != nil {
			return nil, err
		}
		if len(spec.Bitfield) > 0 {
			dspec.Bitfield = bitfield.New(dspec.Info.NumPieces)
			copy(dspec.Bitfield.Bytes(), spec.Bitfield)
		}
	}
	switch spec.StorageType {
	case filestorage.StorageType:
		dspec.Storage = &filestorage.FileStorage{}
	default:
		return nil, errors.New("unknown storage type: " + spec.StorageType)
	}
	err = dspec.Storage.Load(spec.StorageArgs)
	if err != nil {
		return nil, err
	}
	return newTorrent(dspec)
}

func (t *Torrent) writeResume(res resume.DB) error {
	rspec := &resume.Spec{
		InfoHash:    t.spec.InfoHash[:],
		Port:        t.spec.Port,
		Name:        t.spec.Name,
		Trackers:    t.spec.Trackers,
		StorageType: t.spec.Storage.Type(),
		StorageArgs: t.spec.Storage.Args(),
	}
	if t.spec.Info != nil {
		rspec.Info = t.spec.Info.Bytes
	}
	if t.spec.Bitfield != nil {
		rspec.Bitfield = t.spec.Bitfield.Bytes()
	}
	return res.Write(rspec)
}

func newTorrent(spec *downloadSpec) (*Torrent, error) {
	logName := spec.Name
	if len(logName) > 8 {
		logName = logName[:8]
	}
	d := &Torrent{
		spec:     spec,
		infoHash: spec.InfoHash,
		trackers: spec.Trackers,
		port:     spec.Port,
		storage:  spec.Storage,
		resume:   spec.Resume,
		info:     spec.Info,
		bitfield: spec.Bitfield,
		log:      logger.New("download " + logName),

		peerDisconnectedC:         make(chan *peer.Peer),
		messages:                  make(chan peer.Message),
		connectedPeers:            make(map[*peerconn.Peer]*peer.Peer),
		pieceDownloads:            make(map[*peerconn.Peer]*piecedownloader.PieceDownloader),
		infoDownloads:             make(map[*peerconn.Peer]*infodownloader.InfoDownloader),
		writeRequestC:             make(chan piecewriter.Request),
		writeResponseC:            make(chan piecewriter.Response),
		completeC:                 make(chan struct{}),
		closeC:                    make(chan struct{}),
		closedC:                   make(chan struct{}),
		statsCommandC:             make(chan statsRequest),
		notifyErrorCommandC:       make(chan notifyErrorCommand),
		addrsFromTrackers:         make(chan []*net.TCPAddr),
		addrList:                  addrlist.New(),
		peerIDs:                   make(map[[20]byte]struct{}),
		newInConnC:                make(chan net.Conn),
		sKeyHash:                  mse.HashSKey(spec.InfoHash[:]),
		infoDownloaderResultC:     make(chan infodownloader.Result),
		pieceDownloaderResultC:    make(chan piecedownloader.Result),
		incomingHandshakers:       make(map[string]*incominghandshaker.IncomingHandshaker),
		outgoingHandshakers:       make(map[string]*outgoinghandshaker.OutgoingHandshaker),
		incomingHandshakerResultC: make(chan incominghandshaker.Result),
		outgoingHandshakerResultC: make(chan outgoinghandshaker.Result),
		startCommandC:             make(chan struct{}),
		stopCommandC:              make(chan struct{}),
		announcerRequests:         make(chan *announcer.Request),
		dialLimit:                 semaphore.New(maxPeerDial),
		pieceDownloaders:          semaphore.New(parallelPieceDownloads),
		infoDownloaders:           semaphore.New(parallelPieceDownloads),
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
