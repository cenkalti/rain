// Package magnet provides support for parsing magnet links.
package magnet

import (
	"encoding/base32"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
)

type Magnet struct {
	InfoHash [20]byte
	Name     string
	Trackers []string
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
	if !strings.HasPrefix(xt, "urn:btih:") {
		return nil, errors.New("invalid xt param: must start with \"urn:btih:\"")
	}
	xt = xt[9:]

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

	return &magnet, nil
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
