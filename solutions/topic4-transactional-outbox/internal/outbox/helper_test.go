package outbox

import (
	"testing"
	"time"
)

const testSecret = "test-secret"

// newTestEngine 建出一個時間完全由測試驅動、退避時間無隨機性的引擎。
func newTestEngine(t *testing.T, clock Clock, sender WebhookSender, mutate func(*Config)) *Engine {
	t.Helper()

	secrets := NewMemorySecretStore()
	secrets.Put("merchant-1", []byte(testSecret))
	secrets.Put("merchant-2", []byte(testSecret))

	cfg := Config{
		Sender:      sender,
		Secrets:     secrets,
		Clock:       clock,
		Workers:     1,
		MaxRetries:  2,
		BaseBackoff: time.Second,
		MaxBackoff:  time.Minute,
		// 測試取 jitter 區間的上界，讓退避時間可精確預期。
		Rand: func(n int64) int64 { return n },
	}
	if mutate != nil {
		mutate(&cfg)
	}

	eng, err := New(cfg)
	if err != nil {
		t.Fatalf("New() 失敗: %v", err)
	}
	t.Cleanup(func() { eng.Close() })
	return eng
}

// waitForStatus 輪詢等待事件達到指定狀態，避免以固定 sleep 換取穩定性。
func waitForStatus(t *testing.T, eng *Engine, id EventID, want Status) EventRecord {
	t.Helper()
	var last EventRecord
	if !eventually(func() bool {
		rec, err := eng.GetEventStatus(id)
		if err != nil {
			return false
		}
		last = rec
		return rec.Status == want
	}) {
		t.Fatalf("等待事件 %s 進入狀態 %s 逾時, 當前為 %s (lastError=%q)", id, want, last.Status, last.LastError)
	}
	return last
}

// waitForRetryCount 等待事件累積到指定的重試次數。
//
// 退避期間事件一直停在 RETRYING，因此不能只等狀態——狀態條件會立刻成立，
// 拿到的是上一輪的舊快照。要辨識「又失敗了一次」只能看 RetryCount。
func waitForRetryCount(t *testing.T, eng *Engine, id EventID, want int) EventRecord {
	t.Helper()
	var last EventRecord
	if !eventually(func() bool {
		rec, err := eng.GetEventStatus(id)
		if err != nil {
			return false
		}
		last = rec
		return rec.Status == StatusRetrying && rec.RetryCount == want
	}) {
		t.Fatalf("等待事件 %s 的 RetryCount 達到 %d 逾時, 當前 %d (status=%s)", id, want, last.RetryCount, last.Status)
	}
	return last
}

// waitForAttempts 等待 sender 累積到指定的嘗試次數。
func waitForAttempts(t *testing.T, sender *fakeSender, n int) {
	t.Helper()
	if !eventually(func() bool { return sender.totalAttempts() >= n }) {
		t.Fatalf("等待 %d 次投遞嘗試逾時, 實際 %d 次", n, sender.totalAttempts())
	}
}

func eventually(cond func() bool) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}
