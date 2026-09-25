package kvstore

import "errors"

var (
	// ErrKeyNotFound is returned when a key has never been written, was
	// deleted, or had already expired at the queried point in time.
	ErrKeyNotFound = errors.New("kvstore: key not found")

	// ErrInvalidTTL is returned by SetWithTTL when the caller passes a
	// non-positive duration. A key that should never expire is written with
	// Set instead.
	ErrInvalidTTL = errors.New("kvstore: ttl must be positive")

	// ErrSnapshotExpired is returned by GetAt when the requested point in
	// time predates the retention window, so the answer is no longer
	// recoverable. It is deliberately distinct from ErrKeyNotFound: the key
	// may well have held a value back then, the store simply cannot tell.
	ErrSnapshotExpired = errors.New("kvstore: snapshot predates the retention window")

	// ErrStoreClosed is returned by every operation issued after Close.
	ErrStoreClosed = errors.New("kvstore: store is closed")
)
