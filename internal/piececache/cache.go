package piececache

import (
	"container/heap"
	"sync"
	"time"

	"github.com/cenkalti/rain/internal/semaphore"
	"github.com/rcrowley/go-metrics"
)

// Cache is a LRU piece cache of certain size.
// Items in the cache are expired after the defined TTL.
type Cache struct {
	size, maxSize int64
	ttl           time.Duration
	items         map[string]*item
	accessList    accessList
	m             sync.RWMutex
	sem           *semaphore.Semaphore

	NumCached      metrics.Meter
	NumTotal       metrics.Meter
	NumLoad        metrics.Meter
	NumLoadedBytes metrics.Meter

	closeC chan struct{}
}

// Loader is a function that loads data from a piece.
type Loader func() ([]byte, error)

// New returns new Cache.
func New(maxSize int64, ttl time.Duration, parallelReads uint) *Cache {
	return &Cache{
		maxSize:        maxSize,
		ttl:            ttl,
		items:          make(map[string]*item),
		sem:            semaphore.New(int(parallelReads)),
		NumCached:      metrics.NewMeter(),
		NumTotal:       metrics.NewMeter(),
		NumLoad:        metrics.NewMeter(),
		NumLoadedBytes: metrics.NewMeter(),
		closeC:         make(chan struct{}),
	}
}

// Close the cache and release all resources.
func (c *Cache) Close() {
	close(c.closeC)
	c.NumCached.Stop()
	c.NumTotal.Stop()
	c.NumLoad.Stop()
	c.NumLoadedBytes.Stop()
}

// Clear the cache. Drops all items in cache.
func (c *Cache) Clear() {
	c.m.Lock()
	c.items = make(map[string]*item)
	for _, i := range c.accessList {
		i.timer.Stop()
	}
	c.accessList = nil
	c.size = 0
	c.m.Unlock()
}

// Len returns the number of items in the cache. Items may be in different sizes.
func (c *Cache) Len() int {
	c.m.RLock()
	defer c.m.RUnlock()
	return len(c.items)
}

// LoadsActive returns the number of active Loader calls.
func (c *Cache) LoadsActive() int {
	return (c.sem.Len())
}

// LoadsWaiting returns the number of waiting Loader calls.
func (c *Cache) LoadsWaiting() int {
	return (c.sem.Waiting())
}

// Size returns the total size of the items in the cache.
func (c *Cache) Size() int64 {
	c.m.RLock()
	defer c.m.RUnlock()
	return c.size
}

// Utilization is a number between 0 and 100 that returns the hit-ratio of the cache.
func (c *Cache) Utilization() int {
	total := c.NumTotal.Rate1()
	if total == 0 {
		return 0
	}
	return int((100 * c.NumCached.Rate1()) / total)
}

// Get item with the key from cache. If item is not in cache, load by calling `loader` func and put into the cache.
func (c *Cache) Get(key string, loader Loader) ([]byte, error) {
	i := c.getItem(key)
	return c.getValue(i, loader)
}

func (c *Cache) getItem(key string) *item {
	c.m.Lock()
	defer c.m.Unlock()

	c.NumTotal.Mark(1)

	i, ok := c.items[key]
	if ok {
		c.NumCached.Mark(1)
	} else {
		i = &item{key: key}
		c.items[key] = i
	}
	return i
}

func (c *Cache) getValue(i *item, loader Loader) ([]byte, error) {
	i.Lock()
	defer i.Unlock()

	if i.loaded {
		if i.err != nil {
			return nil, i.err
		}
		c.updateAccessTime(i)
		return i.value, nil
	}

	c.sem.Wait()
	i.value, i.err = loader()
	c.sem.Signal()
	i.loaded = true
	c.NumLoad.Mark(1)
	c.NumLoadedBytes.Mark(int64(len(i.value)))

	return c.handleNewItem(i)
}

func (c *Cache) handleNewItem(i *item) ([]byte, error) {
	c.m.Lock()
	defer c.m.Unlock()

	if i.err != nil {
		delete(c.items, i.key)
		return nil, i.err
	}

	// Do not cache values larger than cache size.
	if int64(len(i.value)) > c.maxSize {
		delete(c.items, i.key)
		return i.value, nil
	}

	c.makeRoom(i)

	c.size += int64(len(i.value))

	i.lastAccessed = time.Now()
	heap.Push(&c.accessList, i)

	i.timer = time.AfterFunc(c.ttl, func() {
		c.m.Lock()
		if i.index != -1 {
			c.removeItem(i)
		}
		c.m.Unlock()
	})

	return i.value, nil
}

func (c *Cache) updateAccessTime(i *item) {
	c.m.Lock()
	defer c.m.Unlock()

	i.lastAccessed = time.Now()
	heap.Fix(&c.accessList, i.index)

	i.timer.Reset(c.ttl)
}

func (c *Cache) makeRoom(i *item) {
	for c.maxSize-c.size < int64(len(i.value)) {
		i := c.accessList[0]
		c.removeItem(i)
	}
}

func (c *Cache) removeItem(i *item) {
	i.timer.Stop()
	delete(c.items, i.key)
	heap.Remove(&c.accessList, i.index)
	c.size -= int64(len(i.value))
}
