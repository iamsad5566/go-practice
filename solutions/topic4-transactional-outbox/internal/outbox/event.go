package outbox

import (
	"fmt"
	"net/url"
	"sync"
	"time"
)

// EventID 是事件在 outbox 中的唯一識別碼。
//
// 它同時是投遞給 merchant 的去重鍵：同一事件的所有重試都帶相同的 EventID，
// merchant 端只要對它建唯一索引，就能在 at-least-once 語意下維持冪等。
type EventID string

// OutboxEvent 是呼叫端提交的業務事件。
type OutboxEvent struct {
	// EventID 可由呼叫端指定以取得去重保證（同一 ID 重複發布會被拒絕）；
	// 留空則由引擎產生。這是「transactional outbox」的關鍵：業務交易內產生的
	// 唯一鍵傳進來，即使發布邏輯被重跑也不會造成兩次投遞。
	EventID EventID
	// MerchantID 決定簽章密鑰與 per-merchant 的併發配額。
	MerchantID string
	// EventType 例如 payment.settled、payout.failed。
	EventType string
	// Payload 是要送給 merchant 的原始位元組，通常是 JSON。
	// 引擎在登錄時會複製一份，之後不再改動——簽章必須與送出的內容完全一致。
	Payload []byte
	// DestURL 是 merchant 的 webhook 端點，必須是 http 或 https。
	DestURL string
	// CreatedAt 是業務事件的發生時間。留空時由引擎填入登錄時刻。
	CreatedAt time.Time
	// Sequence 是引擎指派的全域單調序號。
	//
	// 本引擎刻意不保證 per-merchant FIFO（詳見 package 說明），因此把序號隨
	// X-Event-Sequence 一併送出：merchant 端可據此丟棄過期事件，自行補回順序語意。
	Sequence uint64
}

// EventRecord 是事件的投遞稽核快照。它是純資料，可安全複製，
// 因此 GetEventStatus 與 ListDLQ 能在不外洩內部鎖的情況下回傳完整狀態。
//
// 鎖刻意不放在這個結構裡：一旦內嵌 sync.Mutex，任何以值回傳的 API 都會
// 複製鎖（go vet copylocks），且複製出的鎖不再保護原本的資料。
type EventRecord struct {
	Event  OutboxEvent
	Status Status
	// RetryCount 是已失敗的投遞次數。ReplayEvent 會歸零。
	RetryCount int
	// LastError 是最後一次失敗的原因描述。
	LastError string
	// LastDeliveryID 是最後一次投遞嘗試的編號，對照 merchant 端日誌用。
	LastDeliveryID string
	// LastStatusCode 是最後一次收到的 HTTP 狀態碼，網路層失敗時為 0。
	LastStatusCode int
	// NextAttemptAt 是下次可投遞的時刻，退避期間指向未來。
	NextAttemptAt time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeliveredAt   time.Time
	// DeadLetteredAt 供 reaper 依保留期回收，並標示進入 DLQ 的時間。
	DeadLetteredAt time.Time
}

// clone 回傳一份連 Payload 都獨立的深層副本。
//
// EventRecord 以值傳遞時，內含的 Payload slice 仍與引擎共用底層陣列——
// 呼叫端只要改動一個位元組，之後每次重試送出的內容與簽章都會跟著變。
// 因此所有對外的 API（GetEventStatus、ListDLQ）都必須經過這裡；
// 引擎內部的 snapshot 則刻意不複製，避免在投遞熱路徑上多一次配置。
func (r EventRecord) clone() EventRecord {
	r.Event.Payload = append([]byte(nil), r.Event.Payload...)
	return r
}

// eventEntry 是 record 的內部包裝。
//
// 鎖的劃分刻意分成兩層，兩者不可混用：
//   - mu 保護 rec，也就是單筆事件的狀態流轉（臨界區極小，用 Mutex 即可）。
//   - nextAttemptAt 是排程鍵，只在持有 Engine 鎖時讀寫，供 min-heap 比較使用。
//     它與 rec.NextAttemptAt 是同一個時刻的兩份拷貝：後者是給呼叫端看的稽核值，
//     前者是給 heap 用的排序值。分開存放讓 heap 比較永遠不必碰 entry 層的鎖，
//     派送與狀態流轉互不阻塞，也不會構成鎖循環。
type eventEntry struct {
	mu  sync.Mutex
	rec EventRecord

	nextAttemptAt time.Time
}

// newEntry 建立一筆 PENDING 狀態的 entry。
func newEntry(ev OutboxEvent, id EventID, seq uint64, now time.Time) *eventEntry {
	ev.EventID = id
	ev.Sequence = seq
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = now
	}
	// 複製 payload，切斷與呼叫端 slice 的共享。
	// 少了這一步，呼叫端事後改動 slice 會讓簽章與實際送出的內容對不上。
	ev.Payload = append([]byte(nil), ev.Payload...)

	return &eventEntry{
		rec: EventRecord{
			Event:  ev,
			Status: StatusPending,
			// 首次投遞不必等待。
			NextAttemptAt: now,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		nextAttemptAt: now,
	}
}

// snapshot 回傳 record 的副本。
func (e *eventEntry) snapshot() EventRecord {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rec
}

// status 讀取當前狀態，供 dispatcher 做惰性丟棄判斷。
func (e *eventEntry) status() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rec.Status
}

// transition 在檢查狀態機合法性後套用變更，是所有狀態流轉的唯一入口。
//
// 檢查與寫入在同一個臨界區內完成，因此 PENDING → IN_FLIGHT 是原子的 CAS，
// 構成防雙重派送的最後一道閘（PRD 非功能需求 2）。
// apply 在持有 entry 鎖期間執行，嚴禁在其中做任何 I/O 或取 Engine 鎖。
func (e *eventEntry) transition(to Status, now time.Time, apply func(rec *EventRecord)) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := canTransition(e.rec.Status, to); err != nil {
		return err
	}
	e.rec.Status = to
	e.rec.UpdatedAt = now
	if apply != nil {
		apply(&e.rec)
	}
	return nil
}

// validateEvent 檢查事件欄位。所有不合法情形都包成 ErrInvalidEvent，
// 並附上具體欄位讓呼叫端不必猜。
func validateEvent(ev OutboxEvent) error {
	switch {
	case ev.MerchantID == "":
		return fmt.Errorf("%w: MerchantID is required", ErrInvalidEvent)
	case ev.EventType == "":
		return fmt.Errorf("%w: EventType is required", ErrInvalidEvent)
	case ev.Payload == nil:
		return fmt.Errorf("%w: Payload is required", ErrInvalidEvent)
	case ev.DestURL == "":
		return fmt.Errorf("%w: DestURL is required", ErrInvalidEvent)
	}

	u, err := url.Parse(ev.DestURL)
	if err != nil {
		return fmt.Errorf("%w: DestURL is not a valid URL: %v", ErrInvalidEvent, err)
	}
	// 只允許 http(s)：其他 scheme 送不出 webhook，且應在登錄時就擋下，
	// 而不是等到 worker 投遞失敗才進 DLQ。
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: DestURL scheme must be http or https, got %q", ErrInvalidEvent, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("%w: DestURL is missing host", ErrInvalidEvent)
	}
	return nil
}
