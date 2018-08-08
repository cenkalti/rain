package rain

import (
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"net/url"
	"sync"

	"github.com/cenkalti/rain/btconn"
	"github.com/cenkalti/rain/logger"
	"github.com/cenkalti/rain/magnet"
	"github.com/cenkalti/rain/torrent"
	"github.com/cenkalti/rain/tracker"
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

// Version of client. Set when building: "$ go build -ldflags "-X github.com/cenkalti/rain.Version 0001" cmd/rain/rain.go"
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
	log           logger.Logger
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

// NewClient returns a pointer to new Rain BitTorrent client.
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
		log:           logger.New("client"),
	}, nil
}

func generatePeerID() ([20]byte, error) {
	var id [20]byte
	copy(id[:], peerIDPrefix)
	_, err := rand.Read(id[len(peerIDPrefix):])
	return id, err
}

func (c *Client) PeerID() [20]byte { return c.peerID }

// Listen peer port and accept incoming peer connections.
func (c *Client) Listen() error {
	var err error
	addr := &net.TCPAddr{Port: int(c.config.Port)}
	c.listener, err = net.ListenTCP("tcp4", addr)
	if err != nil {
		return err
	}
	c.log.Notice("Listening peers on tcp://" + c.listener.Addr().String())
	go c.accepter()
	return nil
}

// Port returns the port number that the client is listening.
// If the client does not listen any port, returns 0.
func (c *Client) Port() int {
	if c.listener != nil {
		return c.listener.Addr().(*net.TCPAddr).Port
	}
	return 0
}

func (c *Client) Close() error { return c.listener.Close() }

func (c *Client) accepter() {
	limit := make(chan struct{}, maxPeerServe)
	for {
		conn, err := c.listener.Accept()
		if err != nil {
			c.log.Error(err)
			return
		}
		limit <- struct{}{}
		go func(c2 net.Conn) {
			defer func() { <-limit }()
			defer c2.Close()
			c.acceptAndRun(c2)
		}(conn)
	}
}

func (c *Client) acceptAndRun(conn net.Conn) {
	getSKey := func(sKeyHash [20]byte) (sKey []byte) {
		c.transfersM.Lock()
		t, ok := c.transfersSKey[sKeyHash]
		c.transfersM.Unlock()
		if ok {
			sKey = t.info.Hash[:]
		}
		return
	}

	hasInfoHash := func(ih [20]byte) bool {
		c.transfersM.Lock()
		_, ok := c.transfers[ih]
		c.transfersM.Unlock()
		return ok
	}

	log := logger.New("peer <- " + conn.RemoteAddr().String())
	encConn, cipher, extensions, ih, peerID, err := btconn.Accept(conn, getSKey, c.config.Encryption.ForceIncoming, hasInfoHash, [8]byte{}, c.peerID)
	if err != nil {
		if err == btconn.ErrOwnConnection {
			c.log.Debug(err)
		} else {
			c.log.Error(err)
		}
		return
	}
	log.Infof("Connection accepted. (cipher=%s extensions=%x client=%q)", cipher, extensions, peerID[:8])

	c.transfersM.Lock()
	t, ok := c.transfers[ih]
	c.transfersM.Unlock()
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
	t, err := torrent.New(r)
	if err != nil {
		return nil, err
	}
	c.log.Debugf("Parsed torrent file: %#v", t)
	return c.newTransferTorrent(t)
}

func (c *Client) AddMagnet(uri string) (*Transfer, error) {
	m, err := magnet.New(uri)
	if err != nil {
		return nil, err
	}
	return c.newTransferMagnet(m)
}

func (c *Client) newTracker(trackerURL string) (tracker.Tracker, error) {
	u, err := url.Parse(trackerURL)
	if err != nil {
		return nil, err
	}

	base := &tracker.TrackerBase{
		Url:    u,
		Rawurl: trackerURL,
		Client: c,
		Log:    logger.New("tracker " + trackerURL),
	}

	switch u.Scheme {
	case "http", "https":
		return tracker.NewHTTPTracker(base), nil
	case "udp":
		return tracker.NewUDPTracker(base), nil
	default:
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}
}
