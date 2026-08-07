// Package magnet provides support for parsing magnet links.
package magnet

import (
	"cmp"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// Magnet link contains the information to download torrent metadata from network.
type Magnet struct {
	InfoHash [20]byte
	Name     string
	Trackers [][]string
	Peers    []string
}

// New parses the string and returns new Magnet.
func New(s string) (*Magnet, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}

	if u.Scheme != "magnet" {
		return nil, errors.New("not a magnet link")
	}

	params := u.Query()

	xts, ok := params["xt"]
	if !ok {
		return nil, errors.New("missing xt param")
	}
	if len(xts) == 0 {
		return nil, errors.New("empty xt param")
	}

	var magnet Magnet
	magnet.InfoHash, err = parseInfoHash(xts)
	if err != nil {
		return nil, err
	}

	names := params["dn"]
	if len(names) != 0 {
		magnet.Name = names[0]
	}

	var tiers []trackerTier
	for key, tier := range params {
		if key == "tr" {
			for i, tr := range tier {
				tiers = append(tiers, trackerTier{trackers: []string{tr}, index: i - len(tier)})
			}
		} else if strings.HasPrefix(key, "tr.") {
			index, err := strconv.Atoi(key[3:])
			if err == nil && index >= 0 {
				tiers = append(tiers, trackerTier{trackers: tier, index: index})
			}
		}
	}

	slices.SortFunc(tiers, func(a, b trackerTier) int { return cmp.Compare(a.index, b.index) })

	magnet.Trackers = make([][]string, len(tiers))
	for i, ti := range tiers {
		magnet.Trackers[i] = ti.trackers
	}

	magnet.Peers = params["x.pe"]

	return &magnet, nil
}

func (m *Magnet) String() string {
	var b strings.Builder
	b.Grow(2048)
	b.WriteString("magnet:?xt=urn:btih:")
	b.WriteString(hex.EncodeToString(m.InfoHash[:]))
	if m.Name != "" {
		b.WriteString("&dn=")
		b.WriteString(url.QueryEscape(m.Name))
	}
	for i, ti := range m.Trackers {
		if len(ti) == 1 {
			b.WriteString("&tr=")
			b.WriteString(url.QueryEscape(ti[0]))
		} else {
			for _, t := range ti {
				b.WriteString("&tr.")
				b.WriteString(strconv.Itoa(i))
				b.WriteString("=")
				b.WriteString(url.QueryEscape(t))
			}
		}
	}
	for _, p := range m.Peers {
		b.WriteString("&x.pe=")
		b.WriteString(p)
	}
	return b.String()
}

type trackerTier struct {
	trackers []string
	index    int
}

// parseInfoHash returns the v1 info hash found in xts.
// Hybrid magnet links carry both a "urn:btih:" (v1) and a "urn:btmh:" (v2) topic
// in no particular order. Only the v1 topic is usable because v2 is not supported.
func parseInfoHash(xts []string) ([20]byte, error) {
	var v2 bool
	for _, xt := range xts {
		if s, ok := strings.CutPrefix(xt, "urn:btih:"); ok {
			return infoHashString(s)
		}
		if strings.HasPrefix(xt, "urn:btmh:") {
			v2 = true
		}
	}
	if v2 {
		return [20]byte{}, errors.New("magnet link has no v1 info hash: BitTorrent v2 is not supported")
	}
	return [20]byte{}, errors.New("invalid xt param: must start with \"urn:btih:\"")
}

// infoHashString returns a new info hash value from a string.
// s must be 40 (hex encoded) or 32 (base32 encoded) characters, otherwise it returns error.
func infoHashString(s string) ([20]byte, error) {
	var ih [20]byte
	var b []byte
	var err error
	switch len(s) {
	case 40:
		b, err = hex.DecodeString(s)
	case 32:
		b, err = base32.StdEncoding.DecodeString(s)
	default:
		return ih, errors.New("info hash must be 32 or 40 characters")
	}
	if err != nil {
		return ih, err
	}
	copy(ih[:], b)
	return ih, nil
}
