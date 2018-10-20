// Package tracker provides support for announcing torrents to HTTP and UDP trackers.
package tracker

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/logger"
)

type Tracker interface {
	// Announce transfer to the tracker.
	// Announce should be called periodically with the interval returned in AnnounceResponse.
	// Announce should also be called on specific events.
	// TODO specify numwant in announce interface and give 0 for stopped and completed event
	Announce(ctx context.Context, t Transfer, e Event, numWant int) (*AnnounceResponse, error)
	// Close open connections to the tracker.
	Close() error
}

type AnnounceResponse struct {
	Error      error
	Interval   time.Duration
	Leechers   int32
	Seeders    int32
	Peers      []*net.TCPAddr
	ExternalIP net.IP
}

// ParsePeersBinary parses compact representation of peer list.
func ParsePeersBinary(r *bytes.Reader, l logger.Logger) ([]*net.TCPAddr, error) {
	l.Debugf("len(rest): %#v", r.Len())
	if r.Len()%6 != 0 {
		b := make([]byte, r.Len())
		_, _ = r.Read(b)
		l.Debugf("Peers: %q", b)
		return nil, errors.New("invalid peer list")
	}

	count := r.Len() / 6
	l.Debugf("count of peers: %#v", count)
	peers := make([]*net.TCPAddr, count)
	for i := 0; i < count; i++ {
		var peer struct {
			IP   [net.IPv4len]byte
			Port uint16
		}
		if err := binary.Read(r, binary.BigEndian, &peer); err != nil {
			return nil, err
		}
		peers[i] = &net.TCPAddr{IP: peer.IP[:], Port: int(peer.Port)}
	}

	l.Debugf("peers: %#v\n", peers)
	return peers, nil
}

// Error is the string that is sent by the tracker from announce or scrape.
type Error string

func (e Error) Error() string { return string(e) }
