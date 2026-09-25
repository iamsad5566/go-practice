package kvstore

import (
	"fmt"
	"testing"
	"time"
)

// benchStore returns a populated store plus its keys, pre-formatted so that the
// benchmark loop measures the store rather than fmt.
func benchStore(b *testing.B, count int) (*Store, []string) {
	b.Helper()

	store := New(WithCleanInterval(time.Hour))
	b.Cleanup(func() { store.Close() })

	keys := make([]string, count)
	for i := range count {
		keys[i] = fmt.Sprintf("user:%d", i)
		if err := store.Set(keys[i], "value"); err != nil {
			b.Fatalf("Set returned %v", err)
		}
	}
	return store, keys
}

func BenchmarkGetParallel(b *testing.B) {
	store, keys := benchStore(b, 10_000)
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			sinkValue, sinkErr = store.Get(keys[i%len(keys)])
			i++
		}
	})
}

func BenchmarkGetAt(b *testing.B) {
	store, _ := benchStore(b, 1)
	for range 1_000 {
		if err := store.Set("user:0", "value"); err != nil {
			b.Fatalf("Set returned %v", err)
		}
	}
	at := time.Now().UnixNano()
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		sinkValue, sinkErr = store.GetAt("user:0", at)
	}
}

func BenchmarkSetParallel(b *testing.B) {
	store, keys := benchStore(b, 10_000)
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			sinkErr = store.Set(keys[i%len(keys)], "value")
			i++
		}
	})
}
