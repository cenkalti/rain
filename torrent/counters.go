package torrent

import "sync/atomic"

type counterName int

// stats
const (
	counterBytesDownloaded counterName = iota
	counterBytesUploaded
	counterBytesWasted
	counterSeededFor // time.Duration
)

// counters provides concurrent-safe access over set of integers.
type counters [4]int64

func newCounters(dl, ul, waste, seed int64) counters {
	var c counters
	c.Incr(counterBytesDownloaded, dl)
	c.Incr(counterBytesUploaded, ul)
	c.Incr(counterBytesWasted, waste)
	c.Incr(counterSeededFor, seed)
	return c
}

func (c *counters) Incr(name counterName, value int64) {
	atomic.AddInt64(&c[name], value)
}

func (c *counters) Read(name counterName) int64 {
	return atomic.LoadInt64(&c[name])
}
