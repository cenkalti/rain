// Provides support for parsing magnet links.

package rain

import (
	"errors"
	"net/url"
	"strings"
)

type Magnet struct {
	InfoHash InfoHash
	Name     string
	Trackers []string
}

func ParseMagnet(s string) (*Magnet, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
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

	magnet.InfoHash, err = NewInfoHashString(xt)
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
