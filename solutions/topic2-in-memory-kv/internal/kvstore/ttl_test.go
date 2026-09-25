package kvstore

import (
	"errors"
	"testing"
	"time"
)

func TestSetWithTTLExpiresTheValue(t *testing.T) {
	store, clock := newTestStore(t)

	if err := store.SetWithTTL("session:1", "token", time.Minute); err != nil {
		t.Fatalf("SetWithTTL returned %v", err)
	}

	clock.Advance(59 * time.Second)
	mustGet(t, store, "session:1", "token")

	// The deadline itself is already outside the lifetime.
	clock.Advance(time.Second)
	if _, err := store.Get("session:1"); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("Get at the TTL deadline returned %v, want ErrKeyNotFound", err)
	}
}

func TestSetWithTTLRejectsNonPositiveTTL(t *testing.T) {
	store, _ := newTestStore(t)

	for _, ttl := range []time.Duration{0, -time.Nanosecond, -time.Hour} {
		if err := store.SetWithTTL("session:1", "token", ttl); !errors.Is(err, ErrInvalidTTL) {
			t.Errorf("SetWithTTL(ttl=%v) returned %v, want ErrInvalidTTL", ttl, err)
		}
	}

	// The rejected writes must not have left a version behind.
	if _, err := store.Get("session:1"); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("Get after rejected SetWithTTL returned %v, want ErrKeyNotFound", err)
	}
}

func TestSetClearsAPreviousTTL(t *testing.T) {
	store, clock := newTestStore(t)

	if err := store.SetWithTTL("session:1", "token", time.Minute); err != nil {
		t.Fatalf("SetWithTTL returned %v", err)
	}
	clock.Advance(time.Second)
	if err := store.Set("session:1", "permanent"); err != nil {
		t.Fatalf("Set returned %v", err)
	}

	clock.Advance(time.Hour)
	mustGet(t, store, "session:1", "permanent")
}

func TestDeleteOnAnExpiredKey(t *testing.T) {
	store, clock := newTestStore(t)

	if err := store.SetWithTTL("session:1", "token", time.Minute); err != nil {
		t.Fatalf("SetWithTTL returned %v", err)
	}
	clock.Advance(2 * time.Minute)

	if err := store.Delete("session:1"); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("Delete on an expired key returned %v, want ErrKeyNotFound", err)
	}
}

func TestSetReactivatesAnExpiredKey(t *testing.T) {
	store, clock := newTestStore(t)

	if err := store.SetWithTTL("session:1", "token", time.Minute); err != nil {
		t.Fatalf("SetWithTTL returned %v", err)
	}
	clock.Advance(2 * time.Minute)
	if err := store.Set("session:1", "renewed"); err != nil {
		t.Fatalf("Set after expiry returned %v", err)
	}

	mustGet(t, store, "session:1", "renewed")
}
