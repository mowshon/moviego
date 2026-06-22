package cache_test

import (
	"testing"

	"github.com/mowshon/moviego/v2/cache"
)

func TestLRUEvictsLeastRecentlyUsed(t *testing.T) {
	c := cache.NewLRU[int, string](2)
	c.Put(1, "a")
	c.Put(2, "b")
	// Touch 1 so 2 becomes the eviction candidate.
	if _, ok := c.Get(1); !ok {
		t.Fatal("key 1 missing")
	}
	c.Put(3, "c") // evicts 2

	if _, ok := c.Get(2); ok {
		t.Error("key 2 should have been evicted")
	}
	if v, ok := c.Get(1); !ok || v != "a" {
		t.Errorf("key 1 = %q,%v; want a,true", v, ok)
	}
	if v, ok := c.Get(3); !ok || v != "c" {
		t.Errorf("key 3 = %q,%v; want c,true", v, ok)
	}
	if c.Len() != 2 {
		t.Errorf("len = %d, want 2", c.Len())
	}
}

func TestLRUUpdateExisting(t *testing.T) {
	c := cache.NewLRU[int, int](2)
	c.Put(1, 10)
	c.Put(1, 11)
	if v, _ := c.Get(1); v != 11 {
		t.Errorf("value = %d, want 11", v)
	}
	if c.Len() != 1 {
		t.Errorf("len = %d, want 1", c.Len())
	}
}

func TestLRUZeroCapacityDisabled(t *testing.T) {
	c := cache.NewLRU[int, int](0)
	c.Put(1, 1)
	if _, ok := c.Get(1); ok {
		t.Error("zero-capacity cache should store nothing")
	}
}
