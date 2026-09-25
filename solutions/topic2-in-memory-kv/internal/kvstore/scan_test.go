package kvstore

import (
	"maps"
	"testing"
	"time"
)

func seedScanStore(t *testing.T) (*Store, *fakeClock) {
	t.Helper()

	store, clock := newTestStore(t)
	seed := map[string]string{
		"user:1":    "alice",
		"user:2":    "bob",
		"user:30":   "carol",
		"session:1": "token",
		"":          "root",
	}
	for key, value := range seed {
		if err := store.Set(key, value); err != nil {
			t.Fatalf("Set(%q) returned %v", key, err)
		}
	}

	return store, clock
}

func assertScan(t *testing.T, store *Store, prefix string, want map[string]string) {
	t.Helper()

	got, err := store.ScanPrefix(prefix)
	if err != nil {
		t.Fatalf("ScanPrefix(%q) returned %v", prefix, err)
	}
	if !maps.Equal(got, want) {
		t.Errorf("ScanPrefix(%q) = %v, want %v", prefix, got, want)
	}
}

func TestScanPrefix(t *testing.T) {
	store, _ := seedScanStore(t)

	assertScan(t, store, "user:", map[string]string{
		"user:1":  "alice",
		"user:2":  "bob",
		"user:30": "carol",
	})
	assertScan(t, store, "user:3", map[string]string{"user:30": "carol"})
	assertScan(t, store, "nothing-matches", map[string]string{})
}

func TestScanPrefixWithEmptyPrefixReturnsEverything(t *testing.T) {
	store, _ := seedScanStore(t)

	assertScan(t, store, "", map[string]string{
		"user:1":    "alice",
		"user:2":    "bob",
		"user:30":   "carol",
		"session:1": "token",
		"":          "root",
	})
}

func TestScanPrefixSkipsDeletedAndExpiredKeys(t *testing.T) {
	store, clock := newTestStore(t)

	if err := store.Set("user:1", "alice"); err != nil {
		t.Fatalf("Set returned %v", err)
	}
	if err := store.Set("user:2", "bob"); err != nil {
		t.Fatalf("Set returned %v", err)
	}
	if err := store.SetWithTTL("user:3", "carol", time.Minute); err != nil {
		t.Fatalf("SetWithTTL returned %v", err)
	}
	clock.Advance(time.Second)
	if err := store.Delete("user:2"); err != nil {
		t.Fatalf("Delete returned %v", err)
	}
	clock.Advance(time.Hour)

	assertScan(t, store, "user:", map[string]string{"user:1": "alice"})
}

// The result must be the caller's own map: nothing inside the store may stay
// reachable through it.
func TestScanPrefixReturnsAnIndependentMap(t *testing.T) {
	store, _ := seedScanStore(t)

	got, err := store.ScanPrefix("user:")
	if err != nil {
		t.Fatalf("ScanPrefix returned %v", err)
	}
	got["user:1"] = "tampered"
	delete(got, "user:2")

	mustGet(t, store, "user:1", "alice")
	mustGet(t, store, "user:2", "bob")
}
