package rain

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"time"
)

const DefaultPeerPort = 6881

// http://www.bittorrent.org/beps/bep_0020.html
var PeerIDPrefix = []byte("-RN0001-")

type Rain struct {
	peerID   [20]byte
	listener net.Listener
	// Port to listen for peer connections. Set to 0 for random port.
	Port int
	// downloads map[[20]byte]*Download
	// trackers  map[string]*Tracker
}

func New() (*Rain, error) {
	r := &Rain{
		Port: DefaultPeerPort,
		// downloads: make(map[[20]byte]*Download),
		// trackers:  make(map[string]*Tracker),
	}
	if err := r.generatePeerID(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Rain) generatePeerID() error {
	buf := make([]byte, len(r.peerID)-len(PeerIDPrefix))
	_, err := rand.Read(buf)
	if err != nil {
		return err
	}
	copy(r.peerID[:], PeerIDPrefix)
	copy(r.peerID[len(PeerIDPrefix):], buf)
	return nil
}

func (r *Rain) ListenPeerPort() error {
	var err error
	addr := &net.TCPAddr{Port: r.Port}
	r.listener, err = net.ListenTCP("tcp4", addr)
	if err != nil {
		return err
	}
	log.Println("Listening peers on tcp://" + r.listener.Addr().String())
	// Update port number if it's been choosen randomly.
	r.Port = r.listener.Addr().(*net.TCPAddr).Port
	go r.acceptor()
	return nil
}

func (r *Rain) acceptor() {
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			log.Println(err)
			return
		}
		go r.servePeerConn(conn)
	}
}

const bitTorrent10pstrLen = 19

var bitTorrent10pstr = []byte("BitTorrent protocol")

func (r *Rain) servePeerConn(conn net.Conn) {
	defer conn.Close()

	err := conn.SetDeadline(time.Now().Add(time.Minute))
	if err != nil {
		return
	}

	notifyInfoHash := make(chan struct{})
	err = r.readHandShake(conn, notifyInfoHash)
	if err != nil {
		log.Println(err)
		return
	}
}

func (r *Rain) readHandShake(conn net.Conn, notifyInfoHash chan struct{}) error {
	buf := make([]byte, 20)
	_, err := conn.Read(buf[:1]) // pstrlen
	if err != nil {
		return err
	}
	pstrlen := buf[0]
	if pstrlen != bitTorrent10pstrLen {
		return errors.New("unexpected pstrlen")
	}

	pstr := buf[:bitTorrent10pstrLen]
	_, err = io.ReadFull(conn, pstr) // pstr
	if err != nil {
		return err
	}
	if bytes.Compare(pstr, bitTorrent10pstr) != 0 {
		return errors.New("unexpected pstr")
	}

	_, err = io.CopyN(ioutil.Discard, conn, 8) // reserved
	if err != nil {
		return err
	}

	var infoHash [20]byte
	_, err = io.ReadFull(conn, infoHash[:]) // info_hash
	if err != nil {
		return err
	}

	// TODO check if we have a torrent with info_hash

	// The recipient must respond as soon as it sees the info_hash part of the handshake
	// (the peer id will presumably be sent after the recipient sends its own handshake).
	// The tracker's NAT-checking feature does not send the peer_id field of the handshake.
	if notifyInfoHash != nil {
		close(notifyInfoHash)
	}

	_, err = io.ReadFull(conn, buf) // peer_id
	return err
}

func (r *Rain) sendHandShake(conn net.Conn, infoHash [20]byte) {
	var handShake = struct {
		Pstrlen  byte
		Pstr     [bitTorrent10pstrLen]byte
		Reserved [8]byte
		InfoHash [20]byte
		PeerID   [20]byte
	}{
		Pstrlen:  19,
		InfoHash: infoHash,
		PeerID:   r.peerID,
	}
	copy(handShake.Pstr[:], bitTorrent10pstr)
	binary.Write(conn, binary.BigEndian, &handShake)
}

// Download starts a download and waits for it to finish.
func (r *Rain) Download(filePath, where string) error {
	torrent, err := LoadTorrentFile(filePath)
	if err != nil {
		return err
	}
	fmt.Printf("--- torrent: %#v\n", torrent)

	download := NewDownload(torrent)

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
			fmt.Printf("--- announce response: %#v\n", resp)
			for _, p := range resp.Peers {
				fmt.Printf("--- p: %s\n", p.TCPAddr())
				go r.connectToPeer(p)
			}
			// case
		}
	}

	return nil
}

func (r *Rain) connectToPeer(p *Peer) {
	conn, err := net.DialTCP("tcp4", nil, p.TCPAddr())
	if err != nil {
		log.Println(err)
		return
	}

	log.Printf("Connected to peer %s", conn.RemoteAddr().String())

	// r.sendHandShake(conn, infoHash)
	// binary.Write(conn, binary.BigEndian, uint8(19))
	// binary
}
