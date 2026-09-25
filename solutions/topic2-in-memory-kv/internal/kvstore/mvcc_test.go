package kvstore

import (
	"errors"
	"testing"
	"time"
)

// historyStore builds: v1 at +0s, v2 at +10s, deleted at +20s.
func historyStore(t *testing.T) (*Store, *fakeClock) {
	t.Helper()

	store, clock := newTestStore(t)
	if err := store.Set("user:1", "v1"); err != nil {
		t.Fatalf("Set v1 returned %v", err)
	}
	clock.Advance(10 * time.Second)
	if err := store.Set("user:1", "v2"); err != nil {
		t.Fatalf("Set v2 returned %v", err)
	}
	clock.Advance(10 * time.Second)
	if err := store.Delete("user:1"); err != nil {
		t.Fatalf("Delete returned %v", err)
	}

	return store, clock
}

func TestGetAtTravelsThroughTheHistory(t *testing.T) {
	store, _ := historyStore(t)

	tests := []struct {
		name      string
		at        int64
		wantValue string
		wantErr   error
	}{
		{name: "before the key existed", at: nanosAt(-time.Nanosecond), wantErr: ErrKeyNotFound},
		{name: "exactly at the first write", at: nanosAt(0), wantValue: "v1"},
		{name: "between the two writes", at: nanosAt(5 * time.Second), wantValue: "v1"},
		{name: "exactly at the second write", at: nanosAt(10 * time.Second), wantValue: "v2"},
		{name: "just before the delete", at: nanosAt(20*time.Second - time.Nanosecond), wantValue: "v2"},
		{name: "exactly at the delete", at: nanosAt(20 * time.Second), wantErr: ErrKeyNotFound},
		{name: "after the delete", at: nanosAt(time.Hour), wantErr: ErrKeyNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := store.GetAt("user:1", tc.at)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("GetAt error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && got != tc.wantValue {
				t.Errorf("GetAt = %q, want %q", got, tc.wantValue)
			}
		})
	}
}

func TestGetAtUnknownKey(t *testing.T) {
	store, _ := newTestStore(t)

	if _, err := store.GetAt("missing", nanosAt(0)); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("GetAt on unknown key returned %v, want ErrKeyNotFound", err)
	}
}

// A point-in-time read evaluates the TTL at that same point in time, so a key
// that has expired by now is still readable inside its former lifetime.
func TestGetAtEvaluatesTTLAtTheQueriedInstant(t *testing.T) {
	store, clock := newTestStore(t)

	if err := store.SetWithTTL("session:1", "token", time.Minute); err != nil {
		t.Fatalf("SetWithTTL returned %v", err)
	}
	clock.Advance(time.Hour)

	got, err := store.GetAt("session:1", nanosAt(30*time.Second))
	if err != nil {
		t.Fatalf("GetAt inside the lifetime returned %v", err)
	}
	if got != "token" {
		t.Errorf("GetAt inside the lifetime = %q, want %q", got, "token")
	}

	if _, err := store.GetAt("session:1", nanosAt(90*time.Second)); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("GetAt after the lifetime returned %v, want ErrKeyNotFound", err)
	}
}

// Two writes landing on the same wall-clock reading, or a clock that jumped
// backwards, must still produce two separately addressable versions.
func TestGetAtSurvivesAStalledOrRewoundClock(t *testing.T) {
	store, clock := newTestStore(t)

	if err := store.Set("user:1", "v1"); err != nil {
		t.Fatalf("Set v1 returned %v", err)
	}
	// Same reading as v1.
	if err := store.Set("user:1", "v2"); err != nil {
		t.Fatalf("Set v2 returned %v", err)
	}
	// And now the clock jumps backwards.
	clock.Rewind(time.Hour)
	if err := store.Set("user:1", "v3"); err != nil {
		t.Fatalf("Set v3 returned %v", err)
	}

	mustGet(t, store, "user:1", "v3")

	tests := []struct {
		at   int64
		want string
	}{
		{at: nanosAt(0), want: "v1"},
		{at: nanosAt(1), want: "v2"},
		{at: nanosAt(2), want: "v3"},
	}
	for _, tc := range tests {
		got, err := store.GetAt("user:1", tc.at)
		if err != nil {
			t.Fatalf("GetAt(%d) returned %v", tc.at, err)
		}
		if got != tc.want {
			t.Errorf("GetAt(%d) = %q, want %q", tc.at, got, tc.want)
		}
	}
}

// Once the cleaner has dropped versions older than the retention window, a read
// of that era must say so instead of pretending the key did not exist.
func TestGetAtBeyondTheRetentionWindow(t *testing.T) {
	store, clock := newTestStore(t, WithRetention(10*time.Minute))

	if err := store.Set("user:1", "v1"); err != nil {
		t.Fatalf("Set v1 returned %v", err)
	}
	clock.Advance(20 * time.Minute)
	if err := store.Set("user:1", "v2"); err != nil {
		t.Fatalf("Set v2 returned %v", err)
	}
	clock.Advance(40 * time.Minute)
	if err := store.Set("user:1", "v3"); err != nil {
		t.Fatalf("Set v3 returned %v", err)
	}

	store.sweep()

	if _, err := store.GetAt("user:1", nanosAt(time.Minute)); !errors.Is(err, ErrSnapshotExpired) {
		t.Errorf("GetAt beyond the retention window returned %v, want ErrSnapshotExpired", err)
	}
	// v2 was still in effect when the window opened, so it has to survive.
	got, err := store.GetAt("user:1", nanosAt(50*time.Minute))
	if err != nil || got != "v2" {
		t.Errorf("GetAt at the window boundary = (%q, %v), want (\"v2\", nil)", got, err)
	}
	mustGet(t, store, "user:1", "v3")
}
