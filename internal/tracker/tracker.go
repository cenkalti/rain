// Package tracker provides support for announcing torrents to HTTP and UDP trackers.
package tracker

import (
	"context"
	"errors"
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
	Interval       time.Duration
	MinInterval    time.Duration
	Leechers       int32
	Seeders        int32
	WarningMessage string
	Peers          []*net.TCPAddr
}

var ErrDecode = errors.New("cannot decode response")

// Error is the string that is sent by the tracker from announce or scrape.
type Error struct {
	FailureReason string
	RetryIn       time.Duration
}

func (e *Error) Error() string { return e.FailureReason }
