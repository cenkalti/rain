package httptracker

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent/internal/tracker"
	"github.com/zeebo/bencode"
)

type HTTPTracker struct {
	rawURL    string
	url       *url.URL
	log       logger.Logger
	http      *http.Client
	transport *http.Transport
	trackerID string
}

var _ tracker.Tracker = (*HTTPTracker)(nil)

func New(rawURL string, u *url.URL, timeout time.Duration, t *http.Transport) *HTTPTracker {
	return &HTTPTracker{
		rawURL:    rawURL,
		url:       u,
		log:       logger.New("tracker " + u.String()),
		transport: t,
		http: &http.Client{
			Timeout:   timeout,
			Transport: t,
		},
	}
}

func (t *HTTPTracker) URL() string {
	return t.rawURL
}

func (t *HTTPTracker) Announce(ctx context.Context, req tracker.AnnounceRequest) (*tracker.AnnounceResponse, error) {
	q := t.url.Query()
	q.Set("info_hash", string(req.Torrent.InfoHash[:]))
	q.Set("peer_id", string(req.Torrent.PeerID[:]))
	q.Set("port", strconv.FormatUint(uint64(req.Torrent.Port), 10))
	q.Set("uploaded", strconv.FormatInt(req.Torrent.BytesUploaded, 10))
	q.Set("downloaded", strconv.FormatInt(req.Torrent.BytesDownloaded, 10))
	q.Set("left", strconv.FormatInt(req.Torrent.BytesLeft, 10))
	q.Set("compact", "1")
	q.Set("no_peer_id", "1")
	q.Set("numwant", strconv.Itoa(req.NumWant))
	if req.Event != tracker.EventNone {
		q.Set("event", req.Event.String())
	}
	if t.trackerID != "" {
		q.Set("trackerid", t.trackerID)
	}

	u := t.url
	u.RawQuery = q.Encode()
	t.log.Debugf("making request to: %q", u.String())

	httpReq := &http.Request{
		Method:     http.MethodGet,
		URL:        u,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Host:       u.Host,
	}
	httpReq = httpReq.WithContext(ctx)

	doReq := func() ([]byte, error) {
		resp, err := t.http.Do(httpReq)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			data, _ := ioutil.ReadAll(resp.Body)
			return nil, fmt.Errorf("status not 200 OK (status: %d body: %q)", resp.StatusCode, string(data))
		}
		return ioutil.ReadAll(resp.Body)
	}

	body, err := doReq()
	if uerr, ok := err.(*url.Error); ok && uerr.Err == context.Canceled {
		return nil, context.Canceled
	}

	var response announceResponse
	err = bencode.DecodeBytes(body, &response)
	if err != nil {
		return nil, err
	}

	if response.WarningMessage != "" {
		t.log.Warning(response.WarningMessage)
	}
	if response.FailureReason != "" {
		return nil, tracker.Error(response.FailureReason)
	}

	if response.TrackerID != "" {
		t.trackerID = response.TrackerID
	}

	// Peers may be in binary or dictionary model.
	var peers []*net.TCPAddr
	if len(response.Peers) > 0 {
		if response.Peers[0] == 'l' {
			peers, err = parsePeersDictionary(response.Peers)
		} else {
			var b []byte
			err = bencode.DecodeBytes(response.Peers, &b)
			if err != nil {
				return nil, err
			}
			peers, err = tracker.DecodePeersCompact(b)
		}
	}
	if err != nil {
		return nil, err
	}

	// Filter external IP
	if len(response.ExternalIP) != 0 {
		for i, p := range peers {
			if bytes.Equal(p.IP[:], response.ExternalIP) {
				peers[i], peers = peers[len(peers)-1], peers[:len(peers)-1]
				break
			}
		}
	}

	return &tracker.AnnounceResponse{
		Interval:    time.Duration(response.Interval) * time.Second,
		MinInterval: time.Duration(response.MinInterval) * time.Second,
		Leechers:    response.Incomplete,
		Seeders:     response.Complete,
		Peers:       peers,
		ExternalIP:  response.ExternalIP,
	}, nil
}

func parsePeersDictionary(b bencode.RawMessage) ([]*net.TCPAddr, error) {
	var peers []struct {
		IP   string `bencode:"ip"`
		Port uint16 `bencode:"port"`
	}
	err := bencode.DecodeBytes(b, &peers)
	if err != nil {
		return nil, err
	}

	addrs := make([]*net.TCPAddr, len(peers))
	for i, p := range peers {
		pe := &net.TCPAddr{IP: net.ParseIP(p.IP), Port: int(p.Port)}
		addrs[i] = pe
	}
	return addrs, err
}
