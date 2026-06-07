// Package ratelimit implements an in-memory sliding-window rate limiter
// keyed by anonymized client identifier.
//
// The limiter is intentionally exact (not a token bucket) so that the
// advertised limit - N requests per window - is enforced precisely. With
// the default settings (10/hour) memory cost is roughly 10 * 24 bytes per
// active key, which scales to millions of distinct clients per gigabyte.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter applies a sliding-window quota of Max requests within Window.
type Limiter struct {
	max     int
	window  time.Duration
	now     func() time.Time
	mu      sync.Mutex
	entries map[string]*entry

	// sweepEvery controls how often the background sweeper runs. It is
	// independent of Window - sweeping too aggressively wastes CPU,
	// sweeping too rarely lets the entries map grow unbounded between
	// genuine traffic peaks.
	sweepEvery time.Duration

	stop chan struct{}
	done chan struct{}
}

type entry struct {
	// times is a ring of request times sorted oldest-first. Cap == Max.
	times []time.Time
}

// New returns a Limiter that allows max requests per window. If max <= 0
// or window <= 0 it falls back to safe defaults rather than rejecting
// every request or accepting every request.
func New(max int, window time.Duration) *Limiter {
	if max <= 0 {
		max = 10
	}

	if window <= 0 {
		window = time.Hour
	}

	l := &Limiter{
		max:        max,
		window:     window,
		now:        time.Now,
		entries:    make(map[string]*entry),
		sweepEvery: window,
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}

	go l.sweepLoop()
	return l
}

// Close stops the background sweeper. Safe to call multiple times.
func (l *Limiter) Close() {
	select {
	case <-l.stop:
		return
	default:
	}

	close(l.stop)
	<-l.done
}

// Allow records a request for key and reports whether it is within quota.
// When false, retryAfter is the duration until the oldest in-window
// request will roll out of the window - the caller can surface this via
// the Retry-After header.
func (l *Limiter) Allow(key string) (allowed bool, retryAfter time.Duration) {
	now := l.now()
	cutoff := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	e := l.entries[key]
	if e == nil {
		e = &entry{times: make([]time.Time, 0, l.max)}
		l.entries[key] = e
	}

	// Drop entries that have aged out of the window.
	e.times = trimBefore(e.times, cutoff)

	if len(e.times) >= l.max {
		oldest := e.times[0]
		return false, oldest.Add(l.window).Sub(now)
	}

	e.times = append(e.times, now)
	return true, 0
}

func (l *Limiter) sweepLoop() {
	defer close(l.done)
	t := time.NewTicker(l.sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-t.C:
			l.sweep()
		}
	}
}

func (l *Limiter) sweep() {
	cutoff := l.now().Add(-l.window)
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, e := range l.entries {
		e.times = trimBefore(e.times, cutoff)
		if len(e.times) == 0 {
			delete(l.entries, k)
		}
	}
}

func trimBefore(times []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(times) && !times[i].After(cutoff) {
		i++
	}
	if i == 0 {
		return times
	}
	// Slide the kept entries to the front without reallocating.
	n := copy(times, times[i:])
	return times[:n]
}
