package torrent

import (
	"path/filepath"

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
		t, hasStarted, err := s.loadExistingTorrent(id)
		if err != nil {
			s.log.Error(err)
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
		info2, err2 := metainfo.NewInfo(spec.Info)
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
	sto, err := filestorage.New(filepath.Join(s.config.DataDir, id))
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
	)
	if err != nil {
		return
	}
	go s.checkTorrent(t)
	delete(s.availablePorts, spec.Port)

	tt = s.insertTorrent(t)
	return
}
