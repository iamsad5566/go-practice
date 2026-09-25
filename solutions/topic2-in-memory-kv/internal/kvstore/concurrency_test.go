package kvstore

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestConcurrentOperationsAreRaceFree hammers every entry point at once; it is
// meaningful under -race.
func TestConcurrentOperationsAreRaceFree(t *testing.T) {
	store := New(WithCleanInterval(time.Millisecond), WithRetention(10*time.Millisecond))
	defer store.Close()

	const (
		workers = 16
		rounds  = 200
	)

	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for round := range rounds {
				key := fmt.Sprintf("user:%d", (worker+round)%32)
				switch round % 6 {
				case 0:
					_ = store.Set(key, "value")
				case 1:
					_ = store.SetWithTTL(key, "temporary", time.Millisecond)
				case 2:
					_, _ = store.Get(key)
				case 3:
					_, _ = store.GetAt(key, time.Now().UnixNano())
				case 4:
					_, _ = store.ScanPrefix("user:")
				case 5:
					_ = store.Delete(key)
				}
			}
		}()
	}
	wg.Wait()
}

// Concurrent writers share one entry, so the timestamps they produce must still
// come out strictly increasing — that ordering is what GetAt binary searches.
func TestConcurrentWritesKeepTheHistoryStrictlyOrdered(t *testing.T) {
	store := New(WithCleanInterval(time.Hour))
	defer store.Close()

	const (
		writers        = 16
		writesPerActor = 100
	)

	var wg sync.WaitGroup
	for writer := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range writesPerActor {
				if err := store.Set("hot", fmt.Sprintf("w%d-%d", writer, i)); err != nil {
					t.Errorf("Set returned %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	shard := store.shardFor("hot")
	shard.mu.RLock()
	records := shard.items["hot"].records
	shard.mu.RUnlock()

	if len(records) != writers*writesPerActor {
		t.Fatalf("records = %d, want %d", len(records), writers*writesPerActor)
	}
	for i := 1; i < len(records); i++ {
		if records[i].createdAt <= records[i-1].createdAt {
			t.Fatalf("records[%d].createdAt = %d is not after records[%d].createdAt = %d",
				i, records[i].createdAt, i-1, records[i-1].createdAt)
		}
	}
}

// A reader must never be blocked long enough by a scan to matter: scanning
// takes one shard lock at a time, so writes to other shards keep flowing.
func TestScanPrefixDoesNotBlockWritesToOtherShards(t *testing.T) {
	store := New(WithCleanInterval(time.Hour))
	defer store.Close()

	for i := range 10_000 {
		if err := store.Set(fmt.Sprintf("user:%d", i), "value"); err != nil {
			t.Fatalf("Set returned %v", err)
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 20 {
			if _, err := store.ScanPrefix("user:"); err != nil {
				t.Errorf("ScanPrefix returned %v", err)
				return
			}
		}
	}()

	writes := 0
	for {
		select {
		case <-done:
			if writes == 0 {
				t.Error("no write completed while a scan was running")
			}
			return
		default:
			if err := store.Set("other:key", "value"); err != nil {
				t.Fatalf("Set returned %v", err)
			}
			writes++
		}
	}
}

var (
	sinkValue string
	sinkErr   error
)

// The read path must not allocate: no copying of the history, no escaping
// closure, nothing for the collector to clean up behind a Get.
func TestGetDoesNotAllocate(t *testing.T) {
	store := New(WithCleanInterval(time.Hour))
	defer store.Close()

	if err := store.Set("user:1", "alice"); err != nil {
		t.Fatalf("Set returned %v", err)
	}

	allocs := testing.AllocsPerRun(1_000, func() {
		sinkValue, sinkErr = store.Get("user:1")
	})
	if allocs != 0 {
		t.Errorf("Get allocated %.1f objects per call, want 0", allocs)
	}
	if sinkValue != "alice" || !errors.Is(sinkErr, nil) {
		t.Errorf("Get = (%q, %v), want (%q, nil)", sinkValue, sinkErr, "alice")
	}
}
