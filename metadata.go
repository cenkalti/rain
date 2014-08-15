package rain

import (
	"github.com/cenkalti/rain/internal/protocol"
	"github.com/cenkalti/rain/internal/torrent"
)

func Metadata(ih protocol.InfoHash) (*torrent.Info, error) {
	return &torrent.Info{}, nil
}
