package rain

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"io/ioutil"
	"net"
	"time"

	"github.com/cenkalti/log"
)

// http://www.bittorrent.org/beps/bep_0020.html
var peerIDPrefix = []byte("-RN0001-")

type Rain struct {
	peerID   [20]byte
	listener net.Listener
}

// New returns a pointer to new Rain BitTorrent client.
// Call ListenPeerPort method before starting Download to accept incoming connections.
func New() (*Rain, error) {
	r := new(Rain)
	return r, r.generatePeerID()
}

func (r *Rain) generatePeerID() error {
	buf := make([]byte, len(r.peerID)-len(peerIDPrefix))
	_, err := rand.Read(buf)
	if err != nil {
		return err
	}
	copy(r.peerID[:], peerIDPrefix)
	copy(r.peerID[len(peerIDPrefix):], buf)
	return nil
}

// ListenPeerPort starts to listen a TCP port to accept incoming peer connections.
func (r *Rain) ListenPeerPort(port int) error {
	var err error
	addr := &net.TCPAddr{Port: port}
	r.listener, err = net.ListenTCP("tcp4", addr)
	if err != nil {
		return err
	}
	log.Notice("Listening peers on tcp://" + r.listener.Addr().String())
	go r.acceptor()
	return nil
}

func (r *Rain) acceptor() {
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			log.Error(err)
			return
		}
		go r.servePeerConn(conn)
	}
}

const bitTorrent10pstrLen = 19

var bitTorrent10pstr = []byte("BitTorrent protocol")

func (r *Rain) servePeerConn(conn net.Conn) {
	defer conn.Close()

	// Give a minute for completing handshake.
	err := conn.SetDeadline(time.Now().Add(time.Minute))
	if err != nil {
		return
	}

	// Send handshake as soon as you see info_hash.
	var peerID [20]byte
	infoHashC := make(chan [20]byte, 1)
	errC := make(chan error, 1)
	go func() {
		var err error
		peerID, err = r.readHandShake(conn, infoHashC)
		if err != nil {
			errC <- err
		}
		close(errC)
	}()

	select {
	case infoHash := <-infoHashC:
		// TODO check if we have a torrent with info_hash
		err = r.sendHandShake(conn, infoHash)
		if err != nil {
			return
		}
	case <-errC:
		return
	}

	err = <-errC
	if err != nil {
		return
	}

	// TODO save peer with peerID
	r.communicateWithPeer(conn)
}

func (r *Rain) readHandShake(conn net.Conn, notifyInfoHash chan [20]byte) (peerID [20]byte, err error) {
	buf := make([]byte, bitTorrent10pstrLen)
	_, err = conn.Read(buf[:1]) // pstrlen
	if err != nil {
		return [20]byte{}, err
	}
	pstrlen := buf[0]
	if pstrlen != bitTorrent10pstrLen {
		return [20]byte{}, errors.New("unexpected pstrlen")
	}

	_, err = io.ReadFull(conn, buf) // pstr
	if err != nil {
		return [20]byte{}, err
	}
	if bytes.Compare(buf, bitTorrent10pstr) != 0 {
		return [20]byte{}, errors.New("unexpected pstr")
	}

	_, err = io.CopyN(ioutil.Discard, conn, 8) // reserved
	if err != nil {
		return [20]byte{}, err
	}

	var infoHash [20]byte
	_, err = io.ReadFull(conn, infoHash[:]) // info_hash
	if err != nil {
		return [20]byte{}, err
	}

	// The recipient must respond as soon as it sees the info_hash part of the handshake
	// (the peer id will presumably be sent after the recipient sends its own handshake).
	// The tracker's NAT-checking feature does not send the peer_id field of the handshake.
	if notifyInfoHash != nil {
		notifyInfoHash <- infoHash
	}

	_, err = io.ReadFull(conn, peerID[:]) // peer_id
	return peerID, err
}

func (r *Rain) sendHandShake(conn net.Conn, infoHash [20]byte) error {
	var handShake = struct {
		Pstrlen  byte
		Pstr     [bitTorrent10pstrLen]byte
		Reserved [8]byte
		InfoHash [20]byte
		PeerID   [20]byte
	}{
		Pstrlen:  bitTorrent10pstrLen,
		InfoHash: infoHash,
		PeerID:   r.peerID,
	}
	copy(handShake.Pstr[:], bitTorrent10pstr)
	return binary.Write(conn, binary.BigEndian, &handShake)
}

// Download starts a download and waits for it to finish.
func (r *Rain) Download(filePath, where string) error {
	torrent, err := NewTorrentFile(filePath)
	if err != nil {
		return err
	}
	log.Debugf("Parsed torrent file: %#v", torrent)

	download := NewDownload(torrent)

	err = download.allocate(where)
	if err != nil {
		return err
	}

	tracker, err := NewTracker(torrent.Announce, r.peerID)
	if err != nil {
		return err
	}

	err = tracker.Dial()
	if err != nil {
		return err
	}

	responseC := make(chan *AnnounceResponse)
	go tracker.announce(download, nil, nil, responseC)

	for {
		select {
		case resp := <-responseC:
			log.Debug("Announce response: %#v", resp)
			for _, p := range resp.Peers {
				log.Debug("Peer:", p.TCPAddr())
				go r.connectToPeer(p, download)
			}
			// case
		}
	}

	return nil
}

func (r *Rain) connectToPeer(p *Peer, d *download) {
	conn, err := net.DialTCP("tcp4", nil, p.TCPAddr())
	if err != nil {
		log.Error(err)
		return
	}

	log.Info("Connected to peer", conn.RemoteAddr())

	err = r.sendHandShake(conn, d.TorrentFile.InfoHash)
	if err != nil {
		return
	}

	_, err = r.readHandShake(conn, nil)
	if err != nil {
		return
	}

	log.Debug("handshake completed")

	r.communicateWithPeer(conn)
}

// communicateWithPeer is the common method that is called after handshake.
// Peer connections are symmetrical.
func (r *Rain) communicateWithPeer(conn net.Conn) {
	// TODO adjust deadline to heartbeat
	err := conn.SetDeadline(time.Time{})
	if err != nil {
		return
	}
}
