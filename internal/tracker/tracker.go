package tracker

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/url"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/shared"
)

// Number of peers we want from trackers
const NumWant = 50

type trackerEvent int32

// Tracker Announce Events
const (
	trackerEventNone trackerEvent = iota
	trackerEventCompleted
	trackerEventStarted
	trackerEventStopped
)

type Tracker interface {
	Announce(t Transfer, cancel <-chan struct{}, event <-chan trackerEvent, peersC chan<- []Peer)
}

type Transfer interface {
	InfoHash() shared.InfoHash
	Downloaded() int64
	Uploaded() int64
	Left() int64
}

type Peer struct {
	IP   [net.IPv4len]byte
	Port uint16
}

func (p Peer) TCPAddr() *net.TCPAddr {
	ip := make(net.IP, net.IPv4len)
	copy(ip, p.IP[:])
	return &net.TCPAddr{
		IP:   ip,
		Port: int(p.Port),
	}
}

func New(trackerURL string, peerID shared.PeerID, port uint16) (Tracker, error) {
	u, err := url.Parse(trackerURL)
	if err != nil {
		return nil, err
	}

	base := &trackerBase{
		url:    u,
		peerID: peerID,
		port:   port,
		log:    logger.New("tracker " + trackerURL),
	}

	switch u.Scheme {
	case "http", "https":
		return newHTTPTracker(base), nil
	case "udp":
		return newUDPTracker(base), nil
	default:
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}
}

type trackerBase struct {
	url    *url.URL
	peerID shared.PeerID
	port   uint16
	log    logger.Logger
}

// parsePeers parses compact representation of peer list.
func (t *trackerBase) parsePeers(r *bytes.Reader) ([]Peer, error) {
	t.log.Debugf("len(rest): %#v", r.Len())
	if r.Len()%6 != 0 {
		return nil, errors.New("invalid peer list")
	}

	count := r.Len() / 6
	t.log.Debugf("count of peers: %#v", count)
	peers := make([]Peer, count)
	for i := 0; i < count; i++ {
		if err := binary.Read(r, binary.BigEndian, &peers[i]); err != nil {
			return nil, err
		}
	}
	t.log.Debugf("peers: %#v\n", peers)

	return peers, nil
}

type trackerError string

func (e trackerError) Error() string { return string(e) }
