package httptracker

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/rain/v2/internal/logger"
	"github.com/cenkalti/rain/v2/internal/tracker"
	"github.com/zeebo/bencode"
)

// HTTPTracker is a torrent tracker that talks HTTP.
type HTTPTracker struct {
	rawURL            string
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
		log:               logger.New("tracker " + u.Host),
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
	// Some private trackers require that first two parameters be info_hash and peer_id.
	// This is the reason we don't use url.Values to encode query params.
	var sb strings.Builder
	sb.WriteString(t.rawURL)
	if strings.ContainsRune(t.rawURL, '?') {
		sb.WriteString("&info_hash=")
	} else {
		sb.WriteString("?info_hash=")
	}
	sb.WriteString(percentEscape(req.Torrent.InfoHash))
	sb.WriteString("&peer_id=")
	sb.WriteString(percentEscape(req.Torrent.PeerID))
	sb.WriteString("&port=")
	sb.WriteString(strconv.Itoa(req.Torrent.Port))
	sb.WriteString("&uploaded=")
	sb.WriteString(strconv.FormatInt(req.Torrent.BytesUploaded, 10))
	sb.WriteString("&downloaded=")
	sb.WriteString(strconv.FormatInt(req.Torrent.BytesDownloaded, 10))
	sb.WriteString("&left=")
	sb.WriteString(strconv.FormatInt(req.Torrent.BytesLeft, 10))
	sb.WriteString("&compact=1")
	sb.WriteString("&no_peer_id=1")
	sb.WriteString("&numwant=")
	sb.WriteString(strconv.Itoa(req.NumWant))

	if req.Event != tracker.EventNone {
		sb.WriteString("&event=")
		sb.WriteString(req.Event.String())
	}
	if t.trackerID != "" {
		sb.WriteString("&trackerid=")
		sb.WriteString(t.trackerID)
	}
	sb.WriteString("&key=")
	sb.WriteString(hex.EncodeToString(req.Torrent.PeerID[16:20]))

	t.log.Debugf("making request to: %q", sb.String())

	httpReq, err := http.NewRequest(http.MethodGet, sb.String(), nil)
	if err != nil {
		return nil, err
	}
	httpReq = httpReq.WithContext(ctx)

	httpReq.Header.Set("User-Agent", t.userAgent)

	doReq := func() (int, http.Header, []byte, error) {
		resp, err := t.http.Do(httpReq)
		if err != nil {
			return 0, nil, nil, err
		}
		t.log.Debugf("tracker responded %d with %d bytes body", resp.StatusCode, resp.ContentLength)
		defer resp.Body.Close()
		if resp.ContentLength > t.maxResponseLength {
			return 0, resp.Header, nil, fmt.Errorf("tracker respsonse too large: %d", resp.ContentLength)
		}
		r := io.LimitReader(resp.Body, t.maxResponseLength)
		data, err := io.ReadAll(r)
		return resp.StatusCode, resp.Header, data, err
	}

	code, header, body, err := doReq()
	if uerr, ok := err.(*url.Error); ok && uerr.Err == context.Canceled {
		return nil, context.Canceled
	}
	if err != nil {
		return nil, err
	}
	t.log.Debugf("read %d bytes from body", len(body))

	var response announceResponse
	err = bencode.DecodeBytes(body, &response)
	if err != nil {
		if code != 200 {
			return nil, &StatusError{
				Code:   code,
				Header: header,
				Body:   string(body),
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
	t.log.Debugf("got %d peers", len(peers))

	// Filter external IP
	if len(response.ExternalIP) != 0 {
		var filtered int
		for i, p := range peers {
			if !bytes.Equal(p.IP[:], response.ExternalIP) {
				peers[i] = p
				filtered++
			}
		}
		peers = peers[:filtered]
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

// percentEscape puts `%` before every byte.
// Some trackers don't like the output of url.QueryEscape function because it may skip encoding safe characters.
// This function escapes every byte explicitly.
func percentEscape(b [20]byte) string {
	var sb strings.Builder
	sb.Grow(60)
	s := hex.EncodeToString(b[:])
	for i := 0; i < 20; i++ {
		sb.WriteRune('%')
		sb.WriteByte(s[i*2])
		sb.WriteByte(s[i*2+1])
	}
	return sb.String()
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

// Scrape gets statistics about the torrent from the tracker.
func (t *HTTPTracker) Scrape(ctx context.Context, infoHash [20]byte) (*tracker.ScrapeResponse, error) {
	// Convert announce URL to scrape URL
	scrapeURL, err := announceToScrape(t.rawURL)
	if err != nil {
		return nil, err
	}

	// Build scrape URL with info_hash parameter
	var sb strings.Builder
	sb.WriteString(scrapeURL)
	if strings.ContainsRune(scrapeURL, '?') {
		sb.WriteString("&info_hash=")
	} else {
		sb.WriteString("?info_hash=")
	}
	sb.WriteString(percentEscape(infoHash))

	t.log.Debugf("making scrape request to: %q", sb.String())

	httpReq, err := http.NewRequest(http.MethodGet, sb.String(), nil)
	if err != nil {
		return nil, err
	}
	httpReq = httpReq.WithContext(ctx)

	httpReq.Header.Set("User-Agent", t.userAgent)

	resp, err := t.http.Do(httpReq)
	if uerr, ok := err.(*url.Error); ok && uerr.Err == context.Canceled {
		return nil, context.Canceled
	}
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, t.maxResponseLength))
		return nil, &StatusError{
			Code:   resp.StatusCode,
			Header: resp.Header,
			Body:   string(body),
		}
	}

	if resp.ContentLength > t.maxResponseLength {
		return nil, fmt.Errorf("tracker response too large: %d", resp.ContentLength)
	}

	r := io.LimitReader(resp.Body, t.maxResponseLength)
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	t.log.Debugf("read %d bytes from scrape response", len(body))

	var response scrapeResponse
	err = bencode.DecodeBytes(body, &response)
	if err != nil {
		return nil, tracker.ErrDecode
	}

	if response.FailureReason != "" {
		retryIn, _ := strconv.Atoi(response.RetryIn)
		return nil, &tracker.Error{
			FailureReason: response.FailureReason,
			RetryIn:       time.Duration(retryIn) * time.Minute,
		}
	}

	files, ok := response.Files[string(infoHash[:])]
	if !ok {
		return &tracker.ScrapeResponse{}, nil
	}

	return &tracker.ScrapeResponse{
		Complete:   files.Complete,
		Incomplete: files.Incomplete,
		Downloaded: files.Downloaded,
	}, nil
}

// announceToScrape converts an announce URL to a scrape URL.
// According to BEP 48, if the announce URL is /announce, then the scrape URL is /scrape.
// If the announce URL is /x/announce, then the scrape URL is /x/scrape.
func announceToScrape(announceURL string) (string, error) {
	u, err := url.Parse(announceURL)
	if err != nil {
		return "", err
	}

	// Check if the URL path ends with "announce"
	path := u.Path
	if !strings.HasSuffix(path, "/announce") {
		return "", fmt.Errorf("announce URL does not end with /announce: %s", announceURL)
	}

	// Replace "announce" with "scrape"
	u.Path = path[:len(path)-len("announce")] + "scrape"
	return u.String(), nil
}
