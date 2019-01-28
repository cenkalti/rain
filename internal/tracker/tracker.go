// Package tracker provides support for announcing torrents to HTTP and UDP trackers.
package tracker

import (
	"context"
	"net"
	"time"
)

type Tracker interface {
	// Announce transfer to the tracker.
	// Announce should be called periodically with the interval returned in AnnounceResponse.
	// Announce should also be called on specific events.
	Announce(ctx context.Context, req AnnounceRequest) (*AnnounceResponse, error)

	// URL of the tracker.
	URL() string
}

type AnnounceRequest struct {
	Torrent Torrent
	Event   Event
	NumWant int
}

type AnnounceResponse struct {
	Error       error
	Interval    time.Duration
	MinInterval time.Duration
	Leechers    int32
	Seeders     int32
	Peers       []*net.TCPAddr
}

// Error is the string that is sent by the tracker from announce or scrape.
type Error string

func (e Error) Error() string { return string(e) }
