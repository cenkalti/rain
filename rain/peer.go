package rain

import (
	"encoding/binary"
	"io"
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

		if err = r.sendHandShake(conn, res); err != nil {
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
		if *res == *r.peerID {
			log.Warning("Rejected own connection: server")
			return
		}
		// TODO save peer_id
	case error:
		log.Error(res)
		return
	}

	log.Debugln("servePeerConn: Handshake completed", conn.RemoteAddr())
	p := peerConn{conn: conn, dl: d}
	p.readLoop()
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

	ih, id, err := r.readHandShakeBlocking(conn)
	if err != nil {
		log.Error(err)
		return
	}
	if *ih != d.TorrentFile.InfoHash {
		log.Error("unexpected info_hash")
		return
	}
	if *id == *r.peerID {
		log.Warning("Rejected own connection: client")
		return
	}

	log.Debugln("connectToPeer: Handshake completed", conn.RemoteAddr())
	pc := peerConn{conn: conn, dl: d}
	pc.readLoop()
}

const (
	choke = iota
	unchoke
	interested
	notInterested
	have
	bitfield
	request
	piece
	cancel
)

type peerConn struct {
	conn     net.Conn
	dl       *download
	bitfield *BitField
}

// readLoop processes incoming messages after handshake.
func (p *peerConn) readLoop() {
	log.Debugln("Communicating peer", p.conn.RemoteAddr())
	// TODO adjust deadline to keepAlive
	err := p.conn.SetDeadline(time.Time{})
	if err != nil {
		return
	}

	first := true
	buf := make([]byte, blockSize)
	for {
		var length uint32
		err = binary.Read(p.conn, binary.BigEndian, &length)
		if err != nil {
			log.Error(err)
			return
		}

		if length == 0 { // keepAlive
			log.Debug("came keep-alive")
			// TODO handle keepAlive messages
			continue
		}

		var msgType byte
		err = binary.Read(p.conn, binary.BigEndian, &msgType)
		if err != nil {
			log.Error(err)
			return
		}
		length--
		log.Debug("Received message of type ", msgType)

		switch msgType {
		case choke:
		case unchoke:
		case interested:
		case notInterested:
		case have:
			var i int32
			err = binary.Read(p.conn, binary.BigEndian, &i)
			if err != nil {
				log.Error(err)
				return
			}
			p.bitfield.Set(int64(i))
			log.Debugln("Peer", p.conn.RemoteAddr(), "has piece", i)
			log.Debugln("new bitfield:", p.bitfield.Hex())
		case bitfield:
			if !first {
				log.Error("bitfield can only be sent after handshake")
				return
			}

			_, err = io.LimitReader(p.conn, int64(length)).Read(buf)
			if err != nil {
				log.Error(err)
				return
			}

			div, mod := divMod(p.dl.TorrentFile.TotalLength, p.dl.TorrentFile.Info.PieceLength)
			if mod != 0 {
				div++
			}
			log.Debugln("Torrent contains", div, "pieces")
			b := NewBitField(buf, div)
			log.Debugln("Received bitfield:", b.Hex())
			p.bitfield = &b
		case request:
		case piece:
		case cancel:
		}

		first = false
	}
}
