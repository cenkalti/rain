package piececache

import (
	"container/heap"
	"sync"
	"time"

	"github.com/rcrowley/go-metrics"
)

type Cache struct {
	size, maxSize int64
	ttl           time.Duration
	items         map[string]*item
	accessList    accessList
	m             sync.RWMutex

	numCached metrics.EWMA
	numTotal  metrics.EWMA

	closeC chan struct{}
}

type Loader func() ([]byte, error)

func New(maxSize int64, ttl time.Duration) *Cache {
	c := &Cache{
		maxSize:   maxSize,
		ttl:       ttl,
		items:     make(map[string]*item),
		numCached: metrics.NewEWMA1(),
		numTotal:  metrics.NewEWMA1(),
		closeC:    make(chan struct{}),
	}
	go c.tick()
	return c
}

func (c *Cache) tick() {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			c.numCached.Tick()
			c.numTotal.Tick()
		case <-c.closeC:
			return
		}
	}
}

func (c *Cache) Close() {
	close(c.closeC)
}

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

func (c *Cache) Len() int {
	c.m.RLock()
	defer c.m.RUnlock()
	return len(c.items)
}

func (c *Cache) Size() int64 {
	c.m.RLock()
	defer c.m.RUnlock()
	return c.size
}

func (c *Cache) Utilization() int {
	total := c.numTotal.Rate()
	if total == 0 {
		return 0
	}
	return int((100 * c.numCached.Rate()) / total)
}

func (c *Cache) Get(key string, loader Loader) ([]byte, error) {
	i := c.getItem(key)
	return c.getValue(i, loader)
}

func (c *Cache) getItem(key string) *item {
	c.m.Lock()
	defer c.m.Unlock()

	c.numTotal.Update(1)

	i, ok := c.items[key]
	if ok {
		c.numCached.Update(1)
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

	i.value, i.err = loader()
	i.loaded = true

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
		c.removeItem(i)
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
