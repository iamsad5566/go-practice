package outbox

import (
	"fmt"
	"testing"
)

func benchEngine(b *testing.B, workers int) (*Engine, *fakeSender) {
	b.Helper()

	sender := newFakeSender()
	secrets := NewMemorySecretStore()
	secrets.Put("merchant-1", []byte(testSecret))

	eng, err := New(Config{
		Sender:                 sender,
		Secrets:                secrets,
		Workers:                workers,
		MaxInflightPerMerchant: workers,
	})
	if err != nil {
		b.Fatalf("New() 失敗: %v", err)
	}
	b.Cleanup(eng.Close)
	return eng, sender
}

// 事件發布是業務交易的出口，成本必須穩定：
// 只有一次 map 寫入與一次 O(log n) 的 heap push，不隨既有事件量爆炸。
func BenchmarkPublishEvent(b *testing.B) {
	eng, _ := benchEngine(b, 8)
	ev := validEvent()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ev.EventID = EventID(fmt.Sprintf("evt-%d", i))
		if _, err := eng.PublishEvent(ev); err != nil {
			b.Fatal(err)
		}
	}
}

// 多個 goroutine 同時發布時不應互相卡死。
func BenchmarkPublishEventParallel(b *testing.B) {
	eng, _ := benchEngine(b, 8)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		local := 0
		for pb.Next() {
			local++
			ev := validEvent()
			ev.EventID = EventID(fmt.Sprintf("evt-%p-%d", pb, local))
			if _, err := eng.PublishEvent(ev); err != nil {
				b.Error(err)
				return
			}
		}
	})
}

// 狀態查詢只取讀鎖後立即釋放，應能高度並行。
func BenchmarkGetEventStatusParallel(b *testing.B) {
	eng, _ := benchEngine(b, 8)

	const total = 1000
	ids := make([]EventID, 0, total)
	for i := 0; i < total; i++ {
		ev := validEvent()
		ev.EventID = EventID(fmt.Sprintf("evt-%d", i))
		id, err := eng.PublishEvent(ev)
		if err != nil {
			b.Fatal(err)
		}
		ids = append(ids, id)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if _, err := eng.GetEventStatus(ids[i%total]); err != nil {
				b.Error(err)
				return
			}
			i++
		}
	})
}

// 簽章在每次投遞（含每次重試）都會重算，是投遞熱路徑上唯一的密碼學成本。
func BenchmarkBuildHeaders(b *testing.B) {
	secrets := NewMemorySecretStore()
	secrets.Put("merchant-1", []byte(testSecret))
	s := &signer{secrets: secrets}

	clock := NewRealClock()
	rec := newEntry(validEvent(), "evt-1", 1, clock.Now()).snapshot()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := s.buildHeaders(rec, clock.Now()); err != nil {
			b.Fatal(err)
		}
	}
}
