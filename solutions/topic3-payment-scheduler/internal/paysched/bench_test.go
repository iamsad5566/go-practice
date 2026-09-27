package paysched

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type noopExecutor struct{}

func (noopExecutor) Execute(context.Context, PaymentRequest) (ExecutionResult, error) {
	return ExecutionResult{ExternalRef: "ext-ref"}, nil
}

func benchEngine(b *testing.B) *Engine {
	b.Helper()
	eng, err := New(Config{
		Executor: noopExecutor{},
		Workers:  8,
		Merchant: LimiterConfig{Default: BucketConfig{Capacity: 1_000_000, RefillPerSecond: 1_000_000}},
		Channel:  LimiterConfig{Default: BucketConfig{Capacity: 1_000_000, RefillPerSecond: 1_000_000}},
	})
	if err != nil {
		b.Fatalf("New() 失敗: %v", err)
	}
	b.Cleanup(eng.Close)
	return eng
}

// 排程遠期付款的成本應為 O(log n)，且不隨既有排程量爆炸。
func BenchmarkSchedulePayment(b *testing.B) {
	eng := benchEngine(b)
	req := validRequestForBench()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req.ExecuteAt = time.Now().Add(time.Duration(i+1) * time.Hour)
		if _, err := eng.SchedulePayment(req); err != nil {
			b.Fatal(err)
		}
	}
}

// 狀態查詢只取讀鎖後立即釋放，應能高度並行。
func BenchmarkGetPaymentStatusParallel(b *testing.B) {
	eng := benchEngine(b)
	ids := make([]ScheduleID, 0, 1000)
	for i := 0; i < 1000; i++ {
		req := validRequestForBench()
		req.ExecuteAt = time.Now().Add(time.Hour)
		id, err := eng.SchedulePayment(req)
		if err != nil {
			b.Fatal(err)
		}
		ids = append(ids, id)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if _, err := eng.GetPaymentStatus(ids[i%len(ids)]); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}

// 不同租戶各有獨立的 bucket 鎖，理想上應零競爭。
func BenchmarkLimiterAllowDistinctTenants(b *testing.B) {
	l := NewLimiter(LimiterConfig{
		Default: BucketConfig{Capacity: 1_000_000, RefillPerSecond: 1_000_000},
	}, NewRealClock())

	tenants := make([]string, 64)
	for i := range tenants {
		tenants[i] = fmt.Sprintf("merchant-%d", i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if _, err := l.Allow(tenants[i%len(tenants)], 1); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}

// 同一租戶的熱點 bucket：衡量單一 bucket 鎖的競爭成本。
func BenchmarkLimiterAllowHotTenant(b *testing.B) {
	l := NewLimiter(LimiterConfig{
		Default: BucketConfig{Capacity: 1_000_000, RefillPerSecond: 1_000_000},
	}, NewRealClock())

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := l.Allow("hot-merchant", 1); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func validRequestForBench() PaymentRequest {
	return PaymentRequest{
		MerchantID: "merchant-1",
		ChannelID:  "fedwire",
		Amount:     10_000,
		Currency:   "USD",
		ExecuteAt:  time.Now().Add(time.Hour),
	}
}
