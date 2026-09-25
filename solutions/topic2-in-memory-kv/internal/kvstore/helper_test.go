package kvstore

import (
	"sync"
	"testing"
	"time"
)

// baseTime is an arbitrary fixed instant every clock-sensitive test starts from.
var baseTime = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// fakeClock is a manually advanced clock. It is safe for concurrent use so that
// it can also back the background cleaner during tests.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: baseTime}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Rewind simulates a wall-clock jump backwards, e.g. an NTP correction.
func (c *fakeClock) Rewind(d time.Duration) {
	c.Advance(-d)
}

// nanosAt is the unix-nanosecond timestamp of baseTime plus offset, which is
// how point-in-time tests address a moment in a key's history.
func nanosAt(offset time.Duration) int64 {
	return baseTime.Add(offset).UnixNano()
}

// newTestStore builds a store driven by a manual clock, with the background
// cleaner effectively disabled so that tests decide when sweeping happens.
func newTestStore(t *testing.T, opts ...Option) (*Store, *fakeClock) {
	t.Helper()

	clock := newFakeClock()
	base := []Option{WithClock(clock.Now), WithCleanInterval(time.Hour)}
	store := New(append(base, opts...)...)
	t.Cleanup(func() { store.Close() })

	return store, clock
}

func mustGet(t *testing.T, store *Store, key, want string) {
	t.Helper()

	got, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get(%q) returned error %v, want value %q", key, err, want)
	}
	if got != want {
		t.Fatalf("Get(%q) = %q, want %q", key, got, want)
	}
}
