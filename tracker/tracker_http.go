package tracker

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/zeebo/bencode"
)

var httpTimeout = 30 * time.Second

type httpTracker struct {
	*TrackerBase
	http      *http.Client
	transport *http.Transport
	trackerID string
}

func NewHTTPTracker(b *TrackerBase) *httpTracker {
	transport := &http.Transport{
		Dial: (&net.Dialer{
			Timeout: httpTimeout,
		}).Dial,
		TLSHandshakeTimeout: httpTimeout,
		DisableKeepAlives:   true,
	}
	return &httpTracker{
		TrackerBase: b,
		http: &http.Client{
			Timeout:   httpTimeout,
			Transport: transport,
		},
		transport: transport,
	}
}

func (t *httpTracker) Announce(transfer Transfer, e TrackerEvent, cancel <-chan struct{}) (*AnnounceResponse, error) {
	peerID := t.Client.PeerID()
	infoHash := transfer.InfoHash()
	q := url.Values{}
	q.Set("info_hash", string(infoHash[:]))
	q.Set("peer_id", string(peerID[:]))
	q.Set("port", strconv.FormatUint(uint64(t.Client.Port()), 10))
	q.Set("uploaded", strconv.FormatInt(transfer.Uploaded(), 10))
	q.Set("downloaded", strconv.FormatInt(transfer.Downloaded(), 10))
	q.Set("left", strconv.FormatInt(transfer.Left(), 10))
	q.Set("compact", "1")
	q.Set("no_peer_id", "1")
	q.Set("numwant", strconv.Itoa(numWant))
	if e != EventNone {
		q.Set("event", e.String())
	}
	if t.trackerID != "" {
		q.Set("trackerid", t.trackerID)
	}

	u := t.Url
	u.RawQuery = q.Encode()
	t.Log.Debugf("u.String(): %q", u.String())

	req := &http.Request{
		Method:     "GET",
		URL:        u,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Host:       u.Host,
	}

	bodyC := make(chan io.ReadCloser, 1)
	errC := make(chan error, 1)
	go func() {
		resp, err := t.http.Do(req)
		if err != nil {
			errC <- err
			return
		}

		if resp.StatusCode != 200 {
			data, _ := ioutil.ReadAll(resp.Body)
			_ = resp.Body.Close()
			errC <- fmt.Errorf("status not 200 OK (status: %d body: %q)", resp.StatusCode, string(data))
			return
		}

		bodyC <- resp.Body
	}()

	var response = new(httpTrackerAnnounceResponse)

	select {
	case err := <-errC:
		return nil, err
	case <-cancel:
		t.transport.CancelRequest(req)
		return nil, errRequestCancelled
	case body := <-bodyC:
		d := bencode.NewDecoder(body)
		err := d.Decode(&response)
		_ = body.Close()
		if err != nil {
			return nil, err
		}
	}

	if response.WarningMessage != "" {
		t.Log.Warning(response.WarningMessage)
	}
	if response.FailureReason != "" {
		return nil, trackerError(response.FailureReason)
	}

	if response.TrackerId != "" {
		t.trackerID = response.TrackerId
	}

	// Peers may be in binary or dictionary model.
	var peers []*net.TCPAddr
	var err error
	if len(response.Peers) > 0 {
		if response.Peers[0] == 'l' {
			peers, err = t.parsePeersDictionary(response.Peers)
		} else {
			var b []byte
			err = bencode.DecodeBytes(response.Peers, &b)
			if err != nil {
				return nil, err
			}
			peers, err = t.parsePeersBinary(bytes.NewReader(b))
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

	return &AnnounceResponse{
		Interval:   time.Duration(response.Interval) * time.Second,
		Leechers:   response.Incomplete,
		Seeders:    response.Complete,
		Peers:      peers,
		ExternalIP: response.ExternalIP,
	}, nil
}

func (t *httpTracker) parsePeersDictionary(b bencode.RawMessage) ([]*net.TCPAddr, error) {
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

func (t *httpTracker) Scrape(transfers []*Transfer) (*scrapeResponse, error) { return nil, nil }

func (t *httpTracker) Close() error {
	t.transport.CloseIdleConnections()
	return nil
}

type httpTrackerAnnounceResponse struct {
	FailureReason  string             `bencode:"failure reason"`
	WarningMessage string             `bencode:"warning message"`
	Interval       int32              `bencode:"interval"`
	MinInterval    int32              `bencode:"min interval"`
	TrackerId      string             `bencode:"tracker id"`
	Complete       int32              `bencode:"complete"`
	Incomplete     int32              `bencode:"incomplete"`
	Peers          bencode.RawMessage `bencode:"peers"`
	Peers6         string             `bencode:"peers6"`
	ExternalIP     []byte             `bencode:"external ip"`
}
