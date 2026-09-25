package kvstore

// recordState distinguishes a written value from a deletion marker.
//
// Expiry is deliberately *not* a state: it is derived from expiredAt, so a key
// falling out of its TTL never has to mutate the store. That keeps Get and
// ScanPrefix on the read lock only.
type recordState uint8

const (
	stateAlive recordState = iota
	stateTomb
)

// changeRecord is one immutable version of a key. Records are appended to an
// entry in strictly increasing createdAt order and never mutated afterwards,
// which is what lets readers walk them under a read lock.
type changeRecord struct {
	state     recordState
	value     string
	createdAt int64 // unix nanoseconds, strictly increasing within one entry
	expiredAt int64 // unix nanoseconds, exclusive; 0 means "never expires"
}

// visibleAt reports whether this record still holds a readable value at t.
func (r *changeRecord) visibleAt(t int64) bool {
	if r.state != stateAlive {
		return false
	}
	if r.expiredAt == 0 {
		return true
	}
	return t < r.expiredAt
}
