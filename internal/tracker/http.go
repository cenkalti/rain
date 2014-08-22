package tracker

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"code.google.com/p/bencode-go"
)

type httpTracker struct {
	*trackerBase
	client    http.Client
	trackerID string
}

func newHTTPTracker(b *trackerBase) *httpTracker {
	return &httpTracker{
		trackerBase: b,
	}
}

func (t *httpTracker) Announce(transfer Transfer, cancel <-chan struct{}, event <-chan Event, responseC chan<- *AnnounceResponse) {
	var nextAnnounce time.Duration = time.Nanosecond // Start immediately.
	for {
		select {
		case <-time.After(nextAnnounce):
			t.announce(transfer, cancel, responseC, &nextAnnounce, None)
		case e := <-event:
			t.announce(transfer, cancel, responseC, &nextAnnounce, e)
		case <-cancel:
			return
		}
	}
}

func (t *httpTracker) announce(transfer Transfer, cancel <-chan struct{}, responseC chan<- *AnnounceResponse, nextAnnounce *time.Duration, e Event) {
	// If any error happens before parsing the response, try again in a minute.
	*nextAnnounce = time.Minute

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
		t.log.Error(err)
		return
	}

	if resp.StatusCode >= 400 {
		data, _ := ioutil.ReadAll(resp.Body)
		t.log.Errorf("Status: %d Body: %s", resp.StatusCode, string(data))
		resp.Body.Close()
		return
	}

	var response = new(httpTrackerAnnounceResponse)
	err = bencode.Unmarshal(resp.Body, &response)
	resp.Body.Close()
	if err != nil {
		t.log.Error(err)
		return
	}

	if response.FailureReason != "" {
		t.log.Error(response.FailureReason)

		announceResponse := &AnnounceResponse{
			Error: trackerError(response.FailureReason),
		}

		select {
		case responseC <- announceResponse:
		case <-cancel:
			return
		}

		return
	}
	if response.WarningMessage != "" {
		t.log.Warning(response.WarningMessage)
		return
	}

	*nextAnnounce = time.Duration(response.Interval) * time.Second

	if response.TrackerId != "" {
		t.trackerID = response.TrackerId
	}

	peers, err := t.parsePeers(bytes.NewReader([]byte(response.Peers)))
	if err != nil {
		t.log.Error(err)
		return
	}

	announceResponse := &AnnounceResponse{
		Interval: response.Interval,
		Leechers: response.Incomplete,
		Seeders:  response.Complete,
		Peers:    peers,
	}

	select {
	case responseC <- announceResponse:
	case <-cancel:
		return
	}
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
