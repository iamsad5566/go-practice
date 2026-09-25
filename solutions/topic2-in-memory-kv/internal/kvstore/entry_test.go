package kvstore

import (
	"errors"
	"testing"
)

// newEntry builds an entry from already ordered records, bypassing the
// timestamp normalisation, so that tests can describe an exact history.
func newEntry(records ...*changeRecord) *entry {
	return &entry{records: records}
}

func alive(value string, createdAt int64) *changeRecord {
	return &changeRecord{state: stateAlive, value: value, createdAt: createdAt}
}

func aliveUntil(value string, createdAt, expiredAt int64) *changeRecord {
	return &changeRecord{state: stateAlive, value: value, createdAt: createdAt, expiredAt: expiredAt}
}

func tomb(createdAt int64) *changeRecord {
	return &changeRecord{state: stateTomb, createdAt: createdAt}
}

func TestEntryNextTimestampIsStrictlyIncreasing(t *testing.T) {
	tests := []struct {
		name  string
		entry *entry
		now   int64
		want  int64
	}{
		{
			name:  "empty history uses the wall clock as is",
			entry: newEntry(),
			now:   100,
			want:  100,
		},
		{
			name:  "a clock that moved forward is used as is",
			entry: newEntry(alive("v1", 100)),
			now:   101,
			want:  101,
		},
		{
			name:  "a stalled clock is bumped by one nanosecond",
			entry: newEntry(alive("v1", 100)),
			now:   100,
			want:  101,
		},
		{
			name:  "a clock that jumped backwards is bumped past the latest record",
			entry: newEntry(alive("v1", 100)),
			now:   40,
			want:  101,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.entry.nextTimestamp(tc.now); got != tc.want {
				t.Errorf("nextTimestamp(%d) = %d, want %d", tc.now, got, tc.want)
			}
		})
	}
}

func TestEntryRecordAt(t *testing.T) {
	history := newEntry(alive("v1", 100), alive("v2", 200), tomb(300))

	tests := []struct {
		name      string
		entry     *entry
		at        int64
		wantValue string
		wantErr   error
	}{
		{name: "exactly on a write", entry: history, at: 100, wantValue: "v1"},
		{name: "between two writes", entry: history, at: 150, wantValue: "v1"},
		{name: "on the newer write", entry: history, at: 200, wantValue: "v2"},
		{name: "after the last write", entry: history, at: 9_000, wantValue: ""},
		{name: "before the key existed", entry: history, at: 99, wantErr: ErrKeyNotFound},
		{
			name:    "before a trimmed history reports the snapshot as gone",
			entry:   &entry{records: []*changeRecord{alive("v2", 200)}, trimmed: true},
			at:      150,
			wantErr: ErrSnapshotExpired,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.entry.recordAt(tc.at)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("recordAt(%d) error = %v, want %v", tc.at, err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if got.value != tc.wantValue {
				t.Errorf("recordAt(%d) value = %q, want %q", tc.at, got.value, tc.wantValue)
			}
		})
	}
}

func TestEntryTrimKeepsTheRecordInEffectAtCutoff(t *testing.T) {
	tests := []struct {
		name            string
		entry           *entry
		cutoff          int64
		wantCreatedAt   []int64
		wantTrimmedFlag bool
	}{
		{
			name:            "history entirely inside the window is untouched",
			entry:           newEntry(alive("v1", 100), alive("v2", 200)),
			cutoff:          50,
			wantCreatedAt:   []int64{100, 200},
			wantTrimmedFlag: false,
		},
		{
			name:            "the version in effect at the cutoff survives",
			entry:           newEntry(alive("v1", 100), alive("v2", 200), alive("v3", 300)),
			cutoff:          250,
			wantCreatedAt:   []int64{200, 300},
			wantTrimmedFlag: true,
		},
		{
			name:            "a key untouched for a long time keeps its only live version",
			entry:           newEntry(alive("v1", 100)),
			cutoff:          9_000,
			wantCreatedAt:   []int64{100},
			wantTrimmedFlag: false,
		},
		{
			name:            "a long stale history collapses to its newest version",
			entry:           newEntry(alive("v1", 100), alive("v2", 200), alive("v3", 300)),
			cutoff:          9_000,
			wantCreatedAt:   []int64{300},
			wantTrimmedFlag: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.entry.trim(tc.cutoff)

			got := make([]int64, 0, len(tc.entry.records))
			for _, r := range tc.entry.records {
				got = append(got, r.createdAt)
			}
			if len(got) != len(tc.wantCreatedAt) {
				t.Fatalf("after trim records = %v, want %v", got, tc.wantCreatedAt)
			}
			for i := range got {
				if got[i] != tc.wantCreatedAt[i] {
					t.Fatalf("after trim records = %v, want %v", got, tc.wantCreatedAt)
				}
			}
			if tc.entry.trimmed != tc.wantTrimmedFlag {
				t.Errorf("trimmed = %v, want %v", tc.entry.trimmed, tc.wantTrimmedFlag)
			}
		})
	}
}

// Re-slicing would keep the dropped *changeRecord values reachable through the
// original backing array, so trim must hand back a right-sized copy instead.
func TestEntryTrimReleasesTheOldBackingArray(t *testing.T) {
	e := newEntry(alive("v1", 100), alive("v2", 200), alive("v3", 300), alive("v4", 400))

	e.trim(9_000)

	if got := cap(e.records); got != len(e.records) {
		t.Errorf("cap(records) = %d, want %d: the dropped records are still reachable", got, len(e.records))
	}
}

func TestEntryDroppableAt(t *testing.T) {
	tests := []struct {
		name   string
		entry  *entry
		cutoff int64
		want   bool
	}{
		{
			name:   "a live key is never droppable",
			entry:  newEntry(alive("v1", 100)),
			cutoff: 9_000,
			want:   false,
		},
		{
			name:   "a key deleted before the cutoff is droppable",
			entry:  newEntry(tomb(100)),
			cutoff: 9_000,
			want:   true,
		},
		{
			name:   "a key expired before the cutoff is droppable",
			entry:  newEntry(aliveUntil("v1", 100, 200)),
			cutoff: 9_000,
			want:   true,
		},
		{
			name:   "a key deleted after the cutoff is still queryable inside the window",
			entry:  newEntry(tomb(9_500)),
			cutoff: 9_000,
			want:   false,
		},
		{
			name:   "a history with several versions is never droppable as a whole",
			entry:  newEntry(alive("v1", 100), tomb(200)),
			cutoff: 9_000,
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.entry.droppableAt(tc.cutoff); got != tc.want {
				t.Errorf("droppableAt(%d) = %v, want %v", tc.cutoff, got, tc.want)
			}
		})
	}
}
