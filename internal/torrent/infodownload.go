package torrent

import (
	"github.com/cenkalti/rain/internal/torrent/internal/infodownloader"
	"github.com/cenkalti/rain/internal/torrent/internal/peerprotocol"
)

func (t *Torrent) nextInfoDownload() *infodownloader.InfoDownloader {
	for pe := range t.peers {
		if _, ok := t.infoDownloaders[pe]; ok {
			continue
		}
		if pe.ExtensionHandshake == nil {
			continue
		}
		if pe.ExtensionHandshake.MetadataSize == 0 {
			continue
		}
		_, ok := pe.ExtensionHandshake.M[peerprotocol.ExtensionKeyMetadata]
		if !ok {
			continue
		}
		return infodownloader.New(pe)
	}
	return nil
}
