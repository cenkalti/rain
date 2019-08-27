package torrent

import (
	"bytes"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/resumer"
	"github.com/cenkalti/rain/internal/storage/filestorage"
	"github.com/cenkalti/rain/internal/webseedsource"
)

func (s *Session) loadExistingTorrents(ids []string) {
	var loaded int
	var started []*Torrent
	for _, id := range ids {
		hasStarted, err := s.hasStarted(id)
		if err != nil {
			s.log.Error(err)
			continue
		}
		spec, err := s.resumer.Read(id)
		if err != nil {
			s.log.Error(err)
			continue
		}
		var info *metainfo.Info
		var bf *bitfield.Bitfield
		if len(spec.Info) > 0 {
			info2, err2 := metainfo.NewInfo(spec.Info)
			if err2 != nil {
				s.log.Error(err2)
				continue
			}
			info = info2
			if len(spec.Bitfield) > 0 {
				bf3, err3 := bitfield.NewBytes(spec.Bitfield, info.NumPieces)
				if err3 != nil {
					s.log.Error(err3)
					continue
				}
				bf = bf3
			}
		}
		sto, err := filestorage.New(spec.Dest)
		if err != nil {
			s.log.Error(err)
			continue
		}
		t, err := newTorrent2(
			s,
			id,
			spec.AddedAt,
			spec.InfoHash,
			sto,
			spec.Name,
			spec.Port,
			s.parseTrackers(spec.Trackers, info.IsPrivate()),
			spec.FixedPeers,
			info,
			bf,
			resumer.Stats{
				BytesDownloaded: spec.BytesDownloaded,
				BytesUploaded:   spec.BytesUploaded,
				BytesWasted:     spec.BytesWasted,
				SeededFor:       int64(spec.SeededFor),
			},
			webseedsource.NewList(spec.URLList),
		)
		if err != nil {
			s.log.Error(err)
			continue
		}
		go s.checkTorrent(t)
		delete(s.availablePorts, spec.Port)

		t2 := s.insertTorrent(t)
		s.log.Debugf("loaded existing torrent: #%s %s", id, t.Name())
		loaded++
		if hasStarted {
			started = append(started, t2)
		}
	}
	s.log.Infof("loaded %d existing torrents", loaded)
	for _, t := range started {
		t.torrent.Start()
	}
}

func (s *Session) hasStarted(id string) (bool, error) {
	started := false
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(torrentsBucket).Bucket([]byte(id))
		val := b.Get([]byte("started"))
		if bytes.Equal(val, []byte("1")) {
			started = true
		}
		return nil
	})
	return started, err
}
