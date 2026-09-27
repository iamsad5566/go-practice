package paysched

import (
	"context"
	"sync"
	"testing"
	"time"
)

// stubExecutor 是可編程的假外部通道。fn 收到的 attempt 從 0 起算。
type stubExecutor struct {
	mu    sync.Mutex
	calls []PaymentRequest
	fn    func(attempt int, req PaymentRequest) (ExecutionResult, error)
}

func (s *stubExecutor) Execute(ctx context.Context, req PaymentRequest) (ExecutionResult, error) {
	s.mu.Lock()
	attempt := len(s.calls)
	s.calls = append(s.calls, req)
	fn := s.fn
	s.mu.Unlock()

	if fn == nil {
		return ExecutionResult{ExternalRef: "ext-ref"}, nil
	}
	return fn(attempt, req)
}

func (s *stubExecutor) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *stubExecutor) call(i int) PaymentRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[i]
}

// newTestEngine 建出一個時間完全由測試驅動、退避時間無隨機性的引擎。
func newTestEngine(t *testing.T, clock Clock, exec PaymentExecutor, mutate func(*Config)) *Engine {
	t.Helper()

	cfg := Config{
		Executor:    exec,
		Clock:       clock,
		Workers:     1,
		MaxRetries:  2,
		BaseBackoff: time.Second,
		MaxBackoff:  time.Minute,
		// 測試取 full jitter 的上界，讓退避時間可精確預期。
		Rand:     func(n int64) int64 { return n },
		Merchant: LimiterConfig{Default: BucketConfig{Capacity: 1000, RefillPerSecond: 1000}},
		Channel:  LimiterConfig{Default: BucketConfig{Capacity: 1000, RefillPerSecond: 1000}},
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

func validRequest(executeAt time.Time) PaymentRequest {
	return PaymentRequest{
		MerchantID: "merchant-1",
		ChannelID:  "fedwire",
		Amount:     10_000,
		Currency:   "USD",
		ExecuteAt:  executeAt,
	}
}

// waitForStatus 輪詢等待付款達到指定狀態，避免以固定 sleep 換取穩定性。
func waitForStatus(t *testing.T, eng *Engine, id ScheduleID, want Status) PaymentRecord {
	t.Helper()
	var last PaymentRecord
	if !eventually(t, func() bool {
		rec, err := eng.GetPaymentStatus(id)
		if err != nil {
			return false
		}
		last = rec
		return rec.Status == want
	}) {
		t.Fatalf("等待狀態 %s 逾時, 當前為 %s (lastError=%q)", want, last.Status, last.LastError)
	}
	return last
}

func eventually(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}
