// Package kvstore implements a concurrent in-memory key-value store with TTL
// expiry, point-in-time (MVCC) reads over a bounded retention window, and
// prefix scans.
//
// The key space is split across independently locked shards, and each key keeps
// an append-only history of versions. Reads never mutate the store, so the read
// path only ever takes a read lock.
package kvstore

import (
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultShardCount    = 64
	defaultRetention     = 10 * time.Minute
	defaultCleanInterval = 30 * time.Second
)

// Store is safe for concurrent use by multiple goroutines. The zero value is
// not usable; build one with New and release it with Close.
type Store struct {
	shards []*shard
	mask   uint64

	now           func() time.Time
	retention     time.Duration
	cleanInterval time.Duration

	// closed is read on every call, so it is kept outside the shard locks.
	closed    atomic.Bool
	closeOnce sync.Once
	done      chan struct{}
	wg        sync.WaitGroup
}

type config struct {
	shardCount    int
	retention     time.Duration
	cleanInterval time.Duration
	now           func() time.Time
}

// Option customises a Store at construction time.
type Option func(*config)

// WithShardCount sets how many independently locked shards the key space is
// split into. The count is rounded up to a power of two; non-positive values
// are ignored.
func WithShardCount(count int) Option {
	return func(c *config) {
		if count <= 0 {
			return
		}
		c.shardCount = count
	}
}

// WithRetention sets how far back point-in-time reads stay answerable.
// Non-positive values are ignored.
func WithRetention(d time.Duration) Option {
	return func(c *config) {
		if d <= 0 {
			return
		}
		c.retention = d
	}
}

// WithCleanInterval sets how often the background cleaner runs. Non-positive
// values are ignored.
func WithCleanInterval(d time.Duration) Option {
	return func(c *config) {
		if d <= 0 {
			return
		}
		c.cleanInterval = d
	}
}

// WithClock overrides the time source, which makes TTL and history behaviour
// testable without sleeping.
func WithClock(now func() time.Time) Option {
	return func(c *config) {
		if now == nil {
			return
		}
		c.now = now
	}
}

// New builds a Store and starts its background cleaner. The caller must call
// Close to stop that goroutine.
func New(opts ...Option) *Store {
	cfg := config{
		shardCount:    defaultShardCount,
		retention:     defaultRetention,
		cleanInterval: defaultCleanInterval,
		now:           time.Now,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	shardCount := roundUpToPowerOfTwo(cfg.shardCount)
	store := &Store{
		shards:        make([]*shard, shardCount),
		mask:          uint64(shardCount - 1),
		now:           cfg.now,
		retention:     cfg.retention,
		cleanInterval: cfg.cleanInterval,
		done:          make(chan struct{}),
	}
	for i := range store.shards {
		store.shards[i] = newShard()
	}
	store.startCleaner()

	return store
}

// Set writes a value that never expires, replacing whatever the key held.
func (s *Store) Set(key, value string) error {
	if s.closed.Load() {
		return ErrStoreClosed
	}

	s.shardFor(key).write(key, value, s.nowNanos(), 0)
	return nil
}

// SetWithTTL writes a value that stops being readable once ttl has elapsed.
// A key that should never expire is written with Set, so ttl must be positive.
func (s *Store) SetWithTTL(key, value string, ttl time.Duration) error {
	if s.closed.Load() {
		return ErrStoreClosed
	}
	if ttl <= 0 {
		return ErrInvalidTTL
	}

	s.shardFor(key).write(key, value, s.nowNanos(), ttl)
	return nil
}

// Get returns the current value of key, or ErrKeyNotFound if it was never
// written, has been deleted, or has expired.
func (s *Store) Get(key string) (string, error) {
	if s.closed.Load() {
		return "", ErrStoreClosed
	}

	return s.shardFor(key).read(key, s.nowNanos())
}

// GetAt returns the value key held at the given unix-nanosecond timestamp.
//
// It reports ErrKeyNotFound when the key did not exist, was deleted or had
// expired at that instant, and ErrSnapshotExpired when the answer once existed
// but has already been reclaimed by the retention window.
func (s *Store) GetAt(key string, timestamp int64) (string, error) {
	if s.closed.Load() {
		return "", ErrStoreClosed
	}

	return s.shardFor(key).readAt(key, timestamp)
}

// Delete marks key as deleted. It reports ErrKeyNotFound if the key is not
// currently readable, so the caller can tell a real deletion from a no-op.
func (s *Store) Delete(key string) error {
	if s.closed.Load() {
		return ErrStoreClosed
	}

	return s.shardFor(key).remove(key, s.nowNanos())
}

// ScanPrefix returns every currently visible key that starts with prefix.
//
// Shards are locked one at a time rather than all at once, which keeps writes
// to the rest of the store flowing. The result is therefore consistent within
// each shard but only weakly consistent across the store as a whole.
func (s *Store) ScanPrefix(prefix string) (map[string]string, error) {
	if s.closed.Load() {
		return nil, ErrStoreClosed
	}

	now := s.nowNanos()
	matches := make(map[string]string)
	for _, shard := range s.shards {
		shard.collectPrefix(prefix, now, matches)
	}
	return matches, nil
}

// Close stops the background cleaner and rejects all later operations. It waits
// for the cleaner to return, so no goroutine outlives the store, and it is safe
// to call more than once.
func (s *Store) Close() error {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		close(s.done)
		s.wg.Wait()
	})
	return nil
}

func (s *Store) shardFor(key string) *shard {
	return s.shards[hashKey(key)&s.mask]
}

func (s *Store) nowNanos() int64 {
	return s.now().UnixNano()
}

// hashKey is FNV-1a, inlined over the string so that hashing a key allocates
// nothing on the hot path.
func hashKey(key string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)

	hash := uint64(offset64)
	for i := 0; i < len(key); i++ {
		hash ^= uint64(key[i])
		hash *= prime64
	}
	return hash
}

// roundUpToPowerOfTwo lets shard lookup use a bit mask instead of a modulo.
func roundUpToPowerOfTwo(n int) int {
	rounded := 1
	for rounded < n {
		rounded <<= 1
	}
	return rounded
}
