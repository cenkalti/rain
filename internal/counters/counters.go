package counters

import "sync/atomic"

type counterName int

// stats
const (
	BytesDownloaded counterName = iota
	BytesUploaded
	BytesWasted
	SeededFor // time.Duration
)

// counters provides concurrent-safe access over set of integers.
type Counters [4]int64

func New(dl, ul, waste, seed int64) Counters {
	var c Counters
	c.Incr(BytesDownloaded, dl)
	c.Incr(BytesUploaded, ul)
	c.Incr(BytesWasted, waste)
	c.Incr(SeededFor, seed)
	return c
}

func (c *Counters) Incr(name counterName, value int64) {
	atomic.AddInt64(&c[name], value)
}

func (c *Counters) Read(name counterName) int64 {
	return atomic.LoadInt64(&c[name])
}
