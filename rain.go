package rain

import (
	"crypto/rand"
	"net"
	"runtime"
	"sync"

	"github.com/cenkalti/log"
	"github.com/cenkalti/mse"

	"github.com/cenkalti/rain/bt"
	"github.com/cenkalti/rain/internal/connection"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/torrent"
)

// Limits
const (
	// Request pieces in blocks of this size.
	blockSize = 16 * 1024
	// Do not accept more than maxPeerServe connections.
	maxPeerServe = 200
	// Maximum simultaneous uploads.
	uploadSlots = 4
	// Do not connect more than maxPeerPerTorrent peers.
	maxPeerPerTorrent = 200
)

// Client version. Set when building: "$ go build -ldflags "-X github.com/cenkalti/rain.Build 0001" cmd/rain/rain.go"
var Build = "0000" // zero means development version

// http://www.bittorrent.org/beps/bep_0020.html
var peerIDPrefix = []byte("-RN" + Build + "-")

func init() { log.Warning("You are running development version of rain!") }

func SetLogLevel(l log.Level) { logger.DefaultHandler.SetLevel(l) }

type Rain struct {
	config        *Config
	peerID        bt.PeerID
	listener      *net.TCPListener
	transfers     map[bt.InfoHash]*transfer // all active transfers
	transfersSKey map[[20]byte]*transfer    // for encryption
	transfersM    sync.Mutex
	log           logger.Logger
}

// New returns a pointer to new Rain BitTorrent client.
func New(c *Config) (*Rain, error) {
	peerID, err := generatePeerID()
	if err != nil {
		return nil, err
	}
	return &Rain{
		config:        c,
		peerID:        peerID,
		transfers:     make(map[bt.InfoHash]*transfer),
		transfersSKey: make(map[[20]byte]*transfer),
		log:           logger.New("rain"),
	}, nil
}

func generatePeerID() (bt.PeerID, error) {
	var id bt.PeerID
	copy(id[:], peerIDPrefix)
	_, err := rand.Read(id[len(peerIDPrefix):])
	return id, err
}

func (r *Rain) PeerID() bt.PeerID { return r.peerID }

// Listen peer port and accept incoming peer connections.
func (r *Rain) Listen() error {
	var err error
	addr := &net.TCPAddr{Port: int(r.config.Port)}
	r.listener, err = net.ListenTCP("tcp4", addr)
	if err != nil {
		return err
	}
	r.log.Notice("Listening peers on tcp://" + r.listener.Addr().String())
	go r.accepter()
	return nil
}

// Port returns the port number that the client is listening.
// If the client does not listen any port, returns 0.
func (r *Rain) Port() uint16 {
	if r.listener != nil {
		return uint16(r.listener.Addr().(*net.TCPAddr).Port)
	}
	return 0
}

func (r *Rain) Close() error { return r.listener.Close() }

func (r *Rain) accepter() {
	limit := make(chan struct{}, maxPeerServe)
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			r.log.Error(err)
			return
		}
		limit <- struct{}{}
		go func(c net.Conn) {
			defer func() {
				if err := recover(); err != nil {
					buf := make([]byte, 10000)
					r.log.Critical(err, "\n", string(buf[:runtime.Stack(buf, false)]))
				}
				c.Close()
				<-limit
			}()
			r.servePeer(c)
		}(conn)
	}
}

func (r *Rain) servePeer(conn net.Conn) {
	getSKey := func(sKeyHash [20]byte) (sKey []byte) {
		r.transfersM.Lock()
		t, ok := r.transfersSKey[sKeyHash]
		r.transfersM.Unlock()
		if ok {
			sKey = t.torrent.Info.Hash[:]
		}
		return
	}

	hasInfoHash := func(ih bt.InfoHash) bool {
		r.transfersM.Lock()
		_, ok := r.transfers[ih]
		r.transfersM.Unlock()
		return ok
	}

	log := logger.New("peer <- " + conn.RemoteAddr().String())
	encConn, cipher, extensions, ih, peerID, err := connection.Accept(conn, getSKey, r.config.Encryption.ForceIncoming, hasInfoHash, [8]byte{}, r.peerID)
	if err != nil {
		if err == connection.ErrOwnConnection {
			r.log.Debug(err)
		} else {
			r.log.Error(err)
		}
		return
	}
	log.Infof("Connection accepted. (cipher=%s extensions=%x)", cipher, extensions)

	r.transfersM.Lock()
	t, ok := r.transfers[ih]
	r.transfersM.Unlock()
	if !ok {
		log.Debug("Transfer is removed during incoming handshake")
		return
	}

	p := NewPeer(encConn, peerID, t, log)

	if err = p.SendBitField(); err != nil {
		log.Error(err)
		return
	}

	t.m.Lock()
	t.peers[peerID] = p
	t.m.Unlock()
	go func() {
		<-p.Disconnected
		t.m.Lock()
		delete(t.peers, peerID)
		t.m.Unlock()
	}()

	p.Run()
}

func (r *Rain) Add(torrentPath, where string) (*transfer, error) {
	torrent, err := torrent.New(torrentPath)
	if err != nil {
		return nil, err
	}
	r.log.Debugf("Parsed torrent file: %#v", torrent)
	if where == "" {
		where = r.config.DownloadDir
	}
	return r.newTransfer(torrent, where)
}

func (r *Rain) AddMagnet(url, where string) (*transfer, error) { panic("not implemented") }

func (r *Rain) Start(t *transfer) {
	sKey := mse.HashSKey(t.torrent.Info.Hash[:])
	r.transfersM.Lock()
	r.transfers[t.torrent.Info.Hash] = t
	r.transfersSKey[sKey] = t
	r.transfersM.Unlock()
	go func() {
		defer func() {
			if err := recover(); err != nil {
				buf := make([]byte, 10000)
				t.log.Critical(err, "\n", string(buf[:runtime.Stack(buf, false)]))
			}
		}()
		defer func() {
			r.transfersM.Lock()
			delete(r.transfers, t.torrent.Info.Hash)
			delete(r.transfersSKey, sKey)
			r.transfersM.Unlock()
		}()
		t.Run()
	}()
}

func (r *Rain) Stop(t *transfer) { close(t.stopC) }
