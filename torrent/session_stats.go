package torrent

import (
	"strconv"
	"time"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/rain/internal/resumer/boltdbresumer"
)

type SessionStats struct {
	Torrents                      int
	Peers                         int
	PortsAvailable                int
	BlockListRules                int
	BlockListLastSuccessfulUpdate time.Time
	ReadCacheObjects              int
	ReadCacheSize                 int64
	ReadCacheUtilization          int
	ReadsPerSecond                int
	ReadsActive                   int
	ReadsPending                  int
	WriteCacheObjects             int
	WriteCacheSize                int64
	WriteCachePendingKeys         int
	Uptime                        time.Duration
	WritesPerSecond               int
	WritesActive                  int
	WritesPending                 int
	SpeedDownload                 int
	SpeedUpload                   int
	SpeedRead                     int
	SpeedWrite                    int
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
		PortsAvailable:                ports,
		BlockListRules:                s.blocklist.Len(),
		BlockListLastSuccessfulUpdate: blocklistTime,
		ReadCacheObjects:              s.pieceCache.Len(),
		ReadCacheSize:                 s.pieceCache.Size(),
		ReadCacheUtilization:          s.pieceCache.Utilization(),
		ReadsPerSecond:                s.pieceCache.LoadsPerSecond(),
		ReadsActive:                   s.pieceCache.LoadsActive(),
		ReadsPending:                  s.pieceCache.LoadsWaiting(),
		WriteCacheObjects:             ramStats.AllocatedObjects,
		WriteCacheSize:                ramStats.AllocatedSize,
		WriteCachePendingKeys:         ramStats.PendingKeys,
		Uptime:                        time.Since(s.createdAt),
		WritesPerSecond:               int(s.writesPerSecond.Rate()),
		WritesActive:                  s.semWrite.Len(),
		WritesPending:                 s.semWrite.Waiting(),
		Peers:                         int(s.numPeers.Read()),
		SpeedRead:                     s.pieceCache.LoadedBytesPerSecond() / 1024,
		SpeedWrite:                    int(s.writeBytesPerSecond.Rate()) / 1024,
		SpeedDownload:                 int(s.speedDownload.Rate()) / 1024,
		SpeedUpload:                   int(s.speedUpload.Rate()) / 1024,
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
			_ = b.Put(boltdbresumer.Keys.BytesDownloaded, []byte(strconv.FormatInt(t.torrent.bytesDownloaded.Read(), 10)))
			_ = b.Put(boltdbresumer.Keys.BytesUploaded, []byte(strconv.FormatInt(t.torrent.bytesUploaded.Read(), 10)))
			_ = b.Put(boltdbresumer.Keys.BytesWasted, []byte(strconv.FormatInt(t.torrent.bytesWasted.Read(), 10)))
			_ = b.Put(boltdbresumer.Keys.SeededFor, []byte(time.Duration(t.torrent.seededFor.Read()).String()))

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
			s.speedDownload.Tick()
			s.speedUpload.Tick()
		case <-s.closeC:
			return
		}
	}
}
