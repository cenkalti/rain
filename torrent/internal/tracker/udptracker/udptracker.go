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
	"github.com/cenkalti/rain/torrent/internal/tracker"
)

type UDPTracker struct {
	url       *url.URL
	log       logger.Logger
	transport *Transport
}

var _ tracker.Tracker = (*UDPTracker)(nil)

func New(u *url.URL) *UDPTracker {
	return &UDPTracker{
		url:       u,
		log:       logger.New("tracker " + u.String()),
		transport: NewTransport(u.Host),
	}
}

func (t *UDPTracker) Announce(ctx context.Context, req tracker.AnnounceRequest) (*tracker.AnnounceResponse, error) {
	transfer := req.Transfer
	e := req.Event
	numWant := req.NumWant
	request := &announceRequest{
		InfoHash:   transfer.InfoHash,
		PeerID:     transfer.PeerID,
		Downloaded: transfer.BytesDownloaded,
		Left:       transfer.BytesLeft,
		Uploaded:   transfer.BytesUploaded,
		Event:      e,
		Key:        rand.Uint32(),
		NumWant:    int32(numWant),
		Port:       uint16(transfer.Port),
	}
	request.SetAction(actionAnnounce)

	request2 := &transferAnnounceRequest{
		announceRequest: request,
		urlData:         t.url.RequestURI(),
	}
	trx := newTransaction(request2)

	reply, err := t.transport.Do(ctx, trx)
	if err == context.Canceled {
		return nil, err
	}
	if err != nil {
		if err, ok := err.(tracker.Error); ok {
			return &tracker.AnnounceResponse{Error: err}, nil
		}
		return nil, err
	}

	response, peers, err := t.parseAnnounceResponse(reply)
	if err != nil {
		return nil, err
	}
	t.log.Debugf("Announce response: %#v", response)

	return &tracker.AnnounceResponse{
		Interval: time.Duration(response.Interval) * time.Second,
		Leechers: response.Leechers,
		Seeders:  response.Seeders,
		Peers:    peers,
	}, nil
}

// Close the tracker connection.
func (t *UDPTracker) Close() {
	t.transport.Close()
}

func (t *UDPTracker) parseAnnounceResponse(data []byte) (*udpAnnounceResponse, []*net.TCPAddr, error) {
	response := new(udpAnnounceResponse)
	if len(data) < binary.Size(response) {
		return nil, nil, errors.New("response is too small")
	}

	reader := bytes.NewReader(data)

	err := binary.Read(reader, binary.BigEndian, response)
	if err != nil {
		return nil, nil, err
	}
	t.log.Debugf("annouceResponse: %#v", response)

	if response.Action != actionAnnounce {
		return nil, nil, errors.New("invalid action")
	}

	peers, err := tracker.ParsePeersBinary(reader, t.log)
	if err != nil {
		return nil, nil, err
	}

	return response, peers, nil
}
