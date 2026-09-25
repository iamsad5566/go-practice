package kvstore

import (
	"errors"
	"runtime"
	"testing"
	"time"
)

// liveKeyCount counts the keys physically present in the store, which is what
// tells a logical delete apart from a reclaimed one.
func liveKeyCount(store *Store) int {
	total := 0
	for _, shard := range store.shards {
		shard.mu.RLock()
		total += len(shard.items)
		shard.mu.RUnlock()
	}
	return total
}

func TestSweepReclaimsKeysOnlyOnceTheyLeaveTheWindow(t *testing.T) {
	store, clock := newTestStore(t, WithRetention(10*time.Minute))

	if err := store.Set("user:1", "alice"); err != nil {
		t.Fatalf("Set returned %v", err)
	}
	clock.Advance(time.Second)
	if err := store.Delete("user:1"); err != nil {
		t.Fatalf("Delete returned %v", err)
	}

	// Still inside the window: the history has to stay readable.
	clock.Advance(5 * time.Minute)
	store.sweep()
	if got := liveKeyCount(store); got != 1 {
		t.Fatalf("live keys = %d, want 1: history inside the window was reclaimed", got)
	}
	if _, err := store.GetAt("user:1", nanosAt(0)); err != nil {
		t.Errorf("GetAt inside the window returned %v, want the historical value", err)
	}

	// Past the window: nothing can reach it any more.
	clock.Advance(10 * time.Minute)
	store.sweep()
	if got := liveKeyCount(store); got != 0 {
		t.Errorf("live keys = %d, want 0: the dead key was never reclaimed", got)
	}
}

func TestSweepKeepsALiveKeyForever(t *testing.T) {
	store, clock := newTestStore(t, WithRetention(10*time.Minute))

	if err := store.Set("user:1", "alice"); err != nil {
		t.Fatalf("Set returned %v", err)
	}

	clock.Advance(24 * time.Hour)
	store.sweep()

	mustGet(t, store, "user:1", "alice")
}

func TestSweepCollapsesAnOverwrittenHistory(t *testing.T) {
	store, clock := newTestStore(t, WithRetention(time.Minute))

	for _, value := range []string{"v1", "v2", "v3", "v4"} {
		if err := store.Set("user:1", value); err != nil {
			t.Fatalf("Set %q returned %v", value, err)
		}
		clock.Advance(time.Second)
	}

	clock.Advance(time.Hour)
	store.sweep()

	shard := store.shardFor("user:1")
	shard.mu.RLock()
	records := shard.items["user:1"].records
	shard.mu.RUnlock()

	if len(records) != 1 {
		t.Fatalf("records after sweep = %d, want 1", len(records))
	}
	if cap(records) != 1 {
		t.Errorf("cap(records) = %d, want 1: the dropped versions are still reachable", cap(records))
	}
	mustGet(t, store, "user:1", "v4")
}

// Deleting map keys frees their values but not the bucket array, so a shard
// that absorbed a burst has to rebuild its map once the burst is gone.
func TestSweepRebuildsAShardMapThatWentMostlyEmpty(t *testing.T) {
	store, clock := newTestStore(t, WithShardCount(1), WithRetention(time.Minute))

	const burst = 1_000
	for i := range burst {
		key := string(rune('a'+i%26)) + string(rune(i))
		if err := store.Set(key, "value"); err != nil {
			t.Fatalf("Set returned %v", err)
		}
		if err := store.Delete(key); err != nil {
			t.Fatalf("Delete returned %v", err)
		}
		clock.Advance(time.Nanosecond)
	}

	shard := store.shards[0]
	if shard.peak < burst {
		t.Fatalf("peak = %d, want at least %d", shard.peak, burst)
	}

	clock.Advance(time.Hour)
	store.sweep()

	if got := liveKeyCount(store); got != 0 {
		t.Fatalf("live keys = %d, want 0", got)
	}
	if shard.peak != 0 {
		t.Errorf("peak after sweep = %d, want 0: the shard map was never rebuilt", shard.peak)
	}
}

func TestBackgroundCleanerReclaimsWithoutBeingAsked(t *testing.T) {
	clock := newFakeClock()
	store := New(
		WithClock(clock.Now),
		WithRetention(time.Minute),
		WithCleanInterval(time.Millisecond),
	)
	defer store.Close()

	if err := store.Set("user:1", "alice"); err != nil {
		t.Fatalf("Set returned %v", err)
	}
	clock.Advance(time.Second)
	if err := store.Delete("user:1"); err != nil {
		t.Fatalf("Delete returned %v", err)
	}
	clock.Advance(time.Hour)

	deadline := time.Now().Add(2 * time.Second)
	for liveKeyCount(store) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("the background cleaner never reclaimed the dead key")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCloseStopsTheCleanerGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()

	store := New(WithCleanInterval(time.Millisecond))
	if err := store.Set("user:1", "alice"); err != nil {
		t.Fatalf("Set returned %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close returned %v", err)
	}

	// Close waits for the cleaner, so the count must be back immediately.
	if after := runtime.NumGoroutine(); after > before {
		t.Errorf("goroutines: before = %d, after Close = %d: the cleaner leaked", before, after)
	}
	if _, err := store.Get("user:1"); !errors.Is(err, ErrStoreClosed) {
		t.Errorf("Get after Close returned %v, want ErrStoreClosed", err)
	}
}
