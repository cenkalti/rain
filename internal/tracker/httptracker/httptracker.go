package httptracker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/tracker"
	"github.com/zeebo/bencode"
)

// HTTPTracker is a torrent tracker that talks HTTP.
type HTTPTracker struct {
	rawURL            string
	url               *url.URL
	log               logger.Logger
	http              *http.Client
	transport         *http.Transport
	trackerID         string
	userAgent         string
	maxResponseLength int64
}

var _ tracker.Tracker = (*HTTPTracker)(nil)

// New returns a new HTTPTracker.
func New(rawURL string, u *url.URL, timeout time.Duration, t *http.Transport, userAgent string, maxResponseLength int64) *HTTPTracker {
	return &HTTPTracker{
		rawURL:            rawURL,
		url:               u,
		log:               logger.New("tracker " + u.String()),
		transport:         t,
		userAgent:         userAgent,
		maxResponseLength: maxResponseLength,
		http: &http.Client{
			Timeout:   timeout,
			Transport: t,
		},
	}
}

// URL returns the URL string of the tracker.
func (t *HTTPTracker) URL() string {
	return t.rawURL
}

// Announce the torrent by doing a GET request to the tracker.
func (t *HTTPTracker) Announce(ctx context.Context, req tracker.AnnounceRequest) (*tracker.AnnounceResponse, error) {
	u := *t.url

	q := u.Query()
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

	u.RawQuery = q.Encode()
	t.log.Debugf("making request to: %q", u.String())

	httpReq := &http.Request{
		Method:     http.MethodGet,
		URL:        &u,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Host:       u.Host,
	}
	httpReq = httpReq.WithContext(ctx)

	httpReq.Header.Set("User-Agent", t.userAgent)

	doReq := func() (int, []byte, error) {
		resp, err := t.http.Do(httpReq)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		if resp.ContentLength > t.maxResponseLength {
			return 0, nil, fmt.Errorf("tracker respsonse too large: %d", resp.ContentLength)
		}
		r := io.LimitReader(resp.Body, t.maxResponseLength)
		data, err := ioutil.ReadAll(r)
		return resp.StatusCode, data, err
	}

	code, body, err := doReq()
	if uerr, ok := err.(*url.Error); ok && uerr.Err == context.Canceled {
		return nil, context.Canceled
	}
	if err != nil {
		return nil, err
	}

	var response announceResponse
	err = bencode.DecodeBytes(body, &response)
	if err != nil {
		if code != 200 {
			return nil, &StatusError{
				Code: code,
				Body: string(body),
			}
		}
		return nil, tracker.ErrDecode
	}

	if response.FailureReason != "" {
		retryIn, _ := strconv.Atoi(response.RetryIn)
		return nil, &tracker.Error{
			FailureReason: response.FailureReason,
			RetryIn:       time.Duration(retryIn) * time.Minute,
		}
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
				return nil, tracker.ErrDecode
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
		Interval:       time.Duration(response.Interval) * time.Second,
		MinInterval:    time.Duration(response.MinInterval) * time.Second,
		Leechers:       response.Incomplete,
		Seeders:        response.Complete,
		Peers:          peers,
		WarningMessage: response.WarningMessage,
	}, nil
}

func parsePeersDictionary(b bencode.RawMessage) ([]*net.TCPAddr, error) {
	var peers []struct {
		IP   string `bencode:"ip"`
		Port uint16 `bencode:"port"`
	}
	err := bencode.DecodeBytes(b, &peers)
	if err != nil {
		return nil, tracker.ErrDecode
	}

	addrs := make([]*net.TCPAddr, len(peers))
	for i, p := range peers {
		pe := &net.TCPAddr{IP: net.ParseIP(p.IP), Port: int(p.Port)}
		addrs[i] = pe
	}
	return addrs, err
}
