package torrent

import (
	"strconv"
	"time"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/rain/internal/counters"
	"github.com/cenkalti/rain/internal/resumer/boltdbresumer"
)

type SessionStats struct {
	Torrents                      int
	AvailablePorts                int
	BlockListRules                int
	BlockListLastSuccessfulUpdate time.Time
	PieceCacheItems               int
	PieceCacheSize                int64
	PieceCacheUtilization         int
	ReadsPerSecond                int
	ReadsActive                   int
	ReadsPending                  int
	ReadBytesPerSecond            int
	ActivePieceBytes              int64
	TorrentsPendingRAM            int
	Uptime                        time.Duration
	WritesPerSecond               int
	WriteBytesPerSecond           int
}

func (s *Session) Stats() SessionStats {
	s.mTorrents.RLock()
	torrents := len(s.torrents)
	s.mTorrents.RUnlock()

	s.mPorts.RLock()
	ports := len(s.availablePorts)
	s.mPorts.RUnlock()

	s.mBlocklist.RLock()
	blocklistTime := s.blocklistTimestamp
	s.mBlocklist.RUnlock()

	ramStats := s.ram.Stats()

	return SessionStats{
		Torrents:                      torrents,
		AvailablePorts:                ports,
		BlockListRules:                s.blocklist.Len(),
		BlockListLastSuccessfulUpdate: blocklistTime,
		PieceCacheItems:               s.pieceCache.Len(),
		PieceCacheSize:                s.pieceCache.Size(),
		PieceCacheUtilization:         s.pieceCache.Utilization(),
		ReadsPerSecond:                s.pieceCache.LoadsPerSecond(),
		ReadsActive:                   s.pieceCache.LoadsActive(),
		ReadsPending:                  s.pieceCache.LoadsWaiting(),
		ReadBytesPerSecond:            s.pieceCache.LoadedBytesPerSecond(),
		ActivePieceBytes:              ramStats.Used,
		TorrentsPendingRAM:            ramStats.Count,
		Uptime:                        time.Since(s.createdAt),
		WritesPerSecond:               int(s.writesPerSecond.Rate()),
		WriteBytesPerSecond:           int(s.writeBytesPerSecond.Rate()),
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
			_ = b.Put(boltdbresumer.Keys.BytesDownloaded, []byte(strconv.FormatInt(t.torrent.counters.Read(counters.BytesDownloaded), 10)))
			_ = b.Put(boltdbresumer.Keys.BytesUploaded, []byte(strconv.FormatInt(t.torrent.counters.Read(counters.BytesUploaded), 10)))
			_ = b.Put(boltdbresumer.Keys.BytesWasted, []byte(strconv.FormatInt(t.torrent.counters.Read(counters.BytesWasted), 10)))
			_ = b.Put(boltdbresumer.Keys.SeededFor, []byte(time.Duration(t.torrent.counters.Read(counters.SeededFor)).String()))

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

func (s *Session) updateSessionStatsLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.writesPerSecond.Tick()
			s.writeBytesPerSecond.Tick()
		case <-s.closeC:
			return
		}
	}
}
