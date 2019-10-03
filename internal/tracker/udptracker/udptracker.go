package udptracker

// http://bittorrent.org/beps/bep_0015.html
// http://xbtt.sourceforge.net/udp_tracker_protocol.html

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math/rand"
	"net"
	"net/url"
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/tracker"
)

// UDPTracker is a torrent tracker that speaks UDP.
type UDPTracker struct {
	rawURL    string
	dest      string
	urlData   string
	log       logger.Logger
	transport *Transport
}

var _ tracker.Tracker = (*UDPTracker)(nil)

// New returns a new UDPTracker.
func New(rawURL string, u *url.URL, t *Transport) *UDPTracker {
	return &UDPTracker{
		rawURL:    rawURL,
		dest:      u.Host,
		urlData:   u.RequestURI(),
		log:       logger.New("tracker " + u.Host),
		transport: t,
	}
}

// URL returns the URL string.
func (t *UDPTracker) URL() string {
	return t.rawURL
}

// Announce the torrent to UDP tracker.
func (t *UDPTracker) Announce(ctx context.Context, req tracker.AnnounceRequest) (*tracker.AnnounceResponse, error) {
	request := &announceRequest{
		InfoHash:   req.Torrent.InfoHash,
		PeerID:     req.Torrent.PeerID,
		Downloaded: req.Torrent.BytesDownloaded,
		Left:       req.Torrent.BytesLeft,
		Uploaded:   req.Torrent.BytesUploaded,
		Event:      req.Event,
		Key:        rand.Uint32(),
		NumWant:    int32(req.NumWant),
		Port:       uint16(req.Torrent.Port),
	}
	request.SetAction(actionAnnounce)

	request2 := &transferAnnounceRequest{
		announceRequest: request,
		urlData:         t.urlData,
	}
	trx := newTransaction(request2, t.dest)

	reply, err := t.transport.Do(ctx, trx)
	if err != nil {
		return nil, err
	}

	response, peers, err := t.parseAnnounceResponse(reply)
	if err != nil {
		return nil, tracker.ErrDecode
	}
	t.log.Debugf("Announce response: %#v", response)

	return &tracker.AnnounceResponse{
		Interval: time.Duration(response.Interval) * time.Second,
		Leechers: response.Leechers,
		Seeders:  response.Seeders,
		Peers:    peers,
	}, nil
}

func (t *UDPTracker) parseAnnounceResponse(data []byte) (*udpAnnounceResponse, []*net.TCPAddr, error) {
	var response udpAnnounceResponse
	err := binary.Read(bytes.NewReader(data), binary.BigEndian, &response)
	if err != nil {
		return nil, nil, err
	}
	t.log.Debugf("annouceResponse: %#v", response)
	if response.Action != actionAnnounce {
		return nil, nil, errors.New("invalid action")
	}
	peers, err := tracker.DecodePeersCompact(data[binary.Size(response):])
	if err != nil {
		return nil, nil, err
	}
	return &response, peers, nil
}
