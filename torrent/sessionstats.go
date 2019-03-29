package torrent

import (
	"strconv"
	"time"

	"github.com/boltdb/bolt"
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
	}
}

func (s *Session) updateStatsLoop() {
	ticker := time.NewTicker(s.config.StatsWriteInterval)
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
	err := s.db.Update(func(tx *bolt.Tx) error {
		mb := tx.Bucket(torrentsBucket)
		s.mTorrents.RLock()
		for _, t := range s.torrents {
			b := mb.Bucket([]byte(t.torrent.id))
			_ = b.Put(boltdbresumer.Keys.BytesDownloaded, []byte(strconv.FormatInt(t.torrent.counters.Read(counterBytesDownloaded), 10)))
			_ = b.Put(boltdbresumer.Keys.BytesUploaded, []byte(strconv.FormatInt(t.torrent.counters.Read(counterBytesUploaded), 10)))
			_ = b.Put(boltdbresumer.Keys.BytesWasted, []byte(strconv.FormatInt(t.torrent.counters.Read(counterBytesWasted), 10)))
			_ = b.Put(boltdbresumer.Keys.SeededFor, []byte(time.Duration(t.torrent.counters.Read(counterSeededFor)).String()))
		}
		s.mTorrents.RUnlock()
		return nil
	})
	if err != nil {
		s.log.Errorln("cannot update stats:", err.Error())
	}
}
