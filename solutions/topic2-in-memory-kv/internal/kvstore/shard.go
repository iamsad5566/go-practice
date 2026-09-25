package kvstore

import (
	"strings"
	"sync"
	"time"
)

// shrinkRatio is how far the live key count has to fall below the shard's
// high-water mark before its map is rebuilt. See shard.sweep.
const shrinkRatio = 4

// shard is one independently locked slice of the key space. Keys are spread
// across shards by hash, so unrelated writes rarely contend on the same mutex.
type shard struct {
	mu    sync.RWMutex
	items map[string]*entry

	// peak is the largest number of keys this shard has held since its map was
	// last rebuilt. Go never shrinks a map's bucket array on its own, so this
	// is what tells the cleaner when the array has become mostly empty.
	peak int
}

func newShard() *shard {
	return &shard{items: make(map[string]*entry)}
}

// write appends a new version of key. A ttl of 0 means the value never expires.
func (s *shard) write(key, value string, now int64, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	target, ok := s.items[key]
	if !ok {
		target = &entry{}
		s.items[key] = target
		s.peak = max(s.peak, len(s.items))
	}

	createdAt := target.nextTimestamp(now)
	record := &changeRecord{state: stateAlive, value: value, createdAt: createdAt}
	if ttl > 0 {
		record.expiredAt = createdAt + ttl.Nanoseconds()
	}
	target.append(record)
}

// read returns the value that is current as of now.
func (s *shard) read(key string, now int64) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	target, ok := s.items[key]
	if !ok {
		return "", ErrKeyNotFound
	}

	latest := target.latest()
	if latest == nil || !latest.visibleAt(now) {
		return "", ErrKeyNotFound
	}
	return latest.value, nil
}

// readAt returns the value that was current at the given instant.
func (s *shard) readAt(key string, at int64) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	target, ok := s.items[key]
	if !ok {
		return "", ErrKeyNotFound
	}

	record, err := target.recordAt(at)
	if err != nil {
		return "", err
	}
	if !record.visibleAt(at) {
		return "", ErrKeyNotFound
	}
	return record.value, nil
}

// remove appends a tombstone. The key's history is left in place so that
// point-in-time reads can still see what it used to hold.
func (s *shard) remove(key string, now int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	target, ok := s.items[key]
	if !ok {
		return ErrKeyNotFound
	}

	latest := target.latest()
	if latest == nil || !latest.visibleAt(now) {
		return ErrKeyNotFound
	}
	target.append(&changeRecord{state: stateTomb, createdAt: target.nextTimestamp(now)})
	return nil
}

// collectPrefix copies every currently visible match into dst. Only this
// shard's lock is taken, so the scan never freezes the whole store.
func (s *shard) collectPrefix(prefix string, now int64, dst map[string]string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for key, target := range s.items {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		latest := target.latest()
		if latest == nil || !latest.visibleAt(now) {
			continue
		}
		// Copying the string value keeps every pointer into the store behind
		// the lock: the caller can never reach a live record.
		dst[key] = latest.value
	}
}

// sweep drops the history no query inside the retention window can reach, and
// the keys that have nothing left worth keeping.
func (s *shard) sweep(cutoff int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, target := range s.items {
		target.trim(cutoff)
		if target.droppableAt(cutoff) {
			delete(s.items, key)
		}
	}
	s.shrink()
}

// shrink rebuilds the map once most of its buckets have gone empty. Deleting
// from a Go map frees the values but never the bucket array, so a shard that
// once absorbed a burst of keys would otherwise hold that memory for good.
func (s *shard) shrink() {
	if len(s.items)*shrinkRatio >= s.peak {
		s.peak = max(s.peak, len(s.items))
		return
	}

	compacted := make(map[string]*entry, len(s.items))
	for key, target := range s.items {
		compacted[key] = target
	}
	s.items = compacted
	s.peak = len(s.items)
}
