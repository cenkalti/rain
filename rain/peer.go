package rain

import (
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
	r.communicateWithPeer(conn, d)
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

	b := make([]byte, 1)
	_, err = conn.Read(b)
	if err != nil {
		log.Error(err)
		return
	}

	select {}
}
