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
	"github.com/cenkalti/rain/internal/downloader/piecedownloader"
	"github.com/cenkalti/rain/internal/downloader/piecewriter"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/mse"
	"github.com/cenkalti/rain/internal/peer"
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
)

var (
	// http://www.bittorrent.org/beps/bep_0020.html
	peerIDPrefix = []byte("-RN" + version.Version + "-")
)

type PeerMessage struct {
	*peer.Peer
	Message interface{}
}

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
	pieces []Piece

	// Contains pieces in sorted order for piece selection function.
	sortedPieces []*Piece

	// Peers are sent to this channel when they are disconnected.
	disconnectedPeers chan *peer.Peer

	// All messages coming from peers are sent to this channel.
	messages chan PeerMessage

	// We keep connected peers in this map after they complete handshake phase.
	connectedPeers map[*peer.Peer]*Peer

	// Active piece downloads are kept in this map.
	pieceDownloads map[*peer.Peer]*piecedownloader.PieceDownloader

	// Active metadata downloads are kept in this map.
	infoDownloads map[*peer.Peer]*infodownloader.InfoDownloader

	// Downloader run loop sends a message to this channel for writing a piece to disk.
	writeRequestC chan piecewriter.Request

	// When a piece is written to the disk, a message is sent to this channel.
	writeResponseC chan piecewriter.Response

	// A peer is optimistically unchoked regardless of their download rate.
	optimisticUnchokedPeer *Peer

	// This channel is closed once all pieces are downloaded and verified.
	completeC chan struct{}

	// If any unrecoverable error occurs, it will be sent to this channel and download will be stopped.
	errC chan error

	// When Stop() is called, it will close this channel to signal all running goroutines to stop.
	closeC chan struct{}

	// This channel will be closed after run loop exists.
	closedC chan struct{}

	// A message is sent to this channel to get download stats.
	statsC chan StatsRequest

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

	// Announces the status of torrent to trackers to get peer addresses.
	announcers []*announcer.Announcer

	incomingHandshakers map[string]*incominghandshaker.IncomingHandshaker
	outgoingHandshakers map[string]*outgoinghandshaker.OutgoingHandshaker

	incomingHandshakerResultC chan incominghandshaker.Result
	outgoingHandshakerResultC chan outgoinghandshaker.Result

	// We keep connected and handshake completed peers here.
	incomingPeers []*Peer
	outgoingPeers []*Peer

	// When metadata of the torrent downloaded completely, a message is sent to this channel.
	infoDownloaderResultC chan infodownloader.Result

	// When a piece is downloaded completely a message is sent to this channel.
	pieceDownloaderResultC chan piecedownloader.Result

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
		disconnectedPeers:         make(chan *peer.Peer),
		messages:                  make(chan PeerMessage),
		connectedPeers:            make(map[*peer.Peer]*Peer),
		pieceDownloads:            make(map[*peer.Peer]*piecedownloader.PieceDownloader),
		infoDownloads:             make(map[*peer.Peer]*infodownloader.InfoDownloader),
		writeRequestC:             make(chan piecewriter.Request),
		writeResponseC:            make(chan piecewriter.Response),
		completeC:                 make(chan struct{}),
		errC:                      make(chan error),
		log:                       l,
		closeC:                    make(chan struct{}),
		closedC:                   make(chan struct{}),
		statsC:                    make(chan StatsRequest),
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
	}
	copy(d.peerID[:], peerIDPrefix)
	_, err := rand.Read(d.peerID[len(peerIDPrefix):]) // nolint: gosec
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

func (d *Downloader) Close() {
	close(d.closeC)
	<-d.closedC
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

func (d *Downloader) run() {
	defer close(d.closedC)

	defer func() {
		if d.acceptor != nil {
			d.acceptor.Close()
		}
		for _, an := range d.announcers {
			an.Close()
		}
		for _, pw := range d.pieceWriters {
			pw.Close()
		}
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
	}()

	trackers, err := parseTrackers(d.trackers, d.log)
	if err != nil {
		d.log.Errorln("cannot parse trackers:", err)
		d.errC <- err
		return
	}
	if d.info != nil {
		err2 := d.processInfo()
		if err2 != nil {
			d.log.Errorln("cannot process info:", err2)
			d.errC <- err2
			return
		}
	}

	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{Port: d.port})
	if err != nil {
		d.log.Warningf("cannot listen port %d: %s", d.port, err)
	} else {
		d.log.Notice("Listening peers on tcp://" + listener.Addr().String())
		d.port = listener.Addr().(*net.TCPAddr).Port
		d.acceptor = acceptor.New(listener, d.newInConnC, d.log)
		go d.acceptor.Run()
	}

	announcerRequests := make(chan *announcer.Request)

	for _, tr := range trackers {
		an := announcer.New(tr, announcerRequests, d.completeC, d.addrsFromTrackers, d.log)
		d.announcers = append(d.announcers, an)
		go an.Run()
	}

	for i := 0; i < parallelPieceWrites; i++ {
		w := piecewriter.New(d.writeRequestC, d.writeResponseC, d.log)
		d.pieceWriters = append(d.pieceWriters, w)
		go w.Run()
	}

	unchokeTimer := time.NewTicker(10 * time.Second)
	defer unchokeTimer.Stop()

	optimisticUnchokeTimer := time.NewTicker(30 * time.Second)
	defer optimisticUnchokeTimer.Stop()

	pieceDownloaders := semaphore.New(parallelPieceDownloads)
	infoDownloaders := semaphore.New(parallelInfoDownloads)

	dialLimit := semaphore.New(40)

	for {
		select {
		case <-d.closeC:
			return
		case addrs := <-d.addrsFromTrackers:
			d.addrList.Push(addrs, d.port)
			dialLimit.Signal(uint32(len(addrs)))
		case req := <-d.statsC:
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
		case <-dialLimit.Wait:
			addr := d.addrList.Pop()
			if addr == nil {
				dialLimit.Block()
				break
			}
			h := outgoinghandshaker.NewOutgoing(addr, d.peerID, d.infoHash, d.outgoingHandshakerResultC, d.log)
			d.outgoingHandshakers[addr.String()] = h
			go h.Run()
		case conn := <-d.newInConnC:
			if len(d.incomingHandshakers)+len(d.incomingPeers) >= 40 {
				d.log.Debugln("peer limit reached, rejecting peer", conn.RemoteAddr().String())
				conn.Close()
				break
			}
			h := incominghandshaker.NewIncoming(conn, d.peerID, d.sKeyHash, d.infoHash, d.incomingHandshakerResultC, d.log)
			d.incomingHandshakers[conn.RemoteAddr().String()] = h
			go h.Run()
		case req := <-announcerRequests:
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
			select {
			case req.Response <- announcer.Response{Transfer: tr}:
			case <-d.closeC:
				return
			}
		case <-infoDownloaders.Wait:
			if d.info != nil {
				infoDownloaders.Block()
				break
			}
			id := d.nextInfoDownload()
			if id == nil {
				infoDownloaders.Block()
				break
			}
			d.log.Debugln("downloading info from", id.Peer.String())
			d.infoDownloads[id.Peer] = id
			d.connectedPeers[id.Peer].infoDownloader = id
			go id.Run()
		case res := <-d.infoDownloaderResultC:
			// TODO handle info downloader result
			d.connectedPeers[res.Peer].infoDownloader = nil
			delete(d.infoDownloads, res.Peer)
			infoDownloaders.Signal(1)
			if res.Error != nil {
				res.Peer.Logger().Error(err)
				res.Peer.Close()
				break
			}
			hash := sha1.New()    // nolint: gosec
			hash.Write(res.Bytes) // nolint: gosec
			if !bytes.Equal(hash.Sum(nil), d.infoHash[:]) {
				res.Peer.Logger().Errorln("received info does not match with hash")
				infoDownloaders.Signal(1)
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
				go d.resendMessages(pe, d.closeC)
			}
			infoDownloaders.Block()
			pieceDownloaders.Signal(parallelPieceDownloads)
		case <-pieceDownloaders.Wait:
			if d.info == nil {
				pieceDownloaders.Block()
				break
			}
			// TODO check status of existing downloads
			pd := d.nextDownload()
			if pd == nil {
				pieceDownloaders.Block()
				break
			}
			d.log.Debugln("downloading piece", pd.Piece.Index, "from", pd.Peer.String())
			d.pieceDownloads[pd.Peer] = pd
			d.pieces[pd.Piece.Index].requestedPeers[pd.Peer] = pd
			d.connectedPeers[pd.Peer].downloader = pd
			go pd.Run()
		case res := <-d.pieceDownloaderResultC:
			d.log.Debugln("piece download completed. index:", res.Piece.Index)
			// TODO fix nil pointer exception
			if pe, ok := d.connectedPeers[res.Peer]; ok {
				pe.downloader = nil
			}
			delete(d.pieceDownloads, res.Peer)
			delete(d.pieces[res.Piece.Index].requestedPeers, res.Peer)
			pieceDownloaders.Signal(1)
			ok := d.pieces[res.Piece.Index].Piece.Verify(res.Bytes)
			if !ok {
				// TODO handle corrupt piece
				break
			}
			select {
			case d.writeRequestC <- piecewriter.Request{Piece: res.Piece, Data: res.Bytes}:
				d.pieces[res.Piece.Index].writing = true
			case <-d.closeC:
				return
			}
		case resp := <-d.writeResponseC:
			d.pieces[resp.Request.Piece.Index].writing = false
			if resp.Error != nil {
				d.log.Errorln("cannot write piece data:", resp.Error)
				d.errC <- resp.Error
				return
			}
			d.bitfield.Set(resp.Request.Piece.Index)
			if d.resume != nil {
				err := d.resume.WriteBitfield(d.bitfield.Bytes())
				if err != nil {
					d.log.Errorln("cannot write bitfield to resume db:", err)
					d.errC <- err
					return
				}
			}
			d.checkCompletion()
			// Tell everyone that we have this piece
			// TODO skip peers already having that piece
			for _, pe := range d.connectedPeers {
				msg := peerprotocol.HaveMessage{Index: resp.Request.Piece.Index}
				pe.SendMessage(msg, d.closeC)
				d.updateInterestedState(pe, d.closeC)
			}
		case <-unchokeTimer.C:
			peers := make([]*Peer, 0, len(d.connectedPeers))
			for _, pe := range d.connectedPeers {
				if !pe.optimisticUnhoked {
					peers = append(peers, pe)
				}
			}
			sort.Sort(ByDownloadRate(peers))
			for _, pe := range d.connectedPeers {
				pe.bytesDownlaodedInChokePeriod = 0
			}
			unchokedPeers := make(map[*Peer]struct{}, 3)
			for i, pe := range peers {
				if i == 3 {
					break
				}
				d.unchokePeer(pe, d.closeC)
				unchokedPeers[pe] = struct{}{}
			}
			for _, pe := range d.connectedPeers {
				if _, ok := unchokedPeers[pe]; !ok {
					d.chokePeer(pe, d.closeC)
				}
			}
		case <-optimisticUnchokeTimer.C:
			peers := make([]*Peer, 0, len(d.connectedPeers))
			for _, pe := range d.connectedPeers {
				if !pe.optimisticUnhoked && pe.amChoking {
					peers = append(peers, pe)
				}
			}
			if d.optimisticUnchokedPeer != nil {
				d.optimisticUnchokedPeer.optimisticUnhoked = false
				d.chokePeer(d.optimisticUnchokedPeer, d.closeC)
			}
			if len(peers) == 0 {
				d.optimisticUnchokedPeer = nil
				break
			}
			pe := peers[rand.Intn(len(peers))]
			pe.optimisticUnhoked = true
			d.unchokePeer(pe, d.closeC)
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
		case p := <-d.disconnectedPeers:
			delete(d.peerIDs, p.ID())
			pe := d.connectedPeers[p]
			if pe.downloader != nil {
				pe.downloader.Close()
			}
			delete(d.connectedPeers, p)
			for i := range d.pieces {
				delete(d.pieces[i].havingPeers, p)
				delete(d.pieces[i].allowedFastPeers, p)
				delete(d.pieces[i].requestedPeers, p)
			}
		case pm := <-d.messages:
			pe := d.connectedPeers[pm.Peer]
			switch msg := pm.Message.(type) {
			case peerprotocol.HaveMessage:
				// Save have messages for processesing later received while we don't have info yet.
				if d.info == nil {
					pe.messages = append(pe.messages, msg)
					break
				}
				if msg.Index >= uint32(len(d.data.Pieces)) {
					pe.Peer.Logger().Errorln("unexpected piece index:", msg.Index)
					pe.Peer.Close()
					break
				}
				pi := &d.data.Pieces[msg.Index]
				pe.Peer.Logger().Debug("Peer ", pe.Peer.String(), " has piece #", pi.Index)
				pieceDownloaders.Signal(1)
				d.pieces[pi.Index].havingPeers[pe.Peer] = d.connectedPeers[pe.Peer]
				d.updateInterestedState(pe, d.closeC)
			case peerprotocol.BitfieldMessage:
				// Save bitfield messages while we don't have info yet.
				if d.info == nil {
					pe.messages = append(pe.messages, msg)
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
						d.pieces[i].havingPeers[pe.Peer] = d.connectedPeers[pe.Peer]
					}
				}
				pieceDownloaders.Signal(bf.Count())
				d.updateInterestedState(pe, d.closeC)
			case peerprotocol.HaveAllMessage:
				if d.info == nil {
					pe.messages = append(pe.messages, msg)
					break
				}
				for i := range d.pieces {
					d.pieces[i].havingPeers[pe.Peer] = pe
				}
				pieceDownloaders.Signal(uint32(len(d.pieces)))
				d.updateInterestedState(pe, d.closeC)
			case peerprotocol.HaveNoneMessage:
				// TODO handle?
			case peerprotocol.AllowedFastMessage:
				if d.info == nil {
					pe.messages = append(pe.messages, msg)
					break
				}
				if msg.Index >= uint32(len(d.data.Pieces)) {
					pe.Peer.Logger().Errorln("invalid allowed fast piece index:", msg.Index)
					pe.Peer.Close()
					break
				}
				pi := &d.data.Pieces[msg.Index]
				pe.Peer.Logger().Debug("Peer ", pe.Peer.String(), " has allowed fast for piece #", pi.Index)
				d.pieces[msg.Index].allowedFastPeers[pe.Peer] = d.connectedPeers[pe.Peer]
			case peerprotocol.UnchokeMessage:
				pieceDownloaders.Signal(1)
				pe.peerChoking = false
				if pd, ok := d.pieceDownloads[pe.Peer]; ok {
					pd.UnchokeC <- struct{}{}
				}
			case peerprotocol.ChokeMessage:
				pe.peerChoking = true
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
				pe.bytesDownlaodedInChokePeriod += int64(len(msg.Data))
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
				if pe.amChoking {
					if pe.Peer.FastExtension {
						m := peerprotocol.RejectMessage{RequestMessage: msg}
						pe.SendMessage(m, d.closeC)
					}
				} else {
					pe.Peer.SendPiece(msg, pi, d.closeC)
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
				pe.extensionHandshake = msg
				infoDownloaders.Signal(1)
			// TODO make it value type
			case *peerprotocol.ExtensionMetadataMessage:
				switch msg.Type {
				case peerprotocol.ExtensionMetadataMessageTypeRequest:
					if d.info == nil {
						// TODO send reject
						break
					}
					extMsgID, ok := pe.extensionHandshake.M[peerprotocol.ExtensionMetadataKey]
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
					pe.Peer.SendMessage(extDataMsg, d.closeC)
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
	}
}

func (d *Downloader) startPeer(p *peer.Peer, peers *[]*Peer) {
	_, ok := d.peerIDs[p.ID()]
	if ok {
		p.Logger().Errorln("peer with same id already connected:", p.ID())
		p.Close()
		return
	}
	d.peerIDs[p.ID()] = struct{}{}

	pe := NewPeer(p)
	*peers = append(*peers, pe)
	go pe.Run()

	d.connectedPeers[p] = pe
	bf := d.bitfield
	if p.FastExtension && bf != nil && bf.All() {
		msg := peerprotocol.HaveAllMessage{}
		p.SendMessage(msg, d.closeC)
	} else if p.FastExtension && (bf == nil || bf != nil && bf.Count() == 0) {
		msg := peerprotocol.HaveNoneMessage{}
		p.SendMessage(msg, d.closeC)
	} else if bf != nil {
		bitfieldData := make([]byte, len(bf.Bytes()))
		copy(bitfieldData, bf.Bytes())
		msg := peerprotocol.BitfieldMessage{Data: bitfieldData}
		p.SendMessage(msg, d.closeC)
	}
	extHandshakeMsg := peerprotocol.NewExtensionHandshake()
	if d.info != nil {
		extHandshakeMsg.MetadataSize = d.info.InfoSize
	}
	msg := peerprotocol.ExtensionMessage{
		ExtendedMessageID: peerprotocol.ExtensionHandshakeID,
		Payload:           extHandshakeMsg,
	}
	p.SendMessage(msg, d.closeC)
	if len(d.connectedPeers) <= 4 {
		d.unchokePeer(pe, d.closeC)
	}
	go d.readMessages(pe.Peer, d.closeC)
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
	pieces := make([]Piece, len(d.data.Pieces))
	sortedPieces := make([]*Piece, len(d.data.Pieces))
	for i := range d.data.Pieces {
		pieces[i] = Piece{
			Piece:            &d.data.Pieces[i],
			havingPeers:      make(map[*peer.Peer]*Peer),
			allowedFastPeers: make(map[*peer.Peer]*Peer),
			requestedPeers:   make(map[*peer.Peer]*piecedownloader.PieceDownloader),
		}
		sortedPieces[i] = &pieces[i]
	}
	d.pieces = pieces
	d.sortedPieces = sortedPieces
}

func (d *Downloader) nextInfoDownload() *infodownloader.InfoDownloader {
	for _, pe := range d.connectedPeers {
		if pe.infoDownloader != nil {
			continue
		}
		extID, ok := pe.extensionHandshake.M[peerprotocol.ExtensionMetadataKey]
		if !ok {
			continue
		}
		return infodownloader.New(pe.Peer, extID, pe.extensionHandshake.MetadataSize, d.infoDownloaderResultC)
	}
	return nil
}

func (d *Downloader) nextDownload() *piecedownloader.PieceDownloader {
	// TODO request first 4 pieces randomly
	sort.Sort(ByAvailability(d.sortedPieces))
	for _, p := range d.sortedPieces {
		if d.bitfield.Test(p.Index) {
			continue
		}
		if len(p.requestedPeers) > 0 {
			continue
		}
		if p.writing {
			continue
		}
		if len(p.havingPeers) == 0 {
			continue
		}
		// prefer allowed fast peers first
		for _, pe := range p.havingPeers {
			if _, ok := p.allowedFastPeers[pe.Peer]; !ok {
				continue
			}
			if _, ok := d.pieceDownloads[pe.Peer]; ok {
				continue
			}
			// TODO selecting first peer having the piece, change to more smart decision
			return piecedownloader.New(p.Piece, pe.Peer, d.pieceDownloaderResultC)
		}
		for _, pe := range p.havingPeers {
			if pe.peerChoking {
				continue
			}
			if _, ok := d.pieceDownloads[pe.Peer]; ok {
				continue
			}
			// TODO selecting first peer having the piece, change to more smart decision
			return piecedownloader.New(p.Piece, pe.Peer, d.pieceDownloaderResultC)
		}
	}
	return nil
}

func (d *Downloader) updateInterestedState(pe *Peer, stopC chan struct{}) {
	if d.info == nil {
		return
	}
	interested := false
	for i := uint32(0); i < d.bitfield.Len(); i++ {
		weHave := d.bitfield.Test(i)
		_, peerHave := d.pieces[i].havingPeers[pe.Peer]
		if !weHave && peerHave {
			interested = true
			break
		}
	}
	if !pe.amInterested && interested {
		pe.amInterested = true
		msg := peerprotocol.InterestedMessage{}
		pe.Peer.SendMessage(msg, stopC)
		return
	}
	if pe.amInterested && !interested {
		pe.amInterested = false
		msg := peerprotocol.NotInterestedMessage{}
		pe.Peer.SendMessage(msg, stopC)
		return
	}
}

func (d *Downloader) chokePeer(pe *Peer, stopC chan struct{}) {
	if !pe.amChoking {
		pe.amChoking = true
		msg := peerprotocol.ChokeMessage{}
		pe.SendMessage(msg, stopC)
	}
}

func (d *Downloader) unchokePeer(pe *Peer, stopC chan struct{}) {
	if pe.amChoking {
		pe.amChoking = false
		msg := peerprotocol.UnchokeMessage{}
		pe.SendMessage(msg, stopC)
	}
}

func (d *Downloader) readMessages(pe *peer.Peer, stopC chan struct{}) {
	for msg := range pe.Messages() {
		select {
		case d.messages <- PeerMessage{Peer: pe, Message: msg}:
		case <-stopC:
			return
		}
	}
	select {
	case d.disconnectedPeers <- pe:
	case <-stopC:
		return
	}
}

func (d *Downloader) resendMessages(pe *Peer, stopC chan struct{}) {
	for _, msg := range pe.messages {
		select {
		case d.messages <- PeerMessage{Peer: pe.Peer, Message: msg}:
		case <-stopC:
			return
		}
	}
}

func (d *Downloader) checkCompletion() {
	if d.completed {
		return
	}
	if d.bitfield.All() {
		close(d.completeC)
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
	case d.statsC <- req:
	case <-d.closeC:
	}
	select {
	case stats = <-req.Response:
	case <-d.closeC:
	}
	return stats
}
