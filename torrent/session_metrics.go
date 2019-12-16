package torrent

import (
	"time"

	"github.com/rcrowley/go-metrics"
)

type sessionMetrics struct {
	session  *Session
	registry metrics.Registry

	Torrents              metrics.Gauge
	Peers                 metrics.Counter
	PortsAvailable        metrics.Gauge
	Uptime                metrics.Gauge
	BlockListRules        metrics.Gauge
	BlockListRecency      metrics.Gauge
	ReadCacheObjects      metrics.Gauge
	ReadCacheSize         metrics.Gauge
	ReadCacheUtilization  metrics.Gauge
	ReadsPerSecond        metrics.Meter
	ReadsActive           metrics.Gauge
	ReadsPending          metrics.Gauge
	WriteCacheObjects     metrics.Gauge
	WriteCacheSize        metrics.Gauge
	WriteCachePendingKeys metrics.Gauge
	WritesPerSecond       metrics.Meter
	WritesActive          metrics.Gauge
	WritesPending         metrics.Gauge
	SpeedDownload         metrics.Meter
	SpeedUpload           metrics.Meter
	SpeedRead             metrics.Meter
	SpeedWrite            metrics.Meter
}

func (s *Session) initMetrics() {
	r := metrics.NewRegistry()
	s.metrics = &sessionMetrics{
		session:  s,
		registry: r,

		Uptime: metrics.NewRegisteredFunctionalGauge("uptime", r, func() int64 { return int64(time.Since(s.createdAt) / time.Second) }),
		Torrents: metrics.NewRegisteredFunctionalGauge("torrents", r, func() int64 {
			s.mTorrents.RLock()
			defer s.mTorrents.RUnlock()
			return int64(len(s.torrents))
		}),
		Peers: metrics.NewRegisteredCounter("peers", r),
		PortsAvailable: metrics.NewRegisteredFunctionalGauge("ports_available", r, func() int64 {
			s.mPorts.RLock()
			defer s.mPorts.RUnlock()
			return int64(len(s.availablePorts))
		}),

		BlockListRules: metrics.NewRegisteredFunctionalGauge("blocklist_rules", r, func() int64 { return int64(s.blocklist.Len()) }),
		BlockListRecency: metrics.NewRegisteredFunctionalGauge("blocklist_recency", r, func() int64 {
			s.mBlocklist.RLock()
			defer s.mBlocklist.RUnlock()
			if s.blocklistTimestamp.IsZero() {
				return -1
			}
			return int64(time.Since(s.blocklistTimestamp) / time.Second)
		}),

		ReadCacheObjects:     metrics.NewRegisteredFunctionalGauge("read_cache_objects", r, func() int64 { return int64(s.pieceCache.Len()) }),
		ReadCacheSize:        metrics.NewRegisteredFunctionalGauge("read_cache_size", r, func() int64 { return s.pieceCache.Size() }),
		ReadCacheUtilization: metrics.NewRegisteredFunctionalGauge("read_cache_utilization", r, func() int64 { return int64(s.pieceCache.Utilization()) }),

		ReadsPerSecond: s.pieceCache.NumLoad,
		ReadsActive:    metrics.NewRegisteredFunctionalGauge("reads_active", r, func() int64 { return int64(s.pieceCache.LoadsActive()) }),
		ReadsPending:   metrics.NewRegisteredFunctionalGauge("reads_pending", r, func() int64 { return int64(s.pieceCache.LoadsWaiting()) }),

		WriteCacheObjects:     metrics.NewRegisteredFunctionalGauge("write_cache_objects", r, func() int64 { return int64(s.ram.Stats().AllocatedObjects) }),
		WriteCacheSize:        metrics.NewRegisteredFunctionalGauge("write_cache_size", r, func() int64 { return s.ram.Stats().AllocatedSize }),
		WriteCachePendingKeys: metrics.NewRegisteredFunctionalGauge("write_cache_pending_keys", r, func() int64 { return int64(s.ram.Stats().PendingKeys) }),

		WritesPerSecond: metrics.NewRegisteredMeter("writes_per_second", r),
		WritesActive:    metrics.NewRegisteredFunctionalGauge("writes_active", r, func() int64 { return int64(s.semWrite.Len()) }),
		WritesPending:   metrics.NewRegisteredFunctionalGauge("writes_pending", r, func() int64 { return int64(s.semWrite.Waiting()) }),

		SpeedDownload: metrics.NewRegisteredMeter("speed_download", r),
		SpeedUpload:   metrics.NewRegisteredMeter("speed_upload", r),
		SpeedRead:     s.pieceCache.NumLoadedBytes,
		SpeedWrite:    metrics.NewRegisteredMeter("speed_write", r),
	}
	_ = r.Register("speed_read", s.metrics.SpeedRead)
	_ = r.Register("reads_per_seconds", s.metrics.ReadsPerSecond)
}

func (m *sessionMetrics) Close() {
	m.WritesPerSecond.Stop()
	m.SpeedDownload.Stop()
	m.SpeedUpload.Stop()
	m.SpeedWrite.Stop()
}
