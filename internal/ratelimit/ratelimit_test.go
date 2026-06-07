package ratelimit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestLimiter returns a limiter whose clock and sweeper we control,
// so tests are deterministic and don't depend on wall-clock timing.
func newTestLimiter(t *testing.T, max int, window time.Duration) (*Limiter, *fakeClock) {
	t.Helper()
	fc := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	l := &Limiter{
		max:        max,
		window:     window,
		now:        fc.Now,
		entries:    make(map[string]*entry),
		sweepEvery: window,
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
	close(l.done) // no background goroutine in tests
	t.Cleanup(func() {
		// Calling Close after we've already closed `done` is fine - Close
		// just selects on stop. Skip it; nothing to clean up.
	})
	return l, fc
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func TestAllowUnderLimit(t *testing.T) {
	l, _ := newTestLimiter(t, 10, time.Hour)
	for i := 0; i < 10; i++ {
		ok, _ := l.Allow("k")
		if !ok {
			t.Fatalf("request %d should be allowed within the quota", i+1)
		}
	}
}

func TestBlocksOverLimit(t *testing.T) {
	l, _ := newTestLimiter(t, 10, time.Hour)
	for i := 0; i < 10; i++ {
		l.Allow("k")
	}
	ok, retry := l.Allow("k")
	if ok {
		t.Fatal("11th request must be blocked")
	}
	if retry <= 0 || retry > time.Hour {
		t.Fatalf("retry after = %v, want (0, 1h]", retry)
	}
}

func TestWindowSlides(t *testing.T) {
	l, fc := newTestLimiter(t, 3, time.Minute)
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("k"); !ok {
			t.Fatalf("warmup request %d should be allowed", i)
		}
	}
	if ok, _ := l.Allow("k"); ok {
		t.Fatal("4th request inside window should be blocked")
	}
	fc.Advance(61 * time.Second) // entire window has rolled past
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("after window expiry the next request should be allowed")
	}
}

func TestPerKeyIsolation(t *testing.T) {
	l, _ := newTestLimiter(t, 2, time.Hour)
	for i := 0; i < 2; i++ {
		l.Allow("a")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("a should be blocked after exhausting its quota")
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("b should not be affected by a's exhaustion")
	}
}

func TestRetryAfterShrinks(t *testing.T) {
	l, fc := newTestLimiter(t, 1, time.Hour)
	l.Allow("k")
	_, r1 := l.Allow("k")
	fc.Advance(30 * time.Minute)
	_, r2 := l.Allow("k")
	if r2 >= r1 {
		t.Fatalf("retry should shrink as time passes: r1=%v r2=%v", r1, r2)
	}
}

func TestConcurrentAllowSafe(t *testing.T) {
	l, _ := newTestLimiter(t, 1000, time.Hour)
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if ok, _ := l.Allow("shared"); ok {
					allowed.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != 1000 {
		t.Fatalf("expected exactly 1000 allowed under a 1000-quota, got %d", got)
	}
}

func TestNewClampsToDefaults(t *testing.T) {
	l := New(-1, -time.Second)
	defer l.Close()
	if l.max != 10 || l.window != time.Hour {
		t.Fatalf("expected defaults, got max=%d window=%v", l.max, l.window)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	l := New(10, time.Hour)
	l.Close()
	l.Close() // must not panic
}
