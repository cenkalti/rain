package downloader

import (
	"bytes"
	"crypto/sha1" // nolint: gosec
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net"
	"net/url"
	"sort"
	"time"

	"github.com/cenkalti/rain/internal/announcer"
	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/downloader/acceptor"
	"github.com/cenkalti/rain/internal/downloader/addrlist"
	"github.com/cenkalti/rain/internal/downloader/handshaker/incominghandshaker"
	"github.com/cenkalti/rain/internal/downloader/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/internal/downloader/infodownloader"
	"github.com/cenkalti/rain/internal/downloader/peer"
	"github.com/cenkalti/rain/internal/downloader/piece"
	"github.com/cenkalti/rain/internal/downloader/piecedownloader"
	"github.com/cenkalti/rain/internal/downloader/piecewriter"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/mse"
	ip "github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peer/peerprotocol"
	"github.com/cenkalti/rain/internal/semaphore"
	"github.com/cenkalti/rain/internal/torrentdata"
	"github.com/cenkalti/rain/internal/tracker"
	"github.com/cenkalti/rain/internal/tracker/httptracker"
	"github.com/cenkalti/rain/internal/tracker/udptracker"
	"github.com/cenkalti/rain/internal/version"
	"github.com/cenkalti/rain/resume"
	"github.com/cenkalti/rain/storage"
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
	peerIDPrefix = []byte("-RN" + version.Version + "-")
)

type Downloader struct {
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
	connectedPeers map[*ip.Peer]*peer.Peer

	// Active piece downloads are kept in this map.
	pieceDownloads map[*ip.Peer]*piecedownloader.PieceDownloader

	// Active metadata downloads are kept in this map.
	infoDownloads map[*ip.Peer]*infodownloader.InfoDownloader

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
	statsCommandC chan StatsRequest // Stats()
	startCommandC chan startCommand // Start()
	stopCommandC  chan struct{}     // Stop()

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

func New(spec *Spec, l logger.Logger) (*Downloader, error) {
	d := &Downloader{
		infoHash:                  spec.InfoHash,
		trackers:                  spec.Trackers,
		port:                      spec.Port,
		storage:                   spec.Storage,
		resume:                    spec.Resume,
		info:                      spec.Info,
		bitfield:                  spec.Bitfield,
		peerDisconnectedC:         make(chan *peer.Peer),
		messages:                  make(chan peer.Message),
		connectedPeers:            make(map[*ip.Peer]*peer.Peer),
		pieceDownloads:            make(map[*ip.Peer]*piecedownloader.PieceDownloader),
		infoDownloads:             make(map[*ip.Peer]*infodownloader.InfoDownloader),
		writeRequestC:             make(chan piecewriter.Request),
		writeResponseC:            make(chan piecewriter.Response),
		completeC:                 make(chan struct{}),
		log:                       l,
		closeC:                    make(chan struct{}),
		closedC:                   make(chan struct{}),
		statsCommandC:             make(chan StatsRequest),
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
		startCommandC:             make(chan startCommand),
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

func (d *Downloader) NotifyComplete() chan struct{} {
	return d.completeC
}

func (d *Downloader) ErrC() chan error {
	return d.errC
}

type startCommand struct {
	errCC chan chan error
}

func (d *Downloader) Start() <-chan error {
	cmd := startCommand{errCC: make(chan chan error)}
	select {
	case d.startCommandC <- cmd:
		return <-cmd.errCC
	case <-d.closedC:
		return nil
	}
}

func (d *Downloader) Stop() {
	select {
	case d.stopCommandC <- struct{}{}:
	case <-d.closedC:
	}
}

func (d *Downloader) Close() {
	select {
	case d.closeC <- struct{}{}:
	case <-d.closedC:
	}
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

func (d *Downloader) start(cmd startCommand) {
	defer func() { cmd.errCC <- d.errC }()

	if d.running {
		return
	}
	d.running = true

	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{Port: d.port})
	if err != nil {
		d.log.Warningf("cannot listen port %d: %s", d.port, err)
	} else {
		d.log.Notice("Listening peers on tcp://" + listener.Addr().String())
		d.port = listener.Addr().(*net.TCPAddr).Port
		d.acceptor = acceptor.New(listener, d.newInConnC, d.log)
		go d.acceptor.Run()
	}

	for _, tr := range d.trackersInstances {
		an := announcer.New(tr, d.announcerRequests, d.completeC, d.addrsFromTrackers, d.log)
		d.announcers = append(d.announcers, an)
		go an.Run()
	}

	for i := 0; i < parallelPieceWrites; i++ {
		w := piecewriter.New(d.writeRequestC, d.writeResponseC, d.log)
		d.pieceWriters = append(d.pieceWriters, w)
		go w.Run()
	}

	d.unchokeTimer = time.NewTicker(10 * time.Second)
	d.unchokeTimerC = d.unchokeTimer.C

	d.optimisticUnchokeTimer = time.NewTicker(30 * time.Second)
	d.optimisticUnchokeTimerC = d.optimisticUnchokeTimer.C

	d.dialLimit.Start()

	d.pieceDownloaders.Start()
	d.infoDownloaders.Start()

	d.errC = make(chan error, 1)
}

func (d *Downloader) stop(err error) {
	if !d.running {
		return
	}
	d.running = false

	if d.acceptor != nil {
		d.acceptor.Close()
	}
	d.acceptor = nil

	for _, an := range d.announcers {
		an.Close()
	}
	d.announcers = nil

	for _, pw := range d.pieceWriters {
		pw.Close()
	}
	d.pieceWriters = nil

	d.unchokeTimer.Stop()
	d.unchokeTimerC = nil
	d.optimisticUnchokeTimer.Stop()
	d.optimisticUnchokeTimerC = nil

	d.dialLimit.Stop()

	d.pieceDownloaders.Stop()
	d.infoDownloaders.Stop()

	if err != nil {
		d.errC <- err
	}
}

func (d *Downloader) close() {
	d.stop(errors.New("torrent is closed"))

	for _, oh := range d.outgoingHandshakers {
		oh.Close()
	}
	for _, ih := range d.incomingHandshakers {
		ih.Close()
	}
	for _, id := range d.infoDownloads {
		id.Close()
	}
	for _, pd := range d.pieceDownloads {
		pd.Close()
	}
	for _, ip := range d.incomingPeers {
		ip.Close()
	}
	for _, op := range d.outgoingPeers {
		op.Close()
	}
	// TODO close data
	// TODO order closes here
	close(d.closedC)
}

func (d *Downloader) run() {

	// TODO where to put this? this may take long
	if d.info != nil {
		err2 := d.processInfo()
		if err2 != nil {
			d.log.Errorln("cannot process info:", err2)
			d.errC <- err2
			return
		}
	}

	defer d.close()
	for {
		select {
		case <-d.closeC:
			return
		case cmd := <-d.startCommandC:
			d.start(cmd)
		case <-d.stopCommandC:
			d.stop(errors.New("torrent is stopped"))
		case addrs := <-d.addrsFromTrackers:
			d.addrList.Push(addrs, d.port)
			d.dialLimit.Signal(len(addrs))
		case req := <-d.statsCommandC:
			var stats Stats
			if d.info != nil {
				stats.BytesTotal = d.info.TotalLength
				// TODO this is wrong, pre-calculate complete and incomplete bytes
				stats.BytesComplete = int64(d.info.PieceLength) * int64(d.bitfield.Count())
				stats.BytesIncomplete = stats.BytesTotal - stats.BytesComplete
				// TODO calculate bytes downloaded
				// TODO calculate bytes uploaded
			} else {
				stats.BytesIncomplete = math.MaxUint32
				// TODO this is wrong, pre-calculate complete and incomplete bytes
			}
			req.Response <- stats
		case <-d.dialLimit.Ready:
			addr := d.addrList.Pop()
			if addr == nil {
				d.dialLimit.Stop()
				break
			}
			h := outgoinghandshaker.NewOutgoing(addr, d.peerID, d.infoHash, d.outgoingHandshakerResultC, d.log)
			d.outgoingHandshakers[addr.String()] = h
			go h.Run()
		case conn := <-d.newInConnC:
			if len(d.incomingHandshakers)+len(d.incomingPeers) >= maxPeerAccept {
				d.log.Debugln("peer limit reached, rejecting peer", conn.RemoteAddr().String())
				conn.Close()
				break
			}
			h := incominghandshaker.NewIncoming(conn, d.peerID, d.sKeyHash, d.infoHash, d.incomingHandshakerResultC, d.log)
			d.incomingHandshakers[conn.RemoteAddr().String()] = h
			go h.Run()
		case req := <-d.announcerRequests:
			tr := tracker.Transfer{
				InfoHash: d.infoHash,
				PeerID:   d.peerID,
				Port:     d.port,
			}
			if d.info == nil {
				tr.BytesLeft = math.MaxUint32
			} else {
				// TODO this is wrong, pre-calculate complete and incomplete bytes
				tr.BytesLeft = d.info.TotalLength - int64(d.info.PieceLength)*int64(d.bitfield.Count())
			}
			// TODO set bytes uploaded/downloaded
			req.Response <- announcer.Response{Transfer: tr}
		case <-d.infoDownloaders.Ready:
			if d.info != nil {
				d.infoDownloaders.Stop()
				break
			}
			id := d.nextInfoDownload()
			if id == nil {
				d.infoDownloaders.Stop()
				break
			}
			d.log.Debugln("downloading info from", id.Peer.String())
			d.infoDownloads[id.Peer] = id
			d.connectedPeers[id.Peer].InfoDownloader = id
			go id.Run()
		case res := <-d.infoDownloaderResultC:
			// TODO handle info downloader result
			d.connectedPeers[res.Peer].InfoDownloader = nil
			delete(d.infoDownloads, res.Peer)
			d.infoDownloaders.Signal(1)
			if res.Error != nil {
				res.Peer.Logger().Error(res.Error)
				res.Peer.Close()
				break
			}
			hash := sha1.New()    // nolint: gosec
			hash.Write(res.Bytes) // nolint: gosec
			if !bytes.Equal(hash.Sum(nil), d.infoHash[:]) {
				res.Peer.Logger().Errorln("received info does not match with hash")
				d.infoDownloaders.Signal(1)
				res.Peer.Close()
				break
			}
			var err error
			d.info, err = metainfo.NewInfo(res.Bytes)
			if err != nil {
				d.log.Errorln("cannot parse info bytes:", err)
				d.errC <- err
				return
			}
			if d.resume != nil {
				err = d.resume.WriteInfo(d.info.Bytes)
				if err != nil {
					d.log.Errorln("cannot write resume info:", err)
					d.errC <- err
					return
				}
			}
			err = d.processInfo()
			if err != nil {
				d.log.Errorln("cannot process info:", err)
				d.errC <- err
				return
			}
			// process previously received messages
			for _, pe := range d.connectedPeers {
				for _, msg := range pe.Messages {
					pm := peer.Message{Peer: pe, Message: msg}
					d.handlePeerMessage(pm)
				}
			}
			d.infoDownloaders.Stop()
			d.pieceDownloaders.Signal(parallelPieceDownloads)
		case <-d.pieceDownloaders.Ready:
			if d.info == nil {
				d.pieceDownloaders.Stop()
				break
			}
			// TODO check status of existing downloads
			pd := d.nextPieceDownload()
			if pd == nil {
				d.pieceDownloaders.Stop()
				break
			}
			d.log.Debugln("downloading piece", pd.Piece.Index, "from", pd.Peer.String())
			d.pieceDownloads[pd.Peer] = pd
			d.pieces[pd.Piece.Index].RequestedPeers[pd.Peer] = pd
			d.connectedPeers[pd.Peer].Downloader = pd
			go pd.Run()
		case res := <-d.pieceDownloaderResultC:
			d.log.Debugln("piece download completed. index:", res.Piece.Index)
			// TODO fix nil pointer exception
			if pe, ok := d.connectedPeers[res.Peer]; ok {
				pe.Downloader = nil
			}
			delete(d.pieceDownloads, res.Peer)
			delete(d.pieces[res.Piece.Index].RequestedPeers, res.Peer)
			d.pieceDownloaders.Signal(1)
			ok := d.pieces[res.Piece.Index].Piece.Verify(res.Bytes)
			if !ok {
				// TODO handle corrupt piece
				break
			}
			d.writeRequestC <- piecewriter.Request{Piece: res.Piece, Data: res.Bytes}
			d.pieces[res.Piece.Index].Writing = true
		case resp := <-d.writeResponseC:
			d.pieces[resp.Request.Piece.Index].Writing = false
			if resp.Error != nil {
				err := fmt.Errorf("cannot write piece data: %s", resp.Error)
				d.log.Errorln(err)
				d.stop(err)
				break
			}
			d.bitfield.Set(resp.Request.Piece.Index)
			if d.resume != nil {
				err := d.resume.WriteBitfield(d.bitfield.Bytes())
				if err != nil {
					err = fmt.Errorf("cannot write bitfield to resume db: %s", err)
					d.log.Errorln(err)
					d.stop(err)
					break
				}
			}
			d.checkCompletion()
			// Tell everyone that we have this piece
			// TODO skip peers already having that piece
			for _, pe := range d.connectedPeers {
				msg := peerprotocol.HaveMessage{Index: resp.Request.Piece.Index}
				pe.SendMessage(msg)
				d.updateInterestedState(pe)
			}
		case <-d.unchokeTimerC:
			peers := make([]*peer.Peer, 0, len(d.connectedPeers))
			for _, pe := range d.connectedPeers {
				if !pe.OptimisticUnhoked {
					peers = append(peers, pe)
				}
			}
			sort.Sort(peer.ByDownloadRate(peers))
			for _, pe := range d.connectedPeers {
				pe.BytesDownlaodedInChokePeriod = 0
			}
			unchokedPeers := make(map[*peer.Peer]struct{}, 3)
			for i, pe := range peers {
				if i == 3 {
					break
				}
				d.unchokePeer(pe)
				unchokedPeers[pe] = struct{}{}
			}
			for _, pe := range d.connectedPeers {
				if _, ok := unchokedPeers[pe]; !ok {
					d.chokePeer(pe)
				}
			}
		case <-d.optimisticUnchokeTimerC:
			peers := make([]*peer.Peer, 0, len(d.connectedPeers))
			for _, pe := range d.connectedPeers {
				if !pe.OptimisticUnhoked && pe.AmChoking {
					peers = append(peers, pe)
				}
			}
			if d.optimisticUnchokedPeer != nil {
				d.optimisticUnchokedPeer.OptimisticUnhoked = false
				d.chokePeer(d.optimisticUnchokedPeer)
			}
			if len(peers) == 0 {
				d.optimisticUnchokedPeer = nil
				break
			}
			pe := peers[rand.Intn(len(peers))]
			pe.OptimisticUnhoked = true
			d.unchokePeer(pe)
			d.optimisticUnchokedPeer = pe
		case res := <-d.incomingHandshakerResultC:
			delete(d.incomingHandshakers, res.Conn.RemoteAddr().String())
			if res.Error != nil {
				res.Conn.Close()
				break
			}
			d.startPeer(res.Peer, &d.incomingPeers)
		case res := <-d.outgoingHandshakerResultC:
			delete(d.outgoingHandshakers, res.Addr.String())
			if res.Error != nil {
				break
			}
			d.startPeer(res.Peer, &d.outgoingPeers)
		case pe := <-d.peerDisconnectedC:
			delete(d.peerIDs, pe.ID())
			if pe.Downloader != nil {
				pe.Downloader.Close()
			}
			delete(d.connectedPeers, pe.Peer)
			for i := range d.pieces {
				delete(d.pieces[i].HavingPeers, pe.Peer)
				delete(d.pieces[i].AllowedFastPeers, pe.Peer)
				delete(d.pieces[i].RequestedPeers, pe.Peer)
			}
		case pm := <-d.messages:
			d.handlePeerMessage(pm)
		}
	}
}

func (d *Downloader) handlePeerMessage(pm peer.Message) {
	pe := pm.Peer
	switch msg := pm.Message.(type) {
	case peerprotocol.HaveMessage:
		// Save have messages for processesing later received while we don't have info yet.
		if d.info == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		if msg.Index >= uint32(len(d.data.Pieces)) {
			pe.Peer.Logger().Errorln("unexpected piece index:", msg.Index)
			pe.Peer.Close()
			break
		}
		pi := &d.data.Pieces[msg.Index]
		pe.Peer.Logger().Debug("Peer ", pe.Peer.String(), " has piece #", pi.Index)
		d.pieceDownloaders.Signal(1)
		d.pieces[pi.Index].HavingPeers[pe.Peer] = struct{}{}
		d.updateInterestedState(pe)
	case peerprotocol.BitfieldMessage:
		// Save bitfield messages while we don't have info yet.
		if d.info == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		numBytes := uint32(bitfield.NumBytes(uint32(len(d.data.Pieces))))
		if uint32(len(msg.Data)) != numBytes {
			pe.Peer.Logger().Errorln("invalid bitfield length:", len(msg.Data))
			pe.Peer.Close()
			break
		}
		bf := bitfield.NewBytes(msg.Data, uint32(len(d.data.Pieces)))
		pe.Peer.Logger().Debugln("Received bitfield:", bf.Hex())
		for i := uint32(0); i < bf.Len(); i++ {
			if bf.Test(i) {
				d.pieces[i].HavingPeers[pe.Peer] = struct{}{}
			}
		}
		d.pieceDownloaders.Signal(int(bf.Count()))
		d.updateInterestedState(pe)
	case peerprotocol.HaveAllMessage:
		if d.info == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		for i := range d.pieces {
			d.pieces[i].HavingPeers[pe.Peer] = struct{}{}
		}
		d.pieceDownloaders.Signal(len(d.pieces))
		d.updateInterestedState(pe)
	case peerprotocol.HaveNoneMessage:
		// TODO handle?
	case peerprotocol.AllowedFastMessage:
		if d.info == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		if msg.Index >= uint32(len(d.data.Pieces)) {
			pe.Peer.Logger().Errorln("invalid allowed fast piece index:", msg.Index)
			pe.Peer.Close()
			break
		}
		pi := &d.data.Pieces[msg.Index]
		pe.Peer.Logger().Debug("Peer ", pe.Peer.String(), " has allowed fast for piece #", pi.Index)
		d.pieces[msg.Index].AllowedFastPeers[pe.Peer] = struct{}{}
	case peerprotocol.UnchokeMessage:
		d.pieceDownloaders.Signal(1)
		pe.PeerChoking = false
		if pd, ok := d.pieceDownloads[pe.Peer]; ok {
			pd.UnchokeC <- struct{}{}
		}
	case peerprotocol.ChokeMessage:
		pe.PeerChoking = true
		if pd, ok := d.pieceDownloads[pe.Peer]; ok {
			pd.ChokeC <- struct{}{}
		}
	case peerprotocol.InterestedMessage:
		// TODO handle intereseted messages
		_ = pe
	case peerprotocol.NotInterestedMessage:
		// TODO handle not intereseted messages
		_ = pe
	case peerprotocol.PieceMessage:
		if msg.Index >= uint32(len(d.data.Pieces)) {
			pe.Peer.Logger().Errorln("invalid piece index:", msg.Index)
			pe.Peer.Close()
			break
		}
		piece := &d.data.Pieces[msg.Index]
		block := piece.Blocks.Find(msg.Begin, msg.Length)
		if block == nil {
			pe.Peer.Logger().Errorln("invalid piece begin:", msg.Begin, "length:", msg.Length)
			pe.Peer.Close()
			break
		}
		pe.BytesDownlaodedInChokePeriod += int64(len(msg.Data))
		if pd, ok := d.pieceDownloads[pe.Peer]; ok {
			pd.PieceC <- piecedownloader.Piece{Block: block, Data: msg.Data}
		}
	case peerprotocol.RequestMessage:
		if d.info == nil {
			pe.Peer.Logger().Error("request received but we don't have info")
			pe.Peer.Close()
			break
		}
		if msg.Index >= uint32(len(d.data.Pieces)) {
			pe.Peer.Logger().Errorln("invalid request index:", msg.Index)
			pe.Peer.Close()
			break
		}
		if msg.Begin+msg.Length > d.data.Pieces[msg.Index].Length {
			pe.Peer.Logger().Errorln("invalid request length:", msg.Length)
			pe.Peer.Close()
			break
		}
		pi := &d.data.Pieces[msg.Index]
		if pe.AmChoking {
			if pe.Peer.FastExtension {
				m := peerprotocol.RejectMessage{RequestMessage: msg}
				pe.SendMessage(m)
			}
		} else {
			pe.Peer.SendPiece(msg, pi)
		}
	case peerprotocol.RejectMessage:
		if d.info == nil {
			pe.Peer.Logger().Error("reject received but we don't have info")
			pe.Peer.Close()
			break
		}

		if msg.Index >= uint32(len(d.data.Pieces)) {
			pe.Peer.Logger().Errorln("invalid reject index:", msg.Index)
			pe.Peer.Close()
			break
		}
		piece := &d.data.Pieces[msg.Index]
		block := piece.Blocks.Find(msg.Begin, msg.Length)
		if block == nil {
			pe.Peer.Logger().Errorln("invalid reject begin:", msg.Begin, "length:", msg.Length)
			pe.Peer.Close()
			break
		}
		pd, ok := d.pieceDownloads[pe.Peer]
		if !ok {
			pe.Peer.Logger().Error("reject received but we don't have active download")
			pe.Peer.Close()
			break
		}
		pd.RejectC <- block
	// TODO make it value type
	case *peerprotocol.ExtensionHandshakeMessage:
		d.log.Debugln("extension handshake received", msg)
		pe.ExtensionHandshake = msg
		d.infoDownloaders.Signal(1)
	// TODO make it value type
	case *peerprotocol.ExtensionMetadataMessage:
		switch msg.Type {
		case peerprotocol.ExtensionMetadataMessageTypeRequest:
			if d.info == nil {
				// TODO send reject
				break
			}
			extMsgID, ok := pe.ExtensionHandshake.M[peerprotocol.ExtensionMetadataKey]
			if !ok {
				// TODO send reject
			}
			// TODO Clients MAY implement flood protection by rejecting request messages after a certain number of them have been served. Typically the number of pieces of metadata times a factor.
			start := 16 * 1024 * msg.Piece
			end := 16 * 1024 * (msg.Piece + 1)
			totalSize := uint32(len(d.info.Bytes))
			if end > totalSize {
				end = totalSize
			}
			data := d.info.Bytes[start:end]
			dataMsg := peerprotocol.ExtensionMetadataMessage{
				Type:      peerprotocol.ExtensionMetadataMessageTypeData,
				Piece:     msg.Piece,
				TotalSize: totalSize,
				Data:      data,
			}
			extDataMsg := peerprotocol.ExtensionMessage{
				ExtendedMessageID: extMsgID,
				Payload:           &dataMsg,
			}
			pe.Peer.SendMessage(extDataMsg)
		case peerprotocol.ExtensionMetadataMessageTypeData:
			id, ok := d.infoDownloads[pe.Peer]
			if !ok {
				pe.Peer.Logger().Warningln("received unexpected metadata piece:", msg.Piece)
				break
			}
			select {
			case id.DataC <- infodownloader.Data{Index: msg.Piece, Data: msg.Data}:
			case <-d.closeC:
				return
			}
		case peerprotocol.ExtensionMetadataMessageTypeReject:
			// TODO handle metadata piece reject
		}
	default:
		panic(fmt.Sprintf("unhandled peer message type: %T", msg))
	}
}

func (d *Downloader) startPeer(p *ip.Peer, peers *[]*peer.Peer) {
	_, ok := d.peerIDs[p.ID()]
	if ok {
		p.Logger().Errorln("peer with same id already connected:", p.ID())
		p.Close()
		return
	}
	d.peerIDs[p.ID()] = struct{}{}

	pe := peer.New(p, d.messages, d.peerDisconnectedC)
	d.connectedPeers[p] = pe
	*peers = append(*peers, pe)
	go pe.Run()

	d.sendFirstMessage(p)
	if len(d.connectedPeers) <= 4 {
		d.unchokePeer(pe)
	}
}

func (d *Downloader) sendFirstMessage(p *ip.Peer) {
	bf := d.bitfield
	if p.FastExtension && bf != nil && bf.All() {
		msg := peerprotocol.HaveAllMessage{}
		p.SendMessage(msg)
	} else if p.FastExtension && (bf == nil || bf != nil && bf.Count() == 0) {
		msg := peerprotocol.HaveNoneMessage{}
		p.SendMessage(msg)
	} else if bf != nil {
		bitfieldData := make([]byte, len(bf.Bytes()))
		copy(bitfieldData, bf.Bytes())
		msg := peerprotocol.BitfieldMessage{Data: bitfieldData}
		p.SendMessage(msg)
	}
	extHandshakeMsg := peerprotocol.NewExtensionHandshake()
	if d.info != nil {
		extHandshakeMsg.MetadataSize = d.info.InfoSize
	}
	msg := peerprotocol.ExtensionMessage{
		ExtendedMessageID: peerprotocol.ExtensionHandshakeID,
		Payload:           extHandshakeMsg,
	}
	p.SendMessage(msg)
}

func (d *Downloader) processInfo() error {
	var err error
	d.data, err = torrentdata.New(d.info, d.storage)
	if err != nil {
		return err
	}
	// TODO defer data.Close()

	if d.bitfield == nil {
		d.bitfield = bitfield.New(d.info.NumPieces)
		if d.data.Exists {
			buf := make([]byte, d.info.PieceLength)
			hash := sha1.New() // nolint: gosec
			for _, p := range d.data.Pieces {
				_, err = p.Data.ReadAt(buf, 0)
				if err != nil {
					return err
				}
				ok := p.VerifyHash(buf[:p.Length], hash)
				d.bitfield.SetTo(p.Index, ok)
				hash.Reset()
			}
			if d.resume != nil {
				err = d.resume.WriteBitfield(d.bitfield.Bytes())
				if err != nil {
					return err
				}
			}
		}
	}
	d.checkCompletion()
	d.preparePieces()
	return nil
}

func (d *Downloader) preparePieces() {
	pieces := make([]piece.Piece, len(d.data.Pieces))
	sortedPieces := make([]*piece.Piece, len(d.data.Pieces))
	for i := range d.data.Pieces {
		pieces[i] = piece.New(&d.data.Pieces[i])
		sortedPieces[i] = &pieces[i]
	}
	d.pieces = pieces
	d.sortedPieces = sortedPieces
}

func (d *Downloader) nextInfoDownload() *infodownloader.InfoDownloader {
	for _, pe := range d.connectedPeers {
		if pe.InfoDownloader != nil {
			continue
		}
		extID, ok := pe.ExtensionHandshake.M[peerprotocol.ExtensionMetadataKey]
		if !ok {
			continue
		}
		return infodownloader.New(pe.Peer, extID, pe.ExtensionHandshake.MetadataSize, d.infoDownloaderResultC)
	}
	return nil
}

func (d *Downloader) nextPieceDownload() *piecedownloader.PieceDownloader {
	// TODO request first 4 pieces randomly
	sort.Sort(piece.ByAvailability(d.sortedPieces))
	for _, p := range d.sortedPieces {
		if d.bitfield.Test(p.Index) {
			continue
		}
		if len(p.RequestedPeers) > 0 {
			continue
		}
		if p.Writing {
			continue
		}
		if len(p.HavingPeers) == 0 {
			continue
		}
		// prefer allowed fast peers first
		for pe := range p.HavingPeers {
			if _, ok := p.AllowedFastPeers[pe]; !ok {
				continue
			}
			if _, ok := d.pieceDownloads[pe]; ok {
				continue
			}
			// TODO selecting first peer having the piece, change to more smart decision
			return piecedownloader.New(p.Piece, pe, d.pieceDownloaderResultC)
		}
		for pe := range p.HavingPeers {
			if pp, ok := d.connectedPeers[pe]; ok && pp.PeerChoking {
				continue
			}
			if _, ok := d.pieceDownloads[pe]; ok {
				continue
			}
			// TODO selecting first peer having the piece, change to more smart decision
			return piecedownloader.New(p.Piece, pe, d.pieceDownloaderResultC)
		}
	}
	return nil
}

func (d *Downloader) updateInterestedState(pe *peer.Peer) {
	if d.info == nil {
		return
	}
	interested := false
	for i := uint32(0); i < d.bitfield.Len(); i++ {
		weHave := d.bitfield.Test(i)
		_, peerHave := d.pieces[i].HavingPeers[pe.Peer]
		if !weHave && peerHave {
			interested = true
			break
		}
	}
	if !pe.AmInterested && interested {
		pe.AmInterested = true
		msg := peerprotocol.InterestedMessage{}
		pe.Peer.SendMessage(msg)
		return
	}
	if pe.AmInterested && !interested {
		pe.AmInterested = false
		msg := peerprotocol.NotInterestedMessage{}
		pe.Peer.SendMessage(msg)
		return
	}
}

func (d *Downloader) chokePeer(pe *peer.Peer) {
	if !pe.AmChoking {
		pe.AmChoking = true
		msg := peerprotocol.ChokeMessage{}
		pe.SendMessage(msg)
	}
}

func (d *Downloader) unchokePeer(pe *peer.Peer) {
	if pe.AmChoking {
		pe.AmChoking = false
		msg := peerprotocol.UnchokeMessage{}
		pe.SendMessage(msg)
	}
}

func (d *Downloader) checkCompletion() {
	if d.completed {
		return
	}
	if d.bitfield.All() {
		close(d.completeC)
		d.completed = true
	}
}

type Stats struct {
	// BytesDownloaded int64
	// BytesUploaded   int64
	BytesComplete   int64
	BytesIncomplete int64
	BytesTotal      int64
}

type StatsRequest struct {
	Response chan Stats
}

func (d *Downloader) Stats() Stats {
	var stats Stats
	req := StatsRequest{Response: make(chan Stats, 1)}
	select {
	case d.statsCommandC <- req:
	case <-d.closeC:
	}
	select {
	case stats = <-req.Response:
	case <-d.closeC:
	}
	return stats
}
