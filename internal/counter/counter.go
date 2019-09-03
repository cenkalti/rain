package counter

import "sync/atomic"

// Counter provides concurrent-safe access over an int64 value.
type Counter int64

func (c *Counter) Add(value int64) {
	atomic.AddInt64((*int64)(c), value)
}

func (c *Counter) Read() int64 {
	return atomic.LoadInt64((*int64)(c))
}
