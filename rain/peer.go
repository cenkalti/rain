package rain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"io/ioutil"
	"net"
	"time"

	"github.com/cenkalti/log"
)

// All current implementations use 2^14 (16 kiB), and close connections which request an amount greater than that.
const blockSize = 16 * 1024

// http://www.bittorrent.org/beps/bep_0020.html
var peerIDPrefix = []byte("-RN0001-")

type peerID [20]byte

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
	var peerID *peerID
	infoHashC := make(chan *infoHash, 1)
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
		// Do not continue if we don't have a torrent with this infoHash.
		r.downloadsM.Lock()
		if _, ok := r.downloads[infoHash]; !ok {
			log.Error("unexpected info_hash")
			r.downloadsM.Unlock()
			return
		}
		r.downloadsM.Unlock()

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

func (r *Rain) readHandShake(conn net.Conn, notifyInfoHash chan *infoHash) (*peerID, error) {
	buf := make([]byte, bitTorrent10pstrLen)
	_, err := conn.Read(buf[:1]) // pstrlen
	if err != nil {
		return nil, err
	}
	pstrlen := buf[0]
	if pstrlen != bitTorrent10pstrLen {
		return nil, errors.New("unexpected pstrlen")
	}

	_, err = io.ReadFull(conn, buf) // pstr
	if err != nil {
		return nil, err
	}
	if bytes.Compare(buf, bitTorrent10pstr) != 0 {
		return nil, errors.New("unexpected pstr")
	}

	_, err = io.CopyN(ioutil.Discard, conn, 8) // reserved
	if err != nil {
		return nil, err
	}

	var infoHash infoHash
	_, err = io.ReadFull(conn, infoHash[:]) // info_hash
	if err != nil {
		return nil, err
	}

	// The recipient must respond as soon as it sees the info_hash part of the handshake
	// (the peer id will presumably be sent after the recipient sends its own handshake).
	// The tracker's NAT-checking feature does not send the peer_id field of the handshake.
	if notifyInfoHash != nil {
		notifyInfoHash <- &infoHash
	}

	var id peerID
	_, err = io.ReadFull(conn, id[:]) // peer_id
	return &id, err
}

func (r *Rain) sendHandShake(conn net.Conn, ih *infoHash) error {
	var handShake = struct {
		Pstrlen  byte
		Pstr     [bitTorrent10pstrLen]byte
		Reserved [8]byte
		InfoHash infoHash
		PeerID   peerID
	}{
		Pstrlen:  bitTorrent10pstrLen,
		InfoHash: *ih,
		PeerID:   *r.peerID,
	}
	copy(handShake.Pstr[:], bitTorrent10pstr)
	return binary.Write(conn, binary.BigEndian, &handShake)
}

func (r *Rain) connectToPeer(p *Peer, d *download) {
	conn, err := net.DialTCP("tcp4", nil, p.TCPAddr())
	if err != nil {
		log.Error(err)
		return
	}

	log.Info("Connected to peer", conn.RemoteAddr())

	err = r.sendHandShake(conn, &d.TorrentFile.InfoHash)
	if err != nil {
		log.Error(err)
		return
	}

	infoHashC := make(chan *infoHash, 1)
	_, err = r.readHandShake(conn, infoHashC)
	if err != nil {
		log.Error(err)
		return
	}

	if *<-infoHashC != d.TorrentFile.InfoHash {
		log.Error("unexpected info_hash")
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
