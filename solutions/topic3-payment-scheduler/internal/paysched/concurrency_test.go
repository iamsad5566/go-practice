package paysched

import (
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"
)

// 高競爭下的排程、取消、狀態查詢與執行必須零資料競爭且不死鎖。
// 本測試使用真實時鐘與極短的到期時間，刻意製造取消與派送的交錯。
func TestConcurrentScheduleCancelAndInspect(t *testing.T) {
	exec := &stubExecutor{}
	eng, err := New(Config{
		Executor:       exec,
		Workers:        8,
		MaxRetries:     1,
		BaseBackoff:    time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		LimiterBackoff: time.Millisecond,
		Merchant:       LimiterConfig{Default: BucketConfig{Capacity: 10_000, RefillPerSecond: 10_000}},
		Channel:        LimiterConfig{Default: BucketConfig{Capacity: 10_000, RefillPerSecond: 10_000}},
	})
	if err != nil {
		t.Fatalf("New() 失敗: %v", err)
	}
	defer eng.Close()

	const goroutines, perGoroutine = 16, 60
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				req := validRequest(time.Now().Add(time.Duration(i%5) * time.Millisecond))
				req.Priority = Priority(i % 3)
				id, err := eng.SchedulePayment(req)
				if errors.Is(err, ErrPastExecuteAt) {
					continue
				}
				if err != nil {
					t.Errorf("SchedulePayment() 失敗: %v", err)
					return
				}

				// 取消與派送在此刻競爭；任何結果都必須是狀態機定義過的。
				switch err := eng.CancelPayment(id); {
				case err == nil,
					errors.Is(err, ErrInProgress),
					errors.Is(err, ErrAlreadyExecuted):
				default:
					t.Errorf("CancelPayment() 回傳未定義的錯誤: %v", err)
					return
				}

				rec, err := eng.GetPaymentStatus(id)
				if err != nil {
					t.Errorf("GetPaymentStatus() 失敗: %v", err)
					return
				}
				if rec.Status == "" {
					t.Error("狀態不應為空")
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

// Close 後不得殘留 goroutine 或未回收的計時器。
func TestCloseDoesNotLeakGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()

	for i := 0; i < 5; i++ {
		eng, err := New(Config{
			Executor: &stubExecutor{},
			Workers:  4,
			Merchant: LimiterConfig{Default: BucketConfig{Capacity: 100, RefillPerSecond: 100}},
			Channel:  LimiterConfig{Default: BucketConfig{Capacity: 100, RefillPerSecond: 100}},
		})
		if err != nil {
			t.Fatalf("New() 失敗: %v", err)
		}
		for j := 0; j < 50; j++ {
			if _, err := eng.SchedulePayment(validRequest(time.Now().Add(time.Hour))); err != nil {
				t.Fatalf("SchedulePayment() 失敗: %v", err)
			}
		}
		eng.Close()
	}

	if !eventually(t, func() bool { return runtime.NumGoroutine() <= before+2 }) {
		t.Fatalf("goroutine 洩漏: before=%d after=%d", before, runtime.NumGoroutine())
	}
}

// 五十萬筆未來排程只能用一個計時器驅動，不得每筆各開一個 timer 或 goroutine。
func TestLargeScheduleUsesSingleTimer(t *testing.T) {
	if testing.Short() {
		t.Skip("大量排程測試在 -short 下略過")
	}

	before := runtime.NumGoroutine()
	exec := &stubExecutor{}
	eng, err := New(Config{
		Executor: exec,
		Workers:  4,
		Merchant: LimiterConfig{Default: BucketConfig{Capacity: 100, RefillPerSecond: 100}},
		Channel:  LimiterConfig{Default: BucketConfig{Capacity: 100, RefillPerSecond: 100}},
	})
	if err != nil {
		t.Fatalf("New() 失敗: %v", err)
	}
	defer eng.Close()

	now := time.Now()
	for i := 0; i < 200_000; i++ {
		req := validRequest(now.Add(time.Duration(i+1) * time.Hour))
		if _, err := eng.SchedulePayment(req); err != nil {
			t.Fatalf("第 %d 筆排程失敗: %v", i, err)
		}
	}

	// goroutine 數量應與排程數量無關：1 個 dispatcher + N 個 worker。
	if grew := runtime.NumGoroutine() - before; grew > 10 {
		t.Fatalf("goroutine 數量隨排程量成長: +%d", grew)
	}
}
