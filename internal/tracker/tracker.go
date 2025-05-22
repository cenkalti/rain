// Package tracker provides support for announcing torrents to HTTP and UDP trackers.
package tracker

import (
	"context"
	"errors"
	"net"
	"time"
)

// Tracker tracks the IP address of peers of a Torrent swarm.
type Tracker interface {
	// Announce transfer to the tracker.
	// Announce should be called periodically with the interval returned in AnnounceResponse.
	// Announce should also be called on specific events.
	Announce(ctx context.Context, req AnnounceRequest) (*AnnounceResponse, error)

	// Scrape gets statistics about the torrent from the tracker.
	// Scrape can be called without starting the torrent.
	Scrape(ctx context.Context, infoHash [20]byte) (*ScrapeResponse, error)

	// URL of the tracker.
	URL() string
}

// AnnounceRequest contains the parameters that are sent in an announce request to trackers.
type AnnounceRequest struct {
	Torrent Torrent
	Event   Event
	NumWant int
}

// AnnounceResponse contains fields from a response to announce request.
type AnnounceResponse struct {
	Interval       time.Duration
	MinInterval    time.Duration
	Leechers       int32
	Seeders        int32
	WarningMessage string
	Peers          []*net.TCPAddr
}

// ErrDecode is returned from Tracker.Announce method when there is problem with the encoding of response.
var ErrDecode = errors.New("cannot decode response")

// Error is the string that is sent by the tracker from announce or scrape.
type Error struct {
	FailureReason string
	RetryIn       time.Duration
}

func (e *Error) Error() string { return e.FailureReason }

// ScrapeResponse contains fields from a response to scrape request.
type ScrapeResponse struct {
	// Complete is the number of active peers that have completed downloading (seeders).
	Complete int32
	// Incomplete is the number of active peers that have not completed downloading (leechers).
	Incomplete int32
	// Downloaded is the number of peers that have ever completed downloading.
	Downloaded int32
}
