package paysched

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"
)

// PaymentExecutor 是外部清算通道（銀行清算軌道、區塊鏈 RPC 節點）的抽象。
// 實作者必須以 req.IdempotencyKey 做去重，同一筆付款的所有重試都帶相同的鍵。
type PaymentExecutor interface {
	Execute(ctx context.Context, payment PaymentRequest) (ExecutionResult, error)
}

// Config 是引擎的組裝參數。零值欄位會套用合理預設，只有 Executor 是必填。
type Config struct {
	// Executor 為必填的外部通道實作。
	Executor PaymentExecutor

	// Workers 是執行外部 I/O 的 worker 數量，預設 8。
	Workers int
	// QueueSize 是 dispatcher 到 worker 的交接佇列長度，預設為 Workers 的兩倍。
	// 佇列滿時 dispatcher 會阻塞而非丟棄任務——金流不允許靜默掉單。
	QueueSize int

	// MaxRetries 是可重試錯誤的最大重試次數，預設 3；設為負數表示完全不重試。
	MaxRetries int
	// BaseBackoff 是第一次重試的退避基準，預設 1 秒。
	BaseBackoff time.Duration
	// MaxBackoff 是退避時間上限，預設 30 秒。
	MaxBackoff time.Duration

	// LimiterBackoff 是被限流擋下後的重排間隔，預設 1 秒。
	LimiterBackoff time.Duration
	// MaxLimiterDeferrals 是限流重排的上限，避免持續壅塞的付款無限重排，預設 20。
	MaxLimiterDeferrals int

	// Merchant 與 Channel 分別是商戶維度與通道維度的限流配額。
	Merchant LimiterConfig
	Channel  LimiterConfig

	// ExecuteTimeout 是單次外部呼叫的逾時，預設 30 秒。
	ExecuteTimeout time.Duration

	// Clock 可注入以便測試，預設為系統時鐘。
	Clock Clock
	// IsRetryable 可覆寫重試判斷，預設為 DefaultIsRetryable。
	IsRetryable func(error) bool
	// Rand 回傳 [0, n] 之間的整數，供 full jitter 使用；可注入以便測試。
	Rand func(n int64) int64
}

func (c *Config) applyDefaults() {
	if c.Workers <= 0 {
		c.Workers = 8
	}
	if c.QueueSize <= 0 {
		c.QueueSize = c.Workers * 2
	}
	if c.MaxRetries < 0 {
		c.MaxRetries = 0
	}
	if c.MaxRetries == 0 {
		c.MaxRetries = 3
	}
	if c.BaseBackoff <= 0 {
		c.BaseBackoff = time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 30 * time.Second
	}
	if c.LimiterBackoff <= 0 {
		c.LimiterBackoff = time.Second
	}
	if c.MaxLimiterDeferrals <= 0 {
		c.MaxLimiterDeferrals = 20
	}
	if c.ExecuteTimeout <= 0 {
		c.ExecuteTimeout = 30 * time.Second
	}
	if c.Clock == nil {
		c.Clock = NewRealClock()
	}
	if c.IsRetryable == nil {
		c.IsRetryable = DefaultIsRetryable
	}
	if c.Rand == nil {
		c.Rand = func(n int64) int64 { return rand.Int64N(n + 1) }
	}
	if c.Merchant.Default.Capacity <= 0 {
		c.Merchant.Default = BucketConfig{Capacity: 100, RefillPerSecond: 50}
	}
	if c.Channel.Default.Capacity <= 0 {
		c.Channel.Default = BucketConfig{Capacity: 50, RefillPerSecond: 25}
	}
}

func (c *Config) validate() error {
	if c.Executor == nil {
		return fmt.Errorf("%w: Executor is required", ErrInvalidRequest)
	}
	if err := c.Merchant.Default.valid(); err != nil {
		return fmt.Errorf("merchant limiter: %w", err)
	}
	if err := c.Channel.Default.valid(); err != nil {
		return fmt.Errorf("channel limiter: %w", err)
	}
	return nil
}

// backoffFor 計算第 attempt 次重試（從 1 起算）的退避時間，
// 採指數退避 + full jitter：在 [0, exp] 之間隨機取值，避免大量付款同時重擊下游。
func (c *Config) backoffFor(attempt int) time.Duration {
	exp := c.BaseBackoff
	for i := 1; i < attempt && exp < c.MaxBackoff; i++ {
		exp *= 2
	}
	exp = min(exp, c.MaxBackoff)
	return time.Duration(c.Rand(int64(exp)))
}

// limiterBackoff 是限流重排的間隔，同樣加上 full jitter 打散重試潮。
func (c *Config) limiterBackoff() time.Duration {
	return time.Duration(c.Rand(int64(c.LimiterBackoff)))
}
