// Package magnet provides support for parsing magnet links.
package magnet

import (
	"encoding/base32"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"

	"github.com/multiformats/go-multihash"
)

type Magnet struct {
	InfoHash [20]byte
	Name     string
	Trackers []string
	Peers    []string
}

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

	magnet.Trackers = params["tr"]
	magnet.Peers = params["x.pe"]

	return &magnet, nil
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
