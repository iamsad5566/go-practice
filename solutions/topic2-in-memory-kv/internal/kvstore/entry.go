package kvstore

import "sort"

// entry is the change history of a single key, ordered by createdAt.
//
// The newest version sits at the tail, so a current read is O(1) and a
// point-in-time read is a binary search, O(log n).
type entry struct {
	records []*changeRecord

	// trimmed records whether the cleaner has already dropped versions older
	// than the retention window. It is what lets a point-in-time read tell
	// "this key did not exist yet" apart from "that answer is gone".
	trimmed bool
}

// latest returns the newest version, or nil for an entry with no history.
func (e *entry) latest() *changeRecord {
	if len(e.records) == 0 {
		return nil
	}
	return e.records[len(e.records)-1]
}

// append adds a new newest version. The caller is responsible for building it
// with a timestamp obtained from nextTimestamp.
func (e *entry) append(record *changeRecord) {
	e.records = append(e.records, record)
}

// nextTimestamp turns a wall-clock reading into a timestamp that is strictly
// greater than every existing version.
//
// A stalled or backwards-running clock would otherwise break the ordering the
// binary search relies on, so it is bumped by a nanosecond instead.
func (e *entry) nextTimestamp(now int64) int64 {
	latest := e.latest()
	if latest == nil || now > latest.createdAt {
		return now
	}
	return latest.createdAt + 1
}

// recordAt returns the version that was in effect at t. The returned record may
// well be invisible at t (deleted or expired); judging that is the caller's job.
func (e *entry) recordAt(t int64) (*changeRecord, error) {
	// index of the first version written strictly after t
	next := sort.Search(len(e.records), func(i int) bool {
		return e.records[i].createdAt > t
	})
	if next > 0 {
		return e.records[next-1], nil
	}
	if e.trimmed {
		return nil, ErrSnapshotExpired
	}
	return nil, ErrKeyNotFound
}

// trim drops the versions that no query at or after cutoff can reach.
//
// The version *in effect at* the cutoff is kept even when it is much older than
// the window, otherwise a key written once and never touched again would
// silently disappear.
func (e *entry) trim(cutoff int64) {
	keep := e.firstRetainedIndex(cutoff)
	if keep <= 0 {
		return
	}

	// A re-slice would leave the dropped *changeRecord values reachable from
	// the original backing array, so copy into a right-sized slice and let the
	// old array — and everything it points at — become garbage.
	retained := make([]*changeRecord, len(e.records)-keep)
	copy(retained, e.records[keep:])
	e.records = retained
	e.trimmed = true
}

// firstRetainedIndex is the index of the oldest version still reachable from
// the retention window.
func (e *entry) firstRetainedIndex(cutoff int64) int {
	oldest := sort.Search(len(e.records), func(i int) bool {
		return e.records[i].createdAt >= cutoff
	})
	// oldest-1 is the version in effect at the cutoff. When every version
	// predates the cutoff this lands on the newest one, which is exactly the
	// single version worth keeping.
	return max(oldest-1, 0)
}

// droppableAt reports whether the whole key can leave the store: nothing is
// readable at the cutoff any more and no history is left to travel back to.
func (e *entry) droppableAt(cutoff int64) bool {
	if len(e.records) != 1 {
		return false
	}
	only := e.records[0]
	return only.createdAt <= cutoff && !only.visibleAt(cutoff)
}
