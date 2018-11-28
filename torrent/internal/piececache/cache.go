package piececache

import (
	"container/heap"
	"sync"
	"time"
)

type Cache struct {
	size, maxSize int64
	items         map[string]*item
	accessList    accessList
	m             sync.Mutex
}

type Loader func() ([]byte, error)

func New(maxSize int64) *Cache {
	return &Cache{
		maxSize: maxSize,
		items:   make(map[string]*item),
	}
}

func (c *Cache) Clear() {
	c.m.Lock()
	c.items = make(map[string]*item)
	c.accessList = nil
	c.m.Unlock()
}

func (c *Cache) Get(key string, loader Loader) ([]byte, error) {
	i := c.getItem(key)
	return c.getValue(i, loader)
}

func (c *Cache) getItem(key string) *item {
	c.m.Lock()
	defer c.m.Unlock()

	i, ok := c.items[key]
	if !ok {
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

	return i.value, nil
}

func (c *Cache) updateAccessTime(i *item) {
	c.m.Lock()
	defer c.m.Unlock()

	i.lastAccessed = time.Now()
	heap.Fix(&c.accessList, i.index)
}

func (c *Cache) makeRoom(i *item) {
	for c.maxSize-c.size < int64(len(i.value)) {
		oldest := heap.Pop(&c.accessList).(*item)
		delete(c.items, oldest.key)
		c.size -= int64(len(oldest.value))
	}
}
