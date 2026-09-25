package kvstore

import (
	"errors"
	"testing"
	"time"
)

func TestSetAndGet(t *testing.T) {
	store, _ := newTestStore(t)

	if err := store.Set("user:1", "alice"); err != nil {
		t.Fatalf("Set returned %v", err)
	}
	mustGet(t, store, "user:1", "alice")
}

func TestSetOverwritesTheCurrentValue(t *testing.T) {
	store, clock := newTestStore(t)

	if err := store.Set("user:1", "alice"); err != nil {
		t.Fatalf("Set returned %v", err)
	}
	clock.Advance(time.Second)
	if err := store.Set("user:1", "bob"); err != nil {
		t.Fatalf("Set returned %v", err)
	}
	mustGet(t, store, "user:1", "bob")
}

func TestGetUnknownKey(t *testing.T) {
	store, _ := newTestStore(t)

	if _, err := store.Get("missing"); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("Get on unknown key returned %v, want ErrKeyNotFound", err)
	}
}

func TestEmptyKeyIsAValidKey(t *testing.T) {
	store, _ := newTestStore(t)

	if err := store.Set("", "value"); err != nil {
		t.Fatalf("Set with empty key returned %v", err)
	}
	mustGet(t, store, "", "value")
}

func TestDelete(t *testing.T) {
	store, clock := newTestStore(t)

	if err := store.Set("user:1", "alice"); err != nil {
		t.Fatalf("Set returned %v", err)
	}
	clock.Advance(time.Second)

	if err := store.Delete("user:1"); err != nil {
		t.Fatalf("Delete returned %v", err)
	}
	if _, err := store.Get("user:1"); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("Get after Delete returned %v, want ErrKeyNotFound", err)
	}
}

func TestDeleteReportsKeysThatAreAlreadyGone(t *testing.T) {
	store, clock := newTestStore(t)

	if err := store.Delete("never-written"); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("Delete on unknown key returned %v, want ErrKeyNotFound", err)
	}

	if err := store.Set("user:1", "alice"); err != nil {
		t.Fatalf("Set returned %v", err)
	}
	clock.Advance(time.Second)
	if err := store.Delete("user:1"); err != nil {
		t.Fatalf("first Delete returned %v", err)
	}
	clock.Advance(time.Second)
	if err := store.Delete("user:1"); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("second Delete returned %v, want ErrKeyNotFound", err)
	}
}

// A deleted key is only logically gone; writing it again must bring it back as
// a brand new version rather than resurrect the old one.
func TestSetReactivatesADeletedKey(t *testing.T) {
	store, clock := newTestStore(t)

	if err := store.Set("user:1", "alice"); err != nil {
		t.Fatalf("Set returned %v", err)
	}
	clock.Advance(time.Second)
	if err := store.Delete("user:1"); err != nil {
		t.Fatalf("Delete returned %v", err)
	}
	clock.Advance(time.Second)
	if err := store.Set("user:1", "carol"); err != nil {
		t.Fatalf("Set after Delete returned %v", err)
	}

	mustGet(t, store, "user:1", "carol")
}

func TestOperationsAfterCloseAreRejected(t *testing.T) {
	store, _ := newTestStore(t)
	if err := store.Set("user:1", "alice"); err != nil {
		t.Fatalf("Set returned %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close returned %v", err)
	}

	tests := map[string]func() error{
		"Set":        func() error { return store.Set("user:1", "alice") },
		"SetWithTTL": func() error { return store.SetWithTTL("user:1", "alice", time.Minute) },
		"Delete":     func() error { return store.Delete("user:1") },
		"Get": func() error {
			_, err := store.Get("user:1")
			return err
		},
		"GetAt": func() error {
			_, err := store.GetAt("user:1", nanosAt(0))
			return err
		},
		"ScanPrefix": func() error {
			_, err := store.ScanPrefix("user:")
			return err
		},
	}

	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, ErrStoreClosed) {
				t.Errorf("%s after Close returned %v, want ErrStoreClosed", name, err)
			}
		})
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	store, _ := newTestStore(t)

	if err := store.Close(); err != nil {
		t.Fatalf("first Close returned %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second Close returned %v", err)
	}
}
