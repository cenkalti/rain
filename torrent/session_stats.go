package torrent

import (
	"strconv"
	"time"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/rain/internal/resumer/boltdbresumer"
)

type SessionStats struct {
	Uptime         time.Duration
	Torrents       int
	Peers          int
	PortsAvailable int

	BlockListRules   int
	BlockListRecency time.Duration

	ReadCacheObjects     int
	ReadCacheSize        int64
	ReadCacheUtilization int

	ReadsPerSecond int
	ReadsActive    int
	ReadsPending   int

	WriteCacheObjects     int
	WriteCacheSize        int64
	WriteCachePendingKeys int

	WritesPerSecond int
	WritesActive    int
	WritesPending   int

	SpeedDownload int
	SpeedUpload   int
	SpeedRead     int
	SpeedWrite    int
}

func (s *Session) Stats() SessionStats {
	return SessionStats{
		Uptime:         time.Duration(s.metrics.Uptime.Value()) * time.Second,
		Torrents:       int(s.metrics.Torrents.Value()),
		Peers:          int(s.metrics.Peers.Count()),
		PortsAvailable: int(s.metrics.PortsAvailable.Value()),

		BlockListRules:   int(s.metrics.BlockListRules.Value()),
		BlockListRecency: time.Duration(s.metrics.BlockListRecency.Value()) * time.Second,

		ReadCacheObjects:     int(s.metrics.ReadCacheObjects.Value()),
		ReadCacheSize:        s.metrics.ReadCacheSize.Value(),
		ReadCacheUtilization: int(s.metrics.ReadCacheUtilization.Value()),

		ReadsPerSecond: int(s.metrics.ReadsPerSecond.Rate1()),
		ReadsActive:    int(s.metrics.ReadsActive.Value()),
		ReadsPending:   int(s.metrics.ReadsPending.Value()),

		WriteCacheObjects:     int(s.metrics.WriteCacheObjects.Value()),
		WriteCacheSize:        s.metrics.WriteCacheSize.Value(),
		WriteCachePendingKeys: int(s.metrics.WriteCachePendingKeys.Value()),

		WritesPerSecond: int(s.metrics.WritesPerSecond.Rate1()),
		WritesActive:    int(s.metrics.WritesActive.Value()),
		WritesPending:   int(s.metrics.WritesPending.Value()),

		SpeedDownload: int(s.metrics.SpeedDownload.Rate1()) / 1024,
		SpeedUpload:   int(s.metrics.SpeedUpload.Rate1()) / 1024,
		SpeedRead:     int(s.metrics.SpeedRead.Rate1()) / 1024,
		SpeedWrite:    int(s.metrics.SpeedWrite.Rate1()) / 1024,
	}
}

func (s *Session) updateStatsLoop() {
	ticker := time.NewTicker(s.config.ResumeWriteInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.updateStats()
		case <-s.closeC:
			return
		}
	}
}

func (s *Session) updateStats() {
	s.mTorrents.RLock()
	defer s.mTorrents.RUnlock()
	err := s.db.Update(func(tx *bolt.Tx) error {
		mb := tx.Bucket(torrentsBucket)
		for _, t := range s.torrents {
			b := mb.Bucket([]byte(t.torrent.id))
			_ = b.Put(boltdbresumer.Keys.BytesDownloaded, []byte(strconv.FormatInt(t.torrent.bytesDownloaded.Count(), 10)))
			_ = b.Put(boltdbresumer.Keys.BytesUploaded, []byte(strconv.FormatInt(t.torrent.bytesUploaded.Count(), 10)))
			_ = b.Put(boltdbresumer.Keys.BytesWasted, []byte(strconv.FormatInt(t.torrent.bytesWasted.Count(), 10)))
			_ = b.Put(boltdbresumer.Keys.SeededFor, []byte(time.Duration(t.torrent.seededFor.Count()).String()))

			t.torrent.mBitfield.RLock()
			if t.torrent.bitfield != nil {
				_ = b.Put(boltdbresumer.Keys.Bitfield, t.torrent.bitfield.Bytes())
			}
		}
		return nil
	})
	for _, t := range s.torrents {
		t.torrent.mBitfield.RUnlock()
	}
	if err != nil {
		s.log.Errorln("cannot update stats:", err.Error())
	}
}
