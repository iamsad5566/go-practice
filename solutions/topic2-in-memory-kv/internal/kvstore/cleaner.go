package kvstore

import "time"

// startCleaner launches the single background goroutine that reclaims history
// older than the retention window.
func (s *Store) startCleaner() {
	s.wg.Add(1)

	go func() {
		defer s.wg.Done()

		// A ticker is reused across iterations; time.After would allocate a
		// fresh timer on every loop and leave them pending until they fire.
		ticker := time.NewTicker(s.cleanInterval)
		defer ticker.Stop()

		for {
			select {
			case <-s.done:
				return
			case <-ticker.C:
				s.sweep()
			}
		}
	}()
}

// sweep reclaims every shard in turn, taking one shard lock at a time so that
// cleaning never blocks the whole store.
func (s *Store) sweep() {
	cutoff := s.nowNanos() - s.retention.Nanoseconds()
	for _, shard := range s.shards {
		shard.sweep(cutoff)
	}
}
