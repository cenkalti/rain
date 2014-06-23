package rain

import (
	"bytes"
	"code.google.com/p/bencode-go"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type httpTracker struct {
	*trackerBase
	client    http.Client
	trackerID string
}

func (r *Rain) newHTTPTracker(u *url.URL) *httpTracker {
	return &httpTracker{
		trackerBase: r.newTrackerBase(u),
	}
}

func (t *httpTracker) Announce(transfer *transfer, cancel <-chan struct{}, event <-chan trackerEvent, responseC chan<- []peerAddr) {
	var nextAnnounce time.Duration = time.Nanosecond // Start immediately.
	for {
		select {
		case <-time.After(nextAnnounce):
			q := url.Values{}
			q.Set("info_hash", url.QueryEscape(string(transfer.torrentFile.Info.Hash[:])))
			q.Set("peer_id", url.QueryEscape(string(t.peerID[:])))
			q.Set("port", strconv.FormatUint(uint64(t.port), 10))
			q.Set("uploaded", strconv.FormatInt(transfer.Uploaded(), 10))
			q.Set("downloaded", strconv.FormatInt(transfer.Downloaded(), 10))
			q.Set("left", strconv.FormatInt(transfer.Left(), 10))
			q.Set("compact", "1")
			q.Set("no_peer_id", "1")
			q.Set("numwant", strconv.Itoa(numWant))
			if t.trackerID != "" {
				q.Set("trackerid", t.trackerID)
			}
			u := t.URL
			u.RawQuery = q.Encode()

			resp, err := t.client.Get(u.String())
			if err != nil {
				t.log.Error(err)
				continue
			}

			var response = new(httpTrackerAnnounceResponse)
			err = bencode.Unmarshal(resp.Body, &response)
			resp.Body.Close()
			if err != nil {
				t.log.Error(err)
				continue
			}

			if response.FailureReason != "" {
				t.log.Error(response.FailureReason)
				continue
			}
			if response.WarningMessage != "" {
				t.log.Warning(response.WarningMessage)
				continue
			}

			nextAnnounce = time.Duration(response.Interval) * time.Second

			if response.TrackerId != "" {
				t.trackerID = response.TrackerId
			}

			peers, err := t.parsePeers(bytes.NewReader([]byte(response.Peers)))
			if err != nil {
				t.log.Error(err)
				continue
			}

			select {
			case responseC <- peers:
			case <-cancel:
				return
			}
		case <-cancel:
			return
		}
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
