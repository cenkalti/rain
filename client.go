package rain

import (
	"crypto/rand"
	"io"
	"net"
	"sync"
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

// Client version. Set when building: "$ go build -ldflags "-X github.com/cenkalti/rain.Version 0001" cmd/rain/rain.go"
var Version = "0000" // zero means development version

// http://www.bittorrent.org/beps/bep_0020.html
var peerIDPrefix = []byte("-RN" + Version + "-")

type Client struct {
	config        *Config
	peerID        [20]byte
	listener      *net.TCPListener
	transfers     map[[20]byte]*Transfer // all active transfers
	transfersSKey map[[20]byte]*Transfer // for encryption
	transfersM    sync.Mutex
	log           logger
}

type Config struct {
	Port        int
	DownloadDir string `yaml:"download_dir"`
	Encryption  struct {
		DisableOutgoing bool `yaml:"disable_outgoing"`
		ForceOutgoing   bool `yaml:"force_outgoing"`
		ForceIncoming   bool `yaml:"force_incoming"`
	}
}

var DefaultConfig = Config{
	Port:        6881,
	DownloadDir: ".",
}

// New returns a pointer to new Rain BitTorrent client.
func NewClient(c *Config) (*Client, error) {
	if c == nil {
		c = new(Config)
		*c = DefaultConfig
	}
	peerID, err := generatePeerID()
	if err != nil {
		return nil, err
	}
	return &Client{
		config:        c,
		peerID:        peerID,
		transfers:     make(map[[20]byte]*Transfer),
		transfersSKey: make(map[[20]byte]*Transfer),
		log:           newLogger("client"),
	}, nil
}

func generatePeerID() ([20]byte, error) {
	var id [20]byte
	copy(id[:], peerIDPrefix)
	_, err := rand.Read(id[len(peerIDPrefix):])
	return id, err
}

func (r *Client) PeerID() [20]byte { return r.peerID }

// Listen peer port and accept incoming peer connections.
func (r *Client) Listen() error {
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
func (r *Client) Port() int {
	if r.listener != nil {
		return r.listener.Addr().(*net.TCPAddr).Port
	}
	return 0
}

func (r *Client) Close() error { return r.listener.Close() }

func (r *Client) accepter() {
	limit := make(chan struct{}, maxPeerServe)
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			r.log.Error(err)
			return
		}
		limit <- struct{}{}
		go func(c net.Conn) {
			defer func() { <-limit }()
			defer c.Close()
			r.acceptAndRun(c)
		}(conn)
	}
}

func (r *Client) acceptAndRun(conn net.Conn) {
	getSKey := func(sKeyHash [20]byte) (sKey []byte) {
		r.transfersM.Lock()
		t, ok := r.transfersSKey[sKeyHash]
		r.transfersM.Unlock()
		if ok {
			sKey = t.torrent.Info.Hash[:]
		}
		return
	}

	hasInfoHash := func(ih [20]byte) bool {
		r.transfersM.Lock()
		_, ok := r.transfers[ih]
		r.transfersM.Unlock()
		return ok
	}

	log := newLogger("peer <- " + conn.RemoteAddr().String())
	encConn, cipher, extensions, ih, peerID, err := accept(conn, getSKey, r.config.Encryption.ForceIncoming, hasInfoHash, [8]byte{}, r.peerID)
	if err != nil {
		if err == errOwnConnection {
			r.log.Debug(err)
		} else {
			r.log.Error(err)
		}
		return
	}
	log.Infof("Connection accepted. (cipher=%s extensions=%x client=%q)", cipher, extensions, peerID[:8])

	r.transfersM.Lock()
	t, ok := r.transfers[ih]
	r.transfersM.Unlock()
	if !ok {
		log.Debug("Transfer is removed during incoming handshake")
		return
	}

	p := t.newPeer(encConn, peerID, log)

	if err = p.SendBitfield(); err != nil {
		log.Error(err)
		return
	}

	t.m.Lock()
	t.peers[peerID] = p
	t.m.Unlock()
	defer func() {
		t.m.Lock()
		delete(t.peers, peerID)
		t.m.Unlock()
	}()

	p.Run()
}

func (c *Client) AddTorrent(r io.Reader) (*Transfer, error) {
	torrent, err := newTorrent(r)
	if err != nil {
		return nil, err
	}
	c.log.Debugf("Parsed torrent file: %#v", torrent)
	return c.newTransfer(torrent)
}

func (c *Client) AddMagnet(uri string) (*Transfer, error) { panic("not implemented") }
