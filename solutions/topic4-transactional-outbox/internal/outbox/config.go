package outbox

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	mathrand "math/rand/v2"
	"time"
)

// Config 是引擎的組裝參數。零值欄位會套用合理預設，
// 只有 Sender 與 Secrets 是必填。
type Config struct {
	// Sender 是投遞管道實作（生產環境用 NewHTTPSender）。
	Sender WebhookSender
	// Secrets 提供各 merchant 的簽章密鑰。
	Secrets SecretStore

	// Workers 是執行 HTTP 投遞的 worker 數量，預設 8。
	Workers int
	// QueueSize 是 dispatcher 到 worker 的交接佇列長度，預設為 Workers 的兩倍。
	QueueSize int

	// MaxRetries 是可重試失敗的最大重試次數，預設 5。
	// 零值代表「未設定」而套用預設；要完全關閉重試請給負值（會正規化為 0）。
	MaxRetries int
	// BaseBackoff 是第一次重試的退避基準，預設 1 秒。
	BaseBackoff time.Duration
	// MaxBackoff 是退避時間上限，預設 5 分鐘。
	// webhook 的對象是別人的服務，上限拉長比拉短合理：merchant 部署一次可能要好幾分鐘。
	MaxBackoff time.Duration

	// MaxInflightPerMerchant 限制單一 merchant 同時佔用的 worker 數，預設 2。
	//
	// 這是防「毒藥端點」的核心機制：某個 merchant 的端點要 30 秒才逾時，
	// 若不設限，它的 10 筆事件就能佔滿 8 個 worker，讓所有其他 merchant 被餓死。
	// 有了上限，單一 merchant 最多只能吃掉這麼多 worker。
	MaxInflightPerMerchant int
	// MerchantBusyBackoff 是併發額度已滿時的重排間隔，預設 50 毫秒。
	MerchantBusyBackoff time.Duration

	// SendTimeout 是單次投遞的逾時，預設 10 秒。
	SendTimeout time.Duration
	// ShutdownTimeout 是 Close 等待進行中投遞完成的上限，預設 30 秒。
	ShutdownTimeout time.Duration

	// Retention 是終態事件（DELIVERED / DEAD_LETTER）在記憶體中的保留期，預設 1 小時。
	//
	// 沒有這個機制，成功送達的事件會永久留在 map 裡，記憶體只增不減。
	// 保留一段時間而非立即刪除，是為了讓 GetEventStatus 在投遞後仍能查到結果。
	// DEAD_LETTER 同樣受保留期約束，因此 DLQ 必須在期限內被處理掉。
	//
	// 保留期同時也是 EventID 去重窗口的長度：事件被回收後，
	// 同一個 EventID 再次發布會被視為新事件。要真正無期限去重，
	// 得把 records 換成持久化儲存——這正是本引擎定位為記憶體內原型的邊界。
	Retention time.Duration
	// ReapInterval 是回收掃描的間隔，預設 1 分鐘。
	ReapInterval time.Duration

	// Clock 可注入以便測試，預設為系統時鐘。
	Clock Clock
	// IsRetryable 可覆寫重試判斷，預設為 DefaultIsRetryable。
	IsRetryable func(HTTPResponse, error) bool
	// Rand 回傳 [0, n] 之間的整數，供退避 jitter 使用；可注入以便測試。
	Rand func(n int64) int64
	// NewEventID 在呼叫端未指定 EventID 時產生一個，可注入以便測試。
	NewEventID func() (EventID, error)
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
	} else if c.MaxRetries == 0 {
		c.MaxRetries = 5
	}
	if c.BaseBackoff <= 0 {
		c.BaseBackoff = time.Second
	}
	if c.MaxBackoff < c.BaseBackoff {
		c.MaxBackoff = max(5*time.Minute, c.BaseBackoff)
	}
	if c.MaxInflightPerMerchant <= 0 {
		c.MaxInflightPerMerchant = 2
	}
	if c.MerchantBusyBackoff <= 0 {
		c.MerchantBusyBackoff = 50 * time.Millisecond
	}
	if c.SendTimeout <= 0 {
		c.SendTimeout = 10 * time.Second
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 30 * time.Second
	}
	if c.Retention <= 0 {
		c.Retention = time.Hour
	}
	if c.ReapInterval <= 0 {
		c.ReapInterval = time.Minute
	}
	if c.Clock == nil {
		c.Clock = NewRealClock()
	}
	if c.IsRetryable == nil {
		c.IsRetryable = DefaultIsRetryable
	}
	if c.Rand == nil {
		c.Rand = func(n int64) int64 {
			if n <= 0 {
				return 0
			}
			return mathrand.Int64N(n + 1)
		}
	}
	if c.NewEventID == nil {
		c.NewEventID = newEventID
	}
}

func (c *Config) validate() error {
	if c.Sender == nil {
		return fmt.Errorf("%w: Sender is required", ErrInvalidConfig)
	}
	if c.Secrets == nil {
		return fmt.Errorf("%w: Secrets is required", ErrInvalidConfig)
	}
	return nil
}

// backoffFor 計算第 attempt 次重試（從 1 起算）的退避時間。
//
// 採指數退避 + equal jitter：在 [exp/2, exp] 之間隨機取值。
// 刻意不用 full jitter（[0, exp]）——那會讓某些重試幾乎立刻發生，
// 而 merchant 剛回 503 時最需要的就是一段真實的喘息時間。
// equal jitter 保留了打散重試潮的效果，同時保證至少等一半的指數時間。
func (c *Config) backoffFor(attempt int) time.Duration {
	exp := c.BaseBackoff
	for i := 1; i < attempt && exp < c.MaxBackoff; i++ {
		exp *= 2
	}
	exp = min(exp, c.MaxBackoff)

	half := exp / 2
	return half + time.Duration(c.Rand(int64(exp-half)))
}

// newEventID 產生引擎預設的事件編號。
func newEventID() (EventID, error) {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("outbox: generate event id: %w", err)
	}
	return EventID("evt_" + hex.EncodeToString(buf[:])), nil
}
