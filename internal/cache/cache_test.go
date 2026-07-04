package cache

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHitAndExpiry(t *testing.T) {
	c := New(50*time.Millisecond, 10)
	calls := 0
	fn := func() (any, error) { calls++; return "v", nil }

	v, cached, _ := c.Do("k", fn)
	if v != "v" || cached || calls != 1 {
		t.Fatalf("first call: v=%v cached=%v calls=%d", v, cached, calls)
	}
	_, cached, _ = c.Do("k", fn)
	if !cached || calls != 1 {
		t.Fatalf("second call must hit cache: cached=%v calls=%d", cached, calls)
	}
	time.Sleep(60 * time.Millisecond)
	_, cached, _ = c.Do("k", fn)
	if cached || calls != 2 {
		t.Fatalf("expired entry must re-run fn: cached=%v calls=%d", cached, calls)
	}
}

func TestErrorsAreNotCached(t *testing.T) {
	c := New(time.Minute, 10)
	calls := 0
	fn := func() (any, error) { calls++; return nil, errors.New("boom") }
	_, _, err1 := c.Do("k", fn)
	_, _, err2 := c.Do("k", fn)
	if err1 == nil || err2 == nil || calls != 2 {
		t.Fatalf("errors must not be cached: calls=%d", calls)
	}
}

func TestSingleflight(t *testing.T) {
	c := New(time.Minute, 10)
	var calls atomic.Int32
	release := make(chan struct{})
	fn := func() (any, error) {
		calls.Add(1)
		<-release
		return "v", nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _, _ = c.Do("k", fn) }()
	}
	time.Sleep(30 * time.Millisecond) // dejar que todos entren
	close(release)
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Fatalf("singleflight: fn ran %d times, want 1", n)
	}
}

func TestEvictionCap(t *testing.T) {
	c := New(time.Minute, 3)
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		_, _, _ = c.Do(k, func() (any, error) { return k, nil })
	}
	c.mu.Lock()
	n := len(c.entries)
	c.mu.Unlock()
	if n > 3 {
		t.Fatalf("entries = %d, cap 3", n)
	}
}

func TestDisabled(t *testing.T) {
	c := New(0, 10)
	calls := 0
	fn := func() (any, error) { calls++; return "v", nil }
	_, _, _ = c.Do("k", fn)
	_, cached, _ := c.Do("k", fn)
	if cached || calls != 2 {
		t.Fatalf("ttl=0 must disable caching: cached=%v calls=%d", cached, calls)
	}
}
