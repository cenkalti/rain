package rain

import (
	"bytes"
	"encoding/binary"
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
	log.Debugln("Serving peer", conn.RemoteAddr())

	// Give a minute for completing handshake.
	err := conn.SetDeadline(time.Now().Add(time.Minute))
	if err != nil {
		return
	}

	var d *download

	resultC := make(chan interface{}, 2)
	go r.readHandShake(conn, resultC)

	// Send handshake as soon as you see info_hash.
	i := <-resultC
	switch res := i.(type) {
	case *infoHash:
		// Do not continue if we don't have a torrent with this infoHash.
		r.downloadsM.Lock()
		var ok bool
		if d, ok = r.downloads[*res]; !ok {
			log.Error("unexpected info_hash")
			r.downloadsM.Unlock()
			return
		}
		r.downloadsM.Unlock()

		err = r.sendHandShake(conn, res)
		if err != nil {
			log.Error(err)
			return
		}
	case error:
		log.Error(res)
		return
	}

	i = <-resultC
	switch res := i.(type) {
	case *peerID:
		// TODO save peer_id
	case error:
		log.Error(res)
		return
	}

	log.Debugln("servePeerConn: Handshake completed", conn.RemoteAddr())
	r.communicateWithPeer(conn, d)
}

// readHandShake reads handshake from conn and send the result to resultC.
// Results are sent in following order then, resultC is closed:
//     1. error
//     2. *infoHash, error
//     3. *infoHash, *peerID
func (r *Rain) readHandShake(conn net.Conn, resultC chan interface{}) {
	log.Debugln("Reading handshake from", conn.RemoteAddr())
	defer log.Debugln("Handshake is read from", conn.RemoteAddr())

	if resultC == nil {
		resultC = make(chan interface{}, 2)
	}
	defer close(resultC)

	if cap(resultC) < 2 {
		panic("not enough chan capacity")
	}

	buf := make([]byte, bitTorrent10pstrLen)
	_, err := conn.Read(buf[:1]) // pstrlen
	if err != nil {
		resultC <- err
		return
	}
	pstrlen := buf[0]
	if pstrlen != bitTorrent10pstrLen {
		resultC <- err
		return
	}

	_, err = io.ReadFull(conn, buf) // pstr
	if err != nil {
		resultC <- err
		return
	}
	if bytes.Compare(buf, bitTorrent10pstr) != 0 {
		resultC <- err
		return
	}

	_, err = io.CopyN(ioutil.Discard, conn, 8) // reserved
	if err != nil {
		resultC <- err
		return
	}

	var infoHash infoHash
	_, err = io.ReadFull(conn, infoHash[:]) // info_hash
	if err != nil {
		resultC <- err
		return
	}

	// The recipient must respond as soon as it sees the info_hash part of the handshake
	// (the peer id will presumably be sent after the recipient sends its own handshake).
	// The tracker's NAT-checking feature does not send the peer_id field of the handshake.
	resultC <- &infoHash

	var id peerID
	_, err = io.ReadFull(conn, id[:]) // peer_id
	if err != nil {
		resultC <- err
		return
	}

	resultC <- &id
}

func (r *Rain) readHandShakeBlocking(conn net.Conn) (*infoHash, *peerID, error) {
	var ih *infoHash
	var id *peerID

	resultC := make(chan interface{}, 2)
	go r.readHandShake(conn, resultC)

	i := <-resultC
	switch res := i.(type) {
	case *infoHash:
		ih = res
	case error:
		return nil, nil, res
	}

	i = <-resultC
	switch res := i.(type) {
	case *peerID:
		id = res
	case error:
		return nil, nil, res
	}

	return ih, id, nil
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
	log.Debugln("Connecting to peer", p.TCPAddr())

	conn, err := net.DialTCP("tcp4", nil, p.TCPAddr())
	if err != nil {
		log.Error(err)
		return
	}
	defer conn.Close()

	log.Infoln("Connected to peer", conn.RemoteAddr())

	// Give a minute for completing handshake.
	err = conn.SetDeadline(time.Now().Add(time.Minute))
	if err != nil {
		return
	}

	err = r.sendHandShake(conn, &d.TorrentFile.InfoHash)
	if err != nil {
		log.Error(err)
		return
	}

	ih, _, err := r.readHandShakeBlocking(conn)
	if err != nil {
		log.Error(err)
		return
	}
	if *ih != d.TorrentFile.InfoHash {
		log.Error("unexpected info_hash")
		return
	}

	log.Debugln("connectToPeer: Handshake completed", conn.RemoteAddr())
	r.communicateWithPeer(conn, d)
}

// communicateWithPeer is the common method that is called after handshake.
// Peer connections are symmetrical.
func (r *Rain) communicateWithPeer(conn net.Conn, d *download) {
	log.Debugln("Communicating peer", conn.RemoteAddr())
	// TODO adjust deadline to heartbeat
	err := conn.SetDeadline(time.Time{})
	if err != nil {
		return
	}

	select {}
}
