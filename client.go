package rain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"sync"

	"github.com/cenkalti/rain/bitfield"
	"github.com/cenkalti/rain/btconn"
	"github.com/cenkalti/rain/logger"
	"github.com/cenkalti/rain/magnet"
	"github.com/cenkalti/rain/metainfo"
	"github.com/cenkalti/rain/tracker"
	"github.com/cenkalti/rain/tracker/httptracker"
	"github.com/cenkalti/rain/tracker/udptracker"
	"github.com/hashicorp/go-multierror"
)

// Limits
const (
	// Request pieces in blocks of this size.
	blockSize = 16 * 1024
	// TODO move these to config
	// Do not accept more than maxPeerServe connections.
	maxPeerServe = 200
	// Maximum simultaneous uploads.
	uploadSlotsPerTorrent = 4
	// Do not connect more than maxPeerPerTorrent peers.
	maxPeerPerTorrent = 200
)

// Version of client. Set when building: "$ go build -ldflags "-X github.com/cenkalti/rain.Version 0001" cmd/rain/rain.go"
var Version = "0000" // zero means development version

// http://www.bittorrent.org/beps/bep_0020.html
var peerIDPrefix = []byte("-RN" + Version + "-")

type Client struct {
	config       *Config
	peerID       [20]byte
	listener     *net.TCPListener
	torrents     map[[20]byte]*Torrent // all active transfers
	torrentsSKey map[[20]byte]*Torrent // for encryption
	m            sync.Mutex
	log          logger.Logger
}

// NewClient returns a pointer to new Rain BitTorrent client.
func NewClient(c *Config) (*Client, error) {
	if c == nil {
		c = NewConfig()
	}
	var peerID [20]byte
	copy(peerID[:], peerIDPrefix)
	_, err := rand.Read(peerID[len(peerIDPrefix):]) // nolint: gosec
	if err != nil {
		return nil, err
	}
	return &Client{
		config:       c,
		peerID:       peerID,
		torrents:     make(map[[20]byte]*Torrent),
		torrentsSKey: make(map[[20]byte]*Torrent),
		log:          logger.New("client"),
	}, nil
}

func (c *Client) PeerID() [20]byte { return c.peerID }

// Start listening peer port and accepting incoming peer connections.
func (c *Client) Start() error {
	var err error
	addr := &net.TCPAddr{Port: c.config.Port}
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

func (c *Client) Close() error {
	var result error
	err := c.listener.Close()
	if err != nil {
		result = multierror.Append(result, err)
	}
	c.m.Lock()
	for _, t := range c.torrents {
		err = t.Close()
		if err != nil {
			result = multierror.Append(result, err)
		}
	}
	return result
}

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
			defer c2.Close() // nolint: errcheck
			c.acceptAndRun(c2)
		}(conn)
	}
}

func (c *Client) acceptAndRun(conn net.Conn) {
	getSKey := func(sKeyHash [20]byte) (sKey []byte) {
		c.m.Lock()
		t, ok := c.torrentsSKey[sKeyHash]
		c.m.Unlock()
		if ok {
			sKey = t.info.Hash[:]
		}
		return
	}

	hasInfoHash := func(ih [20]byte) bool {
		c.m.Lock()
		_, ok := c.torrents[ih]
		c.m.Unlock()
		return ok
	}

	log := logger.New("peer <- " + conn.RemoteAddr().String())
	encConn, cipher, extensions, peerID, infoHash, err := btconn.Accept(conn, getSKey, c.config.Encryption.ForceIncoming, hasInfoHash, [8]byte{}, c.peerID)
	if err != nil {
		if err == btconn.ErrOwnConnection {
			c.log.Debug(err)
		} else {
			c.log.Error(err)
		}
		return
	}
	log.Infof("Connection accepted. (cipher=%s extensions=%x client=%q)", cipher, extensions, peerID[:8])

	c.m.Lock()
	t, ok := c.torrents[infoHash]
	c.m.Unlock()
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

func (c *Client) AddTorrent(r io.Reader, dest string) (*Torrent, error) {
	t, err := torrent.New(r)
	if err != nil {
		return nil, err
	}
	return c.newTorrentFromMetaInfo(t, dest)
}

func (c *Client) AddMagnet(uri, dest string) (*Torrent, error) {
	m, err := magnet.New(uri)
	if err != nil {
		return nil, err
	}
	return c.newTorrentFromMagnet(m, dest)
}

func (c *Client) newTracker(trackerURL string) (tracker.Tracker, error) {
	u, err := url.Parse(trackerURL)
	if err != nil {
		return nil, err
	}
	l := logger.New("tracker " + trackerURL)
	switch u.Scheme {
	case "http", "https":
		return httptracker.New(u, c, l), nil
	case "udp":
		return udptracker.New(u, c, l), nil
	default:
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}
}

func (c *Client) newTorrent(hash [20]byte, trackerString string, name, dest string) (*Torrent, error) {
	trk, err := c.newTracker(trackerString)
	if err != nil {
		return nil, err
	}
	if len(name) > 8 {
		name = name[:8]
	}
	return &Torrent{
		client:    c,
		dest:      dest,
		hash:      hash,
		tracker:   trk,
		announceC: make(chan *tracker.AnnounceResponse),
		peers:     make(map[[20]byte]*peer),
		stopC:     make(chan struct{}),
		peersC:    make(chan []*net.TCPAddr),
		peerC:     make(chan *net.TCPAddr),
		completed: make(chan struct{}),
		requestC:  make(chan *peerRequest),
		serveC:    make(chan *peerRequest),
		log:       logger.New("download " + name),
	}, nil
}

func (c *Client) newTorrentFromMetaInfo(tor *torrent.MetaInfo, dest string) (*Torrent, error) {
	t, err := c.newTorrent(tor.Info.Hash, tor.Announce, tor.Info.Name, dest)
	if err != nil {
		return nil, err
	}

	t.info = tor.Info

	files, checkHash, err := prepareFiles(tor.Info, dest)
	if err != nil {
		return nil, err
	}
	t.pieces = newPieces(tor.Info, files)
	t.bitfield = bitfield.New(tor.Info.NumPieces)
	var percentDone uint32
	if checkHash {
		c.log.Notice("Doing hash check...")
		for _, p := range t.pieces {
			if err := p.Verify(); err != nil {
				return nil, err
			}
			t.bitfield.SetTo(p.Index, p.OK)
		}
		percentDone = t.bitfield.Count() * 100 / t.bitfield.Len()
		c.log.Noticef("Already downloaded: %d%%", percentDone)
	}
	if percentDone == 100 {
		t.onceCompleted.Do(func() {
			close(t.completed)
			t.log.Notice("Download completed")
		})
	}
	return t, nil
}

func (c *Client) newTorrentFromMagnet(m *magnet.Magnet, dest string) (*Torrent, error) {
	if len(m.Trackers) == 0 {
		return nil, errors.New("no tracker in magnet link")
	}
	var name string
	if m.Name != "" {
		name = m.Name
	} else {
		name = hex.EncodeToString(m.InfoHash[:])
	}
	return c.newTorrent(m.InfoHash, m.Trackers[0], name, dest)
}
