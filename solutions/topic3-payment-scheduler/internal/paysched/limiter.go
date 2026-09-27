package paysched

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// BucketConfig 是單一租戶的 token bucket 參數。
type BucketConfig struct {
	// Capacity 是最大爆發量（桶容量）。
	Capacity int64
	// RefillPerSecond 是每秒補充的 token 數，可為小數。
	RefillPerSecond float64
}

func (c BucketConfig) valid() error {
	if c.Capacity <= 0 {
		return fmt.Errorf("%w: bucket capacity must be positive", ErrInvalidRequest)
	}
	if c.RefillPerSecond <= 0 {
		return fmt.Errorf("%w: bucket refill rate must be positive", ErrInvalidRequest)
	}
	return nil
}

// LimiterConfig 描述一個限流維度的配額：預設值加上個別租戶的覆寫。
type LimiterConfig struct {
	Default   BucketConfig
	PerTenant map[string]BucketConfig
}

// Limiter 是單一維度（商戶或通道）的多租戶 token bucket 限流器。
//
// 採 Lazy Refill：token 在每次請求進來時依「距上次操作的經過時間 × 補充速率」補足，
// 完全不需要背景 ticker——數萬個租戶各開一個 goroutine 會白燒 CPU。
//
// 鎖策略：外層 RWMutex 只保護租戶到 bucket 的對照表（查找為主，多為讀），
// token 的加減則由各 bucket 自己的 Mutex 保護，因此不同租戶之間零競爭。
type Limiter struct {
	cfg   LimiterConfig
	clock Clock

	mu      sync.RWMutex
	buckets map[string]*bucket
}

// NewLimiter 建立一個限流維度。
func NewLimiter(cfg LimiterConfig, clock Clock) *Limiter {
	if clock == nil {
		clock = NewRealClock()
	}
	return &Limiter{
		cfg:     cfg,
		clock:   clock,
		buckets: make(map[string]*bucket),
	}
}

// Allow 嘗試為 tenantID 扣除 cost 個 token，額度不足時立即回報 false（fail-fast）。
// 「額度不足」以 ok=false 表達；只有設定或參數錯誤才回傳 error。
func (l *Limiter) Allow(tenantID string, cost int64) (bool, error) {
	b, err := l.bucket(tenantID)
	if err != nil {
		return false, err
	}
	if err := b.validateCost(cost); err != nil {
		return false, err
	}
	return b.take(cost, l.clock.Now()), nil
}

// Wait 阻塞到取得 token 或 ctx 結束為止。
// 等待時長由「還缺多少 token ÷ 補充速率」精算，而不是固定間隔輪詢。
func (l *Limiter) Wait(ctx context.Context, tenantID string, cost int64) error {
	b, err := l.bucket(tenantID)
	if err != nil {
		return err
	}
	if err := b.validateCost(cost); err != nil {
		return err
	}

	var timer Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		now := l.clock.Now()
		if b.take(cost, now) {
			return nil
		}

		readyAt := now.Add(b.durationUntil(cost, now))
		if timer == nil {
			timer = l.clock.NewTimer(readyAt)
		} else {
			timer.Reset(readyAt)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C():
		}
	}
}

// refund 歸還先前扣除的 token，用於雙維度限流的部分失敗補償。
func (l *Limiter) refund(tenantID string, cost int64) {
	b, err := l.bucket(tenantID)
	if err != nil {
		return
	}
	b.giveBack(cost, l.clock.Now())
}

// bucket 取得（必要時建立）租戶的 bucket。
func (l *Limiter) bucket(tenantID string) (*bucket, error) {
	l.mu.RLock()
	b, ok := l.buckets[tenantID]
	l.mu.RUnlock()
	if ok {
		return b, nil
	}

	cfg := l.cfg.Default
	if override, ok := l.cfg.PerTenant[tenantID]; ok {
		cfg = override
	}
	if err := cfg.valid(); err != nil {
		return nil, err
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	// 雙重檢查：可能有其他 goroutine 在我們升級鎖的空檔建好了。
	if b, ok := l.buckets[tenantID]; ok {
		return b, nil
	}
	b = &bucket{
		capacity:   cfg.Capacity,
		refillRate: cfg.RefillPerSecond,
		tokens:     float64(cfg.Capacity),
		lastRefill: l.clock.Now(),
	}
	l.buckets[tenantID] = b
	return b, nil
}

// bucket 是單一租戶的 token 狀態。
type bucket struct {
	capacity   int64
	refillRate float64

	mu         sync.Mutex
	tokens     float64
	lastRefill time.Time
}

func (b *bucket) validateCost(cost int64) error {
	if cost <= 0 {
		return fmt.Errorf("%w: cost must be positive", ErrInvalidRequest)
	}
	if cost > b.capacity {
		return fmt.Errorf("%w: cost %d > capacity %d", ErrCostExceedsCapacity, cost, b.capacity)
	}
	return nil
}

func (b *bucket) take(cost int64, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.refillLocked(now)
	if b.tokens < float64(cost) {
		return false
	}
	b.tokens -= float64(cost)
	return true
}

func (b *bucket) giveBack(cost int64, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.refillLocked(now)
	b.tokens = min(b.tokens+float64(cost), float64(b.capacity))
}

// durationUntil 回報還要多久才會累積到 cost 個 token。
func (b *bucket) durationUntil(cost int64, now time.Time) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.refillLocked(now)
	missing := float64(cost) - b.tokens
	if missing <= 0 {
		return 0
	}
	return time.Duration(missing / b.refillRate * float64(time.Second))
}

// refillLocked 依經過時間補 token，並夾在容量上限內，避免長時間閒置累積成無限爆發量。
func (b *bucket) refillLocked(now time.Time) {
	elapsed := now.Sub(b.lastRefill)
	if elapsed <= 0 {
		// 時間未前進（或時鐘回撥）時不補也不倒扣，保持冪等。
		return
	}
	b.lastRefill = now
	b.tokens = min(b.tokens+elapsed.Seconds()*b.refillRate, float64(b.capacity))
}

// gate 把商戶與通道兩個維度組合成一次裁決。
type gate struct {
	merchant *Limiter
	channel  *Limiter
}

// allow 依序取得兩個維度的額度。
// 若通道側失敗，必須歸還商戶側已扣的 token，否則被通道擋下的流量會持續侵蝕商戶配額。
// 兩把桶鎖從不同時持有，因此不存在鎖循環。
func (g *gate) allow(merchantID, channelID string, cost int64) (bool, error) {
	ok, err := g.merchant.Allow(merchantID, cost)
	if err != nil || !ok {
		return false, err
	}

	ok, err = g.channel.Allow(channelID, cost)
	if err != nil || !ok {
		g.merchant.refund(merchantID, cost)
		return false, err
	}
	return true, nil
}
