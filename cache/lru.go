package cache

import (
	"container/list"
	"sync"
)

// LRU is a bounded, goroutine-safe least-recently-used cache. A non-positive
// capacity makes every Put a no-op, so callers can disable caching uniformly.
type LRU[K comparable, V any] struct {
	mu    sync.Mutex
	cap   int
	ll    *list.List // front = most recently used
	items map[K]*list.Element
}

type entry[K comparable, V any] struct {
	key K
	val V
}

// NewLRU returns a cache holding at most capacity entries.
func NewLRU[K comparable, V any](capacity int) *LRU[K, V] {
	return &LRU[K, V]{
		cap:   capacity,
		ll:    list.New(),
		items: make(map[K]*list.Element),
	}
}

// Get returns the value for key and marks it most-recently-used.
func (c *LRU[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(*entry[K, V]).val, true
	}
	var zero V
	return zero, false
}

// Put inserts or updates key, evicting the least-recently-used entry when the
// cache is over capacity.
func (c *LRU[K, V]) Put(key K, val V) {
	if c.cap <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		el.Value.(*entry[K, V]).val = val
		c.ll.MoveToFront(el)
		return
	}
	c.items[key] = c.ll.PushFront(&entry[K, V]{key: key, val: val})
	if c.ll.Len() > c.cap {
		c.evictOldest()
	}
}

// Cap returns the maximum number of entries the cache holds. It is fixed at
// construction, so no lock is needed.
func (c *LRU[K, V]) Cap() int { return c.cap }

// Len returns the number of cached entries.
func (c *LRU[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

func (c *LRU[K, V]) evictOldest() {
	el := c.ll.Back()
	if el == nil {
		return
	}
	c.ll.Remove(el)
	delete(c.items, el.Value.(*entry[K, V]).key)
}
