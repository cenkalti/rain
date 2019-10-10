package torrent

import (
	"errors"
	"path/filepath"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/resumer"
	"github.com/cenkalti/rain/internal/storage/filestorage"
	"github.com/cenkalti/rain/internal/webseedsource"
)

var errTooManyPieces = errors.New("too many pieces")

func (s *Session) loadExistingTorrents(ids []string) {
	var loaded int
	var started []*Torrent
	for _, id := range ids {
		t, hasStarted, err := s.loadExistingTorrent(id)
		if err != nil {
			s.log.Error(err)
			s.invalidTorrentIDs = append(s.invalidTorrentIDs, id)
			continue
		}
		s.log.Debugf("loaded existing torrent: #%s %s", id, t.Name())
		loaded++
		if hasStarted {
			started = append(started, t)
		}
	}
	s.log.Infof("loaded %d existing torrents", loaded)
	for _, t := range started {
		t.torrent.Start()
	}
}

func (s *Session) parseInfo(b []byte) (*metainfo.Info, error) {
	i, err := metainfo.NewInfo(b)
	if err != nil {
		return nil, err
	}
	if i.NumPieces > s.config.MaxPieces {
		return nil, errTooManyPieces
	}
	return i, nil
}

func (s *Session) loadExistingTorrent(id string) (tt *Torrent, hasStarted bool, err error) {
	spec, err := s.resumer.Read(id)
	if err != nil {
		return
	}
	hasStarted = spec.Started
	var info *metainfo.Info
	var bf *bitfield.Bitfield
	var private bool
	if len(spec.Info) > 0 {
		info2, err2 := s.parseInfo(spec.Info)
		if err2 != nil {
			return nil, spec.Started, err2
		}
		info = info2
		private = info.Private
		if len(spec.Bitfield) > 0 {
			bf3, err3 := bitfield.NewBytes(spec.Bitfield, info.NumPieces)
			if err3 != nil {
				return nil, spec.Started, err3
			}
			bf = bf3
		}
	}
	var dest string
	if s.config.DataDirIncludesTorrentID {
		dest = filepath.Join(s.config.DataDir, id)
	} else {
		dest = s.config.DataDir
	}
	sto, err := filestorage.New(dest)
	if err != nil {
		return
	}
	t, err := newTorrent2(
		s,
		id,
		spec.AddedAt,
		spec.InfoHash,
		sto,
		spec.Name,
		spec.Port,
		s.parseTrackers(spec.Trackers, private),
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
		spec.StopAfterDownload,
	)
	if err != nil {
		return
	}
	go s.checkTorrent(t)
	delete(s.availablePorts, spec.Port)

	tt = s.insertTorrent(t)
	return
}

// CleanDatabase removes invalid records in the database.
// Normally you don't need to call this.
func (s *Session) CleanDatabase() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(torrentsBucket)
		for _, id := range s.invalidTorrentIDs {
			err := b.DeleteBucket([]byte(id))
			if err != nil {
				return err
			}
		}
		return nil
	})
}
