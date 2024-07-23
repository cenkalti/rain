package torrent

import (
	"fmt"
	"time"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/resumer"
	"github.com/cenkalti/rain/internal/resumer/boltdbresumer"
	"github.com/cenkalti/rain/internal/storage/filestorage"
	"github.com/cenkalti/rain/internal/webseedsource"
	"go.etcd.io/bbolt"
)

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
	if s.config.ResumeOnStartup {
		for _, t := range started {
			t.torrent.Start()
		}
	}
}

func (s *Session) parseInfo(b []byte, version int) (*metainfo.Info, error) {
	var useUTF8Keys bool
	var hidePaddings bool
	switch version {
	case 1:
	case 2:
		useUTF8Keys = true
	case 3:
		useUTF8Keys = true
		hidePaddings = true
	default:
		return nil, fmt.Errorf("unknown resume data version: %d", version)
	}
	i, err := metainfo.NewInfo(b, useUTF8Keys, hidePaddings)
	if err != nil {
		return nil, err
	}
	if i.NumPieces > s.config.MaxPieces {
		return nil, fmt.Errorf("too many pieces: %d", i.NumPieces)
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
		info2, err2 := s.parseInfo(spec.Info, spec.Version)
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
	sto, err := filestorage.New(s.getDataDir(id), s.config.FilePermissions)
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
		spec.StopAfterMetadata,
		spec.CompleteCmdRun,
	)
	if err != nil {
		return
	}
	t.rawTrackers = spec.Trackers
	t.rawWebseedSources = spec.URLList
	go s.checkTorrent(t)
	delete(s.availablePorts, spec.Port)

	tt = s.insertTorrent(t)
	return
}

// CleanDatabase removes invalid records in the database.
// Normally you don't need to call this.
func (s *Session) CleanDatabase() error {
	err := s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(torrentsBucket)
		for _, id := range s.invalidTorrentIDs {
			err := b.DeleteBucket([]byte(id))
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.invalidTorrentIDs = nil
	return nil
}

// CompactDatabase rewrites the database using existing torrent records to a new file.
// Normally you don't need to call this.
func (s *Session) CompactDatabase(output string) error {
	db, err := bbolt.Open(output, 0600, nil)
	if err != nil {
		return err
	}
	defer db.Close()
	err = db.Update(func(tx *bbolt.Tx) error {
		_, err2 := tx.CreateBucketIfNotExists(torrentsBucket)
		return err2
	})
	if err != nil {
		return err
	}
	res, err := boltdbresumer.New(db, torrentsBucket)
	if err != nil {
		return err
	}
	for _, t := range s.torrents {
		if t.torrent.info == nil {
			s.log.Warningf("skipping torrent %s: info is nil", t.torrent.id)
			continue
		}
		spec := &boltdbresumer.Spec{
			InfoHash:          t.torrent.InfoHash(),
			Port:              t.torrent.port,
			Name:              t.torrent.name,
			Trackers:          t.torrent.rawTrackers,
			URLList:           t.torrent.rawWebseedSources,
			FixedPeers:        t.torrent.fixedPeers,
			Info:              t.torrent.info.Bytes,
			Bitfield:          t.torrent.bitfield.Bytes(),
			AddedAt:           t.torrent.addedAt,
			BytesDownloaded:   t.torrent.bytesDownloaded.Count(),
			BytesUploaded:     t.torrent.bytesUploaded.Count(),
			BytesWasted:       t.torrent.bytesWasted.Count(),
			SeededFor:         time.Duration(t.torrent.seededFor.Count()),
			Started:           t.torrent.status() != Stopped,
			StopAfterDownload: t.torrent.stopAfterDownload,
			StopAfterMetadata: t.torrent.stopAfterMetadata,
			CompleteCmdRun:    t.torrent.completeCmdRun,
		}
		err = res.Write(t.torrent.id, spec)
		if err != nil {
			return err
		}
	}
	return db.Close()
}
