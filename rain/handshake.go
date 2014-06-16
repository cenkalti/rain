package rain

import (
	"bytes"
	"encoding/binary"
	"io"
	"io/ioutil"
)

// readHandShake reads handshake from conn and send the result to resultC.
// Results are sent in following order then, resultC is closed:
//     1. error
//     2. infoHash, error
//     3. infoHash, peerID
func (p *peerConn) readHandShake(resultC chan interface{}) {
	// log.Debugln("Reading handshake from", conn.RemoteAddr())
	// defer log.Debugln("Handshake is read from", conn.RemoteAddr())

	if resultC == nil {
		resultC = make(chan interface{}, 2)
	}
	defer close(resultC)

	if cap(resultC) < 2 {
		panic("not enough chan capacity")
	}

	buf := make([]byte, bitTorrent10pstrLen)
	_, err := p.conn.Read(buf[:1]) // pstrlen
	if err != nil {
		resultC <- err
		return
	}
	pstrlen := buf[0]
	if pstrlen != bitTorrent10pstrLen {
		resultC <- err
		return
	}

	_, err = io.ReadFull(p.conn, buf) // pstr
	if err != nil {
		resultC <- err
		return
	}
	if bytes.Compare(buf, bitTorrent10pstr) != 0 {
		resultC <- err
		return
	}

	_, err = io.CopyN(ioutil.Discard, p.conn, 8) // reserved
	if err != nil {
		resultC <- err
		return
	}

	var infoHash infoHash
	_, err = io.ReadFull(p.conn, infoHash[:]) // info_hash
	if err != nil {
		resultC <- err
		return
	}

	// The recipient must respond as soon as it sees the info_hash part of the handshake
	// (the peer id will presumably be sent after the recipient sends its own handshake).
	// The tracker's NAT-checking feature does not send the peer_id field of the handshake.
	resultC <- infoHash

	var id peerID
	_, err = io.ReadFull(p.conn, id[:]) // peer_id
	if err != nil {
		resultC <- err
		return
	}

	resultC <- id
}

func (p *peerConn) readHandShakeBlocking() (*infoHash, *peerID, error) {
	var ih *infoHash
	var id *peerID

	resultC := make(chan interface{}, 2)
	go p.readHandShake(resultC)

	i := <-resultC
	switch res := i.(type) {
	case infoHash:
		ih = &res
	case error:
		return nil, nil, res
	}

	i = <-resultC
	switch res := i.(type) {
	case peerID:
		id = &res
	case error:
		return nil, nil, res
	}

	return ih, id, nil
}

func (p *peerConn) sendHandShake(ih infoHash, id peerID) error {
	var handShake = struct {
		Pstrlen  byte
		Pstr     [bitTorrent10pstrLen]byte
		_        [8]byte
		InfoHash infoHash
		PeerID   peerID
	}{
		Pstrlen:  bitTorrent10pstrLen,
		InfoHash: ih,
		PeerID:   id,
	}
	copy(handShake.Pstr[:], bitTorrent10pstr)
	return binary.Write(p.conn, binary.BigEndian, &handShake)
}
