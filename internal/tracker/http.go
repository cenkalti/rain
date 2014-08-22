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

	"code.google.com/p/bencode-go"
)

var HTTPTimeout = 30 * time.Second

type httpTracker struct {
	*trackerBase
	client    *http.Client
	trackerID string
}

func newHTTPTracker(b *trackerBase) *httpTracker {
	return &httpTracker{
		trackerBase: b,
		client: &http.Client{
			Timeout: HTTPTimeout,
			Transport: &http.Transport{
				Dial: (&net.Dialer{
					Timeout: HTTPTimeout,
				}).Dial,
				TLSHandshakeTimeout: HTTPTimeout,
				DisableKeepAlives:   true,
			},
		},
	}
}

func (t *httpTracker) Announce(transfer Transfer, cancel <-chan struct{}, event <-chan Event, responseC chan<- *AnnounceResponse) {
	var nextAnnounce time.Duration

	announce := func(e Event) {
		r, err := t.announce(transfer, e)
		if err != nil {
			t.log.Error(err)
			r = &AnnounceResponse{Error: err}
			nextAnnounce = HTTPTimeout
		} else {
			nextAnnounce = r.Interval
		}
		select {
		case responseC <- r:
		case <-cancel:
			return
		}
	}

	announce(None)
	for {
		select {
		case <-time.After(nextAnnounce):
			announce(None)
		case e := <-event:
			announce(e)
		case <-cancel:
			return
		}
	}
}

func (t *httpTracker) announce(transfer Transfer, e Event) (*AnnounceResponse, error) {
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
	err = bencode.Unmarshal(resp.Body, &response)
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
