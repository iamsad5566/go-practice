package paysched

import (
	"context"
	"errors"
	"testing"
	"time"
)

var testEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func newTestLimiter(clock Clock, cfg LimiterConfig) *Limiter {
	return NewLimiter(cfg, clock)
}

func TestLimiterConsumesBurstThenRejects(t *testing.T) {
	clock := newFakeClock(testEpoch)
	l := newTestLimiter(clock, LimiterConfig{
		Default: BucketConfig{Capacity: 3, RefillPerSecond: 1},
	})

	for i := 0; i < 3; i++ {
		ok, err := l.Allow("merchant-a", 1)
		if err != nil || !ok {
			t.Fatalf("第 %d 次應通過, got ok=%v err=%v", i+1, ok, err)
		}
	}

	ok, err := l.Allow("merchant-a", 1)
	if err != nil {
		t.Fatalf("超額應以 ok=false 表達而非錯誤, got %v", err)
	}
	if ok {
		t.Fatal("token 耗盡後應拒絕")
	}
}

// Lazy refill：token 只在請求進來時依經過時間補足，沒有背景 ticker。
func TestLimiterLazyRefill(t *testing.T) {
	clock := newFakeClock(testEpoch)
	l := newTestLimiter(clock, LimiterConfig{
		Default: BucketConfig{Capacity: 2, RefillPerSecond: 2}, // 每 500ms 補 1 顆
	})

	mustAllow(t, l, "m", 2)
	mustDeny(t, l, "m", 1)

	clock.Advance(500 * time.Millisecond)
	mustAllow(t, l, "m", 1)
	mustDeny(t, l, "m", 1)

	// 長時間閒置後 token 不得超過容量（防止累積成無限爆發量）。
	clock.Advance(time.Hour)
	mustAllow(t, l, "m", 2)
	mustDeny(t, l, "m", 1)
}

func TestLimiterPerTenantOverride(t *testing.T) {
	clock := newFakeClock(testEpoch)
	l := newTestLimiter(clock, LimiterConfig{
		Default:   BucketConfig{Capacity: 1, RefillPerSecond: 1},
		PerTenant: map[string]BucketConfig{"vip": {Capacity: 5, RefillPerSecond: 1}},
	})

	mustAllow(t, l, "vip", 5)
	mustDeny(t, l, "vip", 1)
	mustAllow(t, l, "normal", 1)
	mustDeny(t, l, "normal", 1)
}

func TestLimiterInvalidCost(t *testing.T) {
	l := newTestLimiter(newFakeClock(testEpoch), LimiterConfig{
		Default: BucketConfig{Capacity: 2, RefillPerSecond: 1},
	})

	if _, err := l.Allow("m", 0); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("cost 0 應回 ErrInvalidRequest, got %v", err)
	}
	// 成本大於容量永遠不可能通過，屬設定錯誤，要明確報錯而非無聲拒絕。
	if _, err := l.Allow("m", 3); !errors.Is(err, ErrCostExceedsCapacity) {
		t.Fatalf("cost 超過容量應回 ErrCostExceedsCapacity, got %v", err)
	}
}

func TestLimiterRefundCapsAtCapacity(t *testing.T) {
	clock := newFakeClock(testEpoch)
	l := newTestLimiter(clock, LimiterConfig{
		Default: BucketConfig{Capacity: 2, RefillPerSecond: 1},
	})

	mustAllow(t, l, "m", 2)
	l.refund("m", 2)
	mustAllow(t, l, "m", 2)
	mustDeny(t, l, "m", 1)

	// 歸還超過容量的部分必須被裁掉。
	l.refund("m", 10)
	mustAllow(t, l, "m", 2)
	mustDeny(t, l, "m", 1)
}

func TestLimiterWaitSucceedsAfterRefill(t *testing.T) {
	clock := newFakeClock(testEpoch)
	l := newTestLimiter(clock, LimiterConfig{
		Default: BucketConfig{Capacity: 1, RefillPerSecond: 1},
	})
	mustAllow(t, l, "m", 1)

	done := make(chan error, 1)
	go func() { done <- l.Wait(context.Background(), "m", 1) }()

	clock.waitForArmedTimers(t, 1)
	clock.Advance(time.Second)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("補足 token 後 Wait 應成功, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait 未在補足 token 後返回")
	}
}

func TestLimiterWaitRespectsContext(t *testing.T) {
	clock := newFakeClock(testEpoch)
	l := newTestLimiter(clock, LimiterConfig{
		Default: BucketConfig{Capacity: 1, RefillPerSecond: 1},
	})
	mustAllow(t, l, "m", 1)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.Wait(ctx, "m", 1) }()

	clock.waitForArmedTimers(t, 1)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ctx 取消應回傳 context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait 未因 ctx 取消而返回")
	}
}

// 雙維度限流：商戶通過但通道被拒時，必須把商戶已扣的 token 歸還，
// 否則被通道擋掉的流量會持續侵蝕商戶額度（token 洩漏）。
func TestGateRefundsMerchantWhenChannelDenies(t *testing.T) {
	clock := newFakeClock(testEpoch)
	merchant := newTestLimiter(clock, LimiterConfig{Default: BucketConfig{Capacity: 5, RefillPerSecond: 1}})
	channel := newTestLimiter(clock, LimiterConfig{Default: BucketConfig{Capacity: 1, RefillPerSecond: 1}})
	g := &gate{merchant: merchant, channel: channel}

	ok, err := g.allow("m", "c", 1)
	if err != nil || !ok {
		t.Fatalf("首次應通過, ok=%v err=%v", ok, err)
	}

	ok, err = g.allow("m", "c", 1)
	if err != nil || ok {
		t.Fatalf("通道額度用盡應被拒, ok=%v err=%v", ok, err)
	}

	// 商戶容量 5，通道擋掉的那次不應扣商戶 token：應還剩 4 次可用。
	for i := 0; i < 4; i++ {
		if ok, err := merchant.Allow("m", 1); err != nil || !ok {
			t.Fatalf("商戶 token 被錯誤扣除, 第 %d 次 ok=%v err=%v", i+1, ok, err)
		}
	}
	mustDeny(t, merchant, "m", 1)
}

func mustAllow(t *testing.T, l *Limiter, tenant string, times int) {
	t.Helper()
	for i := 0; i < times; i++ {
		ok, err := l.Allow(tenant, 1)
		if err != nil || !ok {
			t.Fatalf("tenant %s 第 %d 次應通過, ok=%v err=%v", tenant, i+1, ok, err)
		}
	}
}

func mustDeny(t *testing.T, l *Limiter, tenant string, cost int64) {
	t.Helper()
	ok, err := l.Allow(tenant, cost)
	if err != nil {
		t.Fatalf("tenant %s 應被拒而非錯誤, got %v", tenant, err)
	}
	if ok {
		t.Fatalf("tenant %s 應被拒", tenant)
	}
}
