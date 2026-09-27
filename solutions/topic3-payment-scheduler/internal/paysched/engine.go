// Package paysched 提供一個記憶體內的付款排程引擎與多租戶限流閘門。
//
// 併發模型（三層，責任清楚分離）：
//
//	Engine 鎖（RWMutex）：只保護「records 對照表」與「到期佇列」這兩個共享容器，
//	    臨界區內僅有 map/heap 操作，絕不包含任何 I/O。
//	payment 鎖（Mutex）：只保護單筆付款的狀態流轉，臨界區同樣極小。
//	無鎖區：worker 呼叫外部通道的網路 I/O 全程不持有任何鎖。
//
// 鎖階層固定為 Engine → payment，且從不反向取得，因此不可能死鎖。
package paysched

import (
	"container/heap"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Engine 是排程引擎。以 New 建立，使用完畢必須呼叫 Close。
type Engine struct {
	cfg   Config
	clock Clock
	gate  *gate

	// mu 保護 records 與 queue。
	mu      sync.RWMutex
	records map[ScheduleID]*payment
	queue   paymentQueue
	closed  bool

	seq atomic.Uint64

	// wake 通知 dispatcher 佇列內容有變（新排程或重排），緩衝 1 即足夠：
	// 訊號只代表「該重新檢視堆頂」，多次通知可合併。
	wake chan struct{}
	// dispatch 是 dispatcher 交棒給 worker 的有界佇列。
	dispatch chan *payment
	// stop 在 Close 時關閉，作為所有迴圈的退出訊號。
	stop chan struct{}

	dispatcherWG sync.WaitGroup
	workerWG     sync.WaitGroup
	closeOnce    sync.Once
}

// New 建立並啟動引擎：一個 dispatcher goroutine 加上 cfg.Workers 個 worker。
// goroutine 與計時器數量固定，與排程筆數無關。
func New(cfg Config) (*Engine, error) {
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	e := &Engine{
		cfg:     cfg,
		clock:   cfg.Clock,
		records: make(map[ScheduleID]*payment),
		gate: &gate{
			merchant: NewLimiter(cfg.Merchant, cfg.Clock),
			channel:  NewLimiter(cfg.Channel, cfg.Clock),
		},
		wake:     make(chan struct{}, 1),
		dispatch: make(chan *payment, cfg.QueueSize),
		stop:     make(chan struct{}),
	}

	e.dispatcherWG.Add(1)
	go e.runDispatcher()

	e.workerWG.Add(cfg.Workers)
	for i := 0; i < cfg.Workers; i++ {
		go e.runWorker()
	}
	return e, nil
}

// SchedulePayment 登記一筆未來執行的付款並回傳其 ScheduleID。
// ExecuteAt 必須晚於當下；本引擎不支援立即執行，過期或零值一律以 ErrPastExecuteAt 拒絕。
func (e *Engine) SchedulePayment(req PaymentRequest) (ScheduleID, error) {
	now := e.clock.Now()
	if err := validateRequest(req, now); err != nil {
		return "", err
	}
	if req.Cost <= 0 {
		req.Cost = 1
	}

	seq := e.seq.Add(1)
	id, err := newScheduleID(seq)
	if err != nil {
		return "", err
	}
	// 冪等鍵由引擎指定並貫穿所有重試，讓下游能辨識重複投遞。
	req.IdempotencyKey = string(id)

	p := &payment{
		rec: PaymentRecord{
			ScheduleID: id,
			Request:    req,
			Status:     StatusScheduled,
			ExecuteAt:  req.ExecuteAt,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
		dueAt:    req.ExecuteAt,
		priority: req.Priority,
		seq:      seq,
	}

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return "", ErrEngineClosed
	}
	e.records[id] = p
	heap.Push(&e.queue, p)
	e.mu.Unlock()

	e.notifyDispatcher()
	return id, nil
}

// CancelPayment 取消尚未進入結算作業的付款。
//
// 取消與派送的裁決完全由 payment 層的鎖決定：先取得鎖者勝出。
// 若付款已被 worker 推進到 DISPATCHED（正在呼叫外部通道），回傳 ErrInProgress。
//
// 取消採惰性策略：只改狀態，不從堆中移除。dispatcher 彈出後看到終態就丟棄，
// 因此無須在 payment 上維護 heap index。record 本身作為稽核資料保留，不刪除 map key，
// 所以整個流程只需要 Engine 讀鎖。
func (e *Engine) CancelPayment(id ScheduleID) error {
	p, err := e.lookup(id)
	if err != nil {
		return err
	}
	return p.transition(StatusCancelled, e.clock.Now(), nil)
}

// GetPaymentStatus 回傳付款的稽核快照，含狀態、重試次數與最後的錯誤細節。
func (e *Engine) GetPaymentStatus(id ScheduleID) (PaymentRecord, error) {
	p, err := e.lookup(id)
	if err != nil {
		return PaymentRecord{}, err
	}
	return p.snapshot(), nil
}

// Allow 對外暴露雙維度限流裁決：商戶維度防吵鬧鄰居，通道維度保護清算軌道。
// 商戶通過但通道被拒時會自動歸還商戶 token。
func (e *Engine) Allow(merchantID, channelID string, cost int64) (bool, error) {
	return e.gate.allow(merchantID, channelID, cost)
}

// MerchantLimiter 與 ChannelLimiter 供呼叫端做單維度的 Allow/Wait 操作。
func (e *Engine) MerchantLimiter() *Limiter { return e.gate.merchant }
func (e *Engine) ChannelLimiter() *Limiter  { return e.gate.channel }

// Close 停止接受新排程、結束 dispatcher，並等待 worker 把已交接的付款做完。
//
// 刻意不中斷進行中的外部呼叫：金流請求已送出時，強行取消 context 會讓本地狀態
// 與銀行端結果不一致。單次呼叫的時間上限由 Config.ExecuteTimeout 控制。
// Close 可安全重複呼叫。
func (e *Engine) Close() {
	e.closeOnce.Do(func() {
		e.mu.Lock()
		e.closed = true
		e.mu.Unlock()

		close(e.stop)
		// 必須等 dispatcher 收工後才關閉交接佇列，否則會對已關閉的 channel 送值。
		e.dispatcherWG.Wait()
		close(e.dispatch)
		e.workerWG.Wait()
	})
}

// lookup 只用讀鎖取出 payment 指標後立刻釋放，後續狀態變更改由 payment 層的鎖負責，
// 因此外部 I/O 與狀態流轉都不會卡住 Engine 這一層。
func (e *Engine) lookup(id ScheduleID) (*payment, error) {
	e.mu.RLock()
	p, ok := e.records[id]
	e.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return p, nil
}

// reschedule 以新的到期時間把付款放回佇列（用於重試與限流延後）。
// 排程鍵只在持有 Engine 鎖時寫入，heap 比較因此完全不需要碰 payment 的鎖。
func (e *Engine) reschedule(p *payment, dueAt time.Time) {
	e.mu.Lock()
	if e.closed {
		// 引擎已關閉：付款留在 SCHEDULED 供稽核，不再排入佇列。
		e.mu.Unlock()
		return
	}
	p.dueAt = dueAt
	heap.Push(&e.queue, p)
	e.mu.Unlock()

	e.notifyDispatcher()
}

// notifyDispatcher 發出「請重新檢視堆頂」的訊號；訊號可合併，故不阻塞。
func (e *Engine) notifyDispatcher() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func validateRequest(req PaymentRequest, now time.Time) error {
	switch {
	case req.MerchantID == "":
		return fmt.Errorf("%w: MerchantID is required", ErrInvalidRequest)
	case req.ChannelID == "":
		return fmt.Errorf("%w: ChannelID is required", ErrInvalidRequest)
	case req.Amount <= 0:
		return fmt.Errorf("%w: Amount must be positive", ErrInvalidRequest)
	case req.Currency == "":
		return fmt.Errorf("%w: Currency is required", ErrInvalidRequest)
	case req.Priority < PriorityHigh || req.Priority > PriorityLow:
		return fmt.Errorf("%w: unknown Priority %d", ErrInvalidRequest, req.Priority)
	case !req.ExecuteAt.After(now):
		return fmt.Errorf("%w: got %v, now %v", ErrPastExecuteAt, req.ExecuteAt, now)
	default:
		return nil
	}
}

// newScheduleID 以單調序號加上隨機尾碼組成。
// 序號讓同批排程可排序（也是 heap 的最終決勝鍵），隨機尾碼避免 ID 可被猜測。
func newScheduleID(seq uint64) (ScheduleID, error) {
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("paysched: generate schedule id: %w", err)
	}
	return ScheduleID(fmt.Sprintf("pay_%012d_%s", seq, hex.EncodeToString(suffix[:]))), nil
}
