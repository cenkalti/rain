package dialer

import (
	"net"
	"sync"

	"github.com/cenkalti/rain/internal/announcer"
	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/btconn"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
)

type Dialer struct {
	peerID        [20]byte
	infoHash      [20]byte
	announcer     *announcer.Announcer
	bitfield      *bitfield.Bitfield
	limiter       chan struct{}
	peerConnected chan *peer.Peer
	peerMessages  chan peer.Message
}

func New(a *announcer.Announcer, peerID, infoHash [20]byte, b *bitfield.Bitfield, limiter chan struct{}, peerConnected chan *peer.Peer, peerMessages chan peer.Message) *Dialer {
	return &Dialer{
		peerID:        peerID,
		infoHash:      infoHash,
		announcer:     a,
		bitfield:      b,
		limiter:       limiter,
		peerConnected: peerConnected,
		peerMessages:  peerMessages,
	}
}

func (d *Dialer) Run(stopC chan struct{}) {
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		select {
		case d.limiter <- struct{}{}:
			addr := d.announcer.NextPeerAddr()
			if addr == nil {
				<-d.limiter
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-d.limiter }()
				d.dialAndRun(addr)
			}()
		case <-stopC:
			return
		}
	}
}

func (d *Dialer) dialAndRun(addr net.Addr) {
	log := logger.New("peer -> " + addr.String())

	// TODO get this from config
	encryptionDisableOutgoing := false
	encryptionForceOutgoing := false

	conn, cipher, extensions, peerID, err := btconn.Dial(addr, !encryptionDisableOutgoing, encryptionForceOutgoing, [8]byte{}, d.infoHash, d.peerID)
	if err != nil {
		if err == btconn.ErrOwnConnection {
			log.Debug(err)
		} else {
			log.Error(err)
		}
		return
	}
	log.Infof("Connected to peer. (cipher=%s extensions=%x client=%q)", cipher, extensions, peerID[:8])
	defer func() {
		err := conn.Close()
		if err != nil {
			log.Errorln("cannot close conn:", err)
		}
	}()

	p := peer.New(conn, peerID, d.bitfield.Len(), log, d.peerMessages)
	d.peerConnected <- p
	p.Run(d.bitfield)
}
