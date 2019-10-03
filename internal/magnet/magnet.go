// Package magnet provides support for parsing magnet links.
package magnet

import (
	"encoding/base32"
	"encoding/hex"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/multiformats/go-multihash"
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
	xt := xts[0]

	var magnet Magnet
	magnet.InfoHash, err = infoHashString(xt)
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

	sort.Slice(tiers, func(i, j int) bool { return tiers[i].index < tiers[j].index })

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

// infoHashString returns a new info hash value from a string.
// s must be 40 (hex encoded) or 32 (base32 encoded) characters, otherwise it returns error.
func infoHashString(xt string) ([20]byte, error) {
	var ih [20]byte
	var b []byte
	var err error
	switch {
	case strings.HasPrefix(xt, "urn:btih:"):
		xt = xt[9:]
		switch len(xt) {
		case 40:
			b, err = hex.DecodeString(xt)
		case 32:
			b, err = base32.StdEncoding.DecodeString(xt)
		default:
			return ih, errors.New("info hash must be 32 or 40 characters")
		}
		if err != nil {
			return ih, err
		}
	case strings.HasPrefix(xt, "urn:btmh:"):
		xt = xt[9:]
		b, err = multihash.FromHexString(xt)
		if err != nil {
			return ih, err
		}
		if len(b) != 20 {
			return ih, errors.New("invalid multihash (len != 20)")
		}
	default:
		return ih, errors.New("invalid xt param: must start with \"urn:btih:\" or \"urn:btmh\"")
	}
	copy(ih[:], b)
	return ih, nil
}
