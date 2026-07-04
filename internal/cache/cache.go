// Package cache is a small in-memory TTL cache with singleflight: identical
// concurrent lookups collapse into one upstream call. Deliberately simple —
// one replica, one process — with an interface small enough to swap for
// Valkey later without touching callers.
package cache

import (
	"sync"
	"time"
)

type entry struct {
	val any
	exp time.Time
}

type call struct {
	wg  sync.WaitGroup
	val any
	err error
}

type Cache struct {
	ttl time.Duration
	max int

	mu       sync.Mutex
	entries  map[string]entry
	inflight map[string]*call
}

// New returns a cache with the given TTL and entry cap. ttl <= 0 disables
// caching entirely (Do always calls fn); max <= 0 defaults to 512.
func New(ttl time.Duration, max int) *Cache {
	if max <= 0 {
		max = 512
	}
	return &Cache{ttl: ttl, max: max, entries: map[string]entry{}, inflight: map[string]*call{}}
}

// Do returns the cached value for key, or runs fn (once, even under
// concurrent callers for the same key) and caches its result. The second
// return reports whether the value came from cache.
func (c *Cache) Do(key string, fn func() (any, error)) (any, bool, error) {
	if c == nil || c.ttl <= 0 {
		v, err := fn()
		return v, false, err
	}

	c.mu.Lock()
	if e, ok := c.entries[key]; ok && time.Now().Before(e.exp) {
		c.mu.Unlock()
		return e.val, true, nil
	}
	if cl, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		cl.wg.Wait()
		return cl.val, true, cl.err
	}
	cl := &call{}
	cl.wg.Add(1)
	c.inflight[key] = cl
	c.mu.Unlock()

	cl.val, cl.err = fn()
	cl.wg.Done()

	c.mu.Lock()
	delete(c.inflight, key)
	if cl.err == nil {
		if len(c.entries) >= c.max {
			c.evictOldestLocked()
		}
		c.entries[key] = entry{val: cl.val, exp: time.Now().Add(c.ttl)}
	}
	c.mu.Unlock()
	return cl.val, false, cl.err
}

// evictOldestLocked drops the entry closest to expiry (and any already
// expired). Called with the lock held.
func (c *Cache) evictOldestLocked() {
	now := time.Now()
	var oldestKey string
	var oldestExp time.Time
	for k, e := range c.entries {
		if now.After(e.exp) {
			delete(c.entries, k)
			continue
		}
		if oldestKey == "" || e.exp.Before(oldestExp) {
			oldestKey, oldestExp = k, e.exp
		}
	}
	if len(c.entries) >= c.max && oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}
