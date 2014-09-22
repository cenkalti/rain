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

func (t *httpTracker) Announce(transfer Transfer, e Event, cancel <-chan struct{}) (*AnnounceResponse, error) {
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
	if e != None {
		q.Set("event", e.String())
	}
	if t.trackerID != "" {
		q.Set("trackerid", t.trackerID)
	}

	u := t.url
	u.RawQuery = q.Encode()
	t.log.Debugf("u.String(): %q", u.String())

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
		resp, err := t.client.Do(req)
		if err != nil {
			errC <- err
		}

		if resp.StatusCode != 200 {
			data, _ := ioutil.ReadAll(resp.Body)
			resp.Body.Close()
			errC <- fmt.Errorf("status not 200 OK (status: %d body: %q)", resp.StatusCode, string(data))
		}

		bodyC <- resp.Body
	}()

	var response = new(httpTrackerAnnounceResponse)

	select {
	case err := <-errC:
		return nil, err
	case <-cancel:
		t.transport.CancelRequest(req)
		return nil, RequestCancelled
	case body := <-bodyC:
		d := bencode.NewDecoder(body)
		err := d.Decode(&response)
		body.Close()
		if err != nil {
			return nil, err
		}
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

	// Peers may be in binary or dictionary model.
	var peers []*net.TCPAddr
	var err error
	if len(response.Peers) > 0 {
		if response.Peers[0] == 'l' {
			peers, err = t.parsePeersDictionary(response.Peers)
		} else {
			peers, err = t.parsePeersBinary(bytes.NewReader(response.Peers))
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

func (t *httpTracker) Scrape(transfers []Transfer) (*ScrapeResponse, error) { return nil, nil }

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
