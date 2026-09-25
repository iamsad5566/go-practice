package kvstore

import "testing"

func TestChangeRecordVisibleAt(t *testing.T) {
	const created = 1_000

	tests := []struct {
		name   string
		record changeRecord
		at     int64
		want   bool
	}{
		{
			name:   "permanent record is visible at creation time",
			record: changeRecord{state: stateAlive, createdAt: created},
			at:     created,
			want:   true,
		},
		{
			name:   "permanent record stays visible far in the future",
			record: changeRecord{state: stateAlive, createdAt: created},
			at:     created + 1_000_000,
			want:   true,
		},
		{
			name:   "record with ttl is visible strictly before its deadline",
			record: changeRecord{state: stateAlive, createdAt: created, expiredAt: created + 100},
			at:     created + 99,
			want:   true,
		},
		{
			name:   "expiredAt is exclusive: the deadline itself is already expired",
			record: changeRecord{state: stateAlive, createdAt: created, expiredAt: created + 100},
			at:     created + 100,
			want:   false,
		},
		{
			name:   "tombstone is never visible",
			record: changeRecord{state: stateTomb, createdAt: created},
			at:     created,
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.record.visibleAt(tc.at); got != tc.want {
				t.Errorf("visibleAt(%d) = %v, want %v", tc.at, got, tc.want)
			}
		})
	}
}
