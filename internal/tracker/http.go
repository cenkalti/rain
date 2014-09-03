package tracker

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/zeebo/bencode"
)

var HTTPTimeout = 30 * time.Second

type httpTracker struct {
	*trackerBase
	client    *http.Client
	transport *http.Transport
	trackerID string
}

func newHTTPTracker(b *trackerBase) *httpTracker {
	transport := &http.Transport{
		Dial: (&net.Dialer{
			Timeout: HTTPTimeout,
		}).Dial,
		TLSHandshakeTimeout: HTTPTimeout,
		DisableKeepAlives:   true,
	}
	return &httpTracker{
		trackerBase: b,
		client: &http.Client{
			Timeout:   HTTPTimeout,
			Transport: transport,
		},
		transport: transport,
	}
}

func (t *httpTracker) Announce(transfer Transfer, e Event) (*AnnounceResponse, error) {
	infoHash := transfer.InfoHash()
	q := url.Values{}
	q.Set("info_hash", string(infoHash[:]))
	q.Set("peer_id", string(t.peerID[:]))
	q.Set("port", strconv.FormatUint(uint64(t.port), 10))
	q.Set("uploaded", strconv.FormatInt(transfer.Uploaded(), 10))
	q.Set("downloaded", strconv.FormatInt(transfer.Downloaded(), 10))
	q.Set("left", strconv.FormatInt(transfer.Left(), 10))
	q.Set("compact", "1")
	q.Set("no_peer_id", "1")
	q.Set("numwant", strconv.Itoa(NumWant))
	q.Set("event", e.String())
	if t.trackerID != "" {
		q.Set("trackerid", t.trackerID)
	}
	u := t.url
	u.RawQuery = q.Encode()
	t.log.Debugf("u.String(): %q", u.String())

	resp, err := t.client.Get(u.String())
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		data, _ := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("status not 200 OK (status: %d body: %q)", resp.StatusCode, string(data))
	}

	var response = new(httpTrackerAnnounceResponse)
	d := bencode.NewDecoder(resp.Body)
	err = d.Decode(&response)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}

	if response.WarningMessage != "" {
		t.log.Warning(response.WarningMessage)
	}
	if response.FailureReason != "" {
		return nil, Error(response.FailureReason)
	}

	if response.TrackerId != "" {
		t.trackerID = response.TrackerId
	}

	peers, err := t.parsePeers(bytes.NewReader([]byte(response.Peers)))
	if err != nil {
		return nil, err
	}

	return &AnnounceResponse{
		Interval: time.Duration(response.Interval) * time.Second,
		Leechers: response.Incomplete,
		Seeders:  response.Complete,
		Peers:    peers,
	}, nil
}

func (t *httpTracker) Close() error {
	t.transport.CloseIdleConnections()
	return nil
}

type httpTrackerAnnounceResponse struct {
	FailureReason  string `bencode:"failure reason"`
	WarningMessage string `bencode:"warning message"`
	Interval       int32  `bencode:"interval"`
	MinInterval    int32  `bencode:"min interval"`
	TrackerId      string `bencode:"tracker id"`
	Complete       int32  `bencode:"complete"`
	Incomplete     int32  `bencode:"incomplete"`
	Peers          string `bencode:"peers"`
	Peers6         string `bencode:"peers6"`
}
