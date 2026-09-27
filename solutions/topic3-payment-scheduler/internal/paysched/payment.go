package paysched

import (
	"sync"
	"time"
)

// ScheduleID 是引擎產生的排程唯一識別碼，同時作為對外部通道的冪等鍵。
type ScheduleID string

// Priority 是付款的優先權層級。數值越小越優先。
type Priority int

const (
	// PriorityHigh 最高優先權。
	PriorityHigh Priority = iota
	// PriorityNormal 一般優先權，為零值預設。
	PriorityNormal
	// PriorityLow 最低優先權。
	PriorityLow
)

// String 讓 Priority 在日誌與稽核輸出中可讀。
func (p Priority) String() string {
	switch p {
	case PriorityHigh:
		return "HIGH"
	case PriorityLow:
		return "LOW"
	default:
		return "NORMAL"
	}
}

// PaymentRequest 是呼叫端提交的付款請求。
type PaymentRequest struct {
	// MerchantID 用於商戶維度限流，防止吵鬧鄰居擠壓其他商戶。
	MerchantID string
	// ChannelID 用於清算通道維度限流，保護銀行/RPC 節點的 QPS 上限。
	ChannelID string
	// Amount 為最小貨幣單位的正整數金額。
	Amount int64
	// Currency 例如 "USD"。
	Currency string
	// Cost 是本筆付款消耗的限流權重，零值視為 1。
	Cost int64
	// Priority 僅在同一到期時刻之間決勝，不會讓付款提前於 ExecuteAt 送出。
	Priority Priority
	// ExecuteAt 為預定執行時間，必須晚於當下；本引擎不支援立即執行。
	ExecuteAt time.Time
	// IdempotencyKey 由引擎在排程時填入（等同 ScheduleID），呼叫端無須設定。
	// 同一筆付款的所有重試都帶相同的鍵，讓下游得以拒絕重複扣款。
	IdempotencyKey string
}

// ExecutionResult 是外部通道成功執行後回傳的結果。
type ExecutionResult struct {
	// ExternalRef 是外部系統的交易編號，供對帳使用。
	ExternalRef string
	// CompletedAt 是外部系統回報的完成時間。
	CompletedAt time.Time
}

// PaymentRecord 是付款的稽核快照。它是純資料且可安全複製，
// 因此 GetPaymentStatus 能在不外洩內部鎖的情況下回傳完整狀態。
type PaymentRecord struct {
	ScheduleID    ScheduleID
	Request       PaymentRequest
	Status        Status
	ExecuteAt     time.Time
	RetryAttempts int
	// LimiterDeferrals 是因限流而延後的次數，刻意與 RetryAttempts 分離：
	// 限流是系統自我節流，不應消耗付款的重試額度。
	LimiterDeferrals int
	LastError        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DispatchedAt     time.Time
	SettledAt        time.Time
	Result           ExecutionResult
}

// payment 是 record 的內部包裝。
//
// 鎖的劃分刻意分成兩層，兩者不可混用：
//   - mu 保護 rec，也就是單筆付款的狀態流轉（臨界區極小，用 Mutex 即可）。
//   - dueAt / priority / seq 是排程鍵，只在持有 Engine 鎖時讀寫，
//     供 min-heap 比較使用。這樣 heap 比較永遠不需要碰 payment 層的鎖，
//     排程與狀態流轉互不阻塞，也不會構成鎖循環。
type payment struct {
	mu  sync.Mutex
	rec PaymentRecord

	dueAt    time.Time
	priority Priority
	seq      uint64
}

// snapshot 回傳 record 的副本。
func (p *payment) snapshot() PaymentRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rec
}

// transition 在檢查狀態機合法性後套用變更，是所有狀態流轉的唯一入口。
// apply 在持有 payment 鎖期間執行，因此嚴禁在其中做任何 I/O 或取 Engine 鎖。
func (p *payment) transition(to Status, now time.Time, apply func(rec *PaymentRecord)) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := canTransition(p.rec.Status, to); err != nil {
		return err
	}
	p.rec.Status = to
	p.rec.UpdatedAt = now
	if apply != nil {
		apply(&p.rec)
	}
	return nil
}

// status 讀取當前狀態，供 dispatcher 做惰性丟棄判斷。
func (p *payment) status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rec.Status
}
