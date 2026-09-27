// Package outbox 提供一個記憶體內的 transactional outbox 與 webhook 投遞引擎。
//
// # 投遞語意
//
// 引擎保證 at-least-once：事件一旦登錄就不會遺失，但可能重複投遞
// （例如 merchant 已處理成功、回應卻在網路上丟了）。因此每次投遞都帶
// 恆定的 X-Event-ID 供 merchant 去重，以及每次嘗試都不同的 X-Delivery-ID 供審計。
//
// 順序上刻意採「全域併發、允許亂序」：同一 merchant 的兩個事件若前者進入退避，
// 後者不會被擋住。代價是 merchant 可能先收到較新的事件，
// 因此引擎把單調的 X-Event-Sequence 一併送出，讓 merchant 能自行丟棄過期事件。
// 反過來若採嚴格 FIFO，一個進入長退避的事件會擋住該 merchant 後面所有事件
// （head-of-line blocking），在通知場景裡這個代價通常比亂序更難接受。
//
// # 併發模型（三層，責任清楚分離）
//
//	Engine 鎖（RWMutex）：只保護 records 對照表、到期佇列、DLQ 索引與
//	    per-merchant 併發計數這四個共享容器，臨界區內僅有 map/heap/slice 操作，
//	    絕不包含任何 I/O。
//	eventEntry 鎖（Mutex）：只保護單筆事件的狀態流轉，臨界區同樣極小。
//	無鎖區：worker 呼叫 merchant 端點的網路 I/O 全程不持有任何鎖。
//
// 鎖階層固定為 Engine → eventEntry，且從不反向取得，因此不可能死鎖。
//
// # goroutine 配置
//
// 一個 dispatcher、cfg.Workers 個 worker、一個 reaper，數量固定，與事件筆數無關。
// 整個引擎只使用兩個計時器（dispatcher 與 reaper 各一），
// 絕不為每筆退避中的事件各開一個計時器。
package outbox

import (
	"container/heap"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Engine 是 outbox 投遞引擎。以 New 建立，使用完畢必須呼叫 Close。
type Engine struct {
	cfg    Config
	clock  Clock
	signer *signer

	// mu 保護以下四個共享容器。
	mu      sync.RWMutex
	records map[EventID]*eventEntry
	queue   eventQueue
	// dlq 只存 EventID，records 才是唯一真實來源。
	//
	// 刻意不另存一份 *eventEntry：那樣會有兩份可獨立變動的狀態，
	// replay 後若沒同步清掉，DLQ 裡就會留下幽靈項目，
	// 重試再失敗還會出現重複。這裡改由 ReplayEvent 與 reaper 同步維護索引，
	// 因此索引裡永遠只有真正處於 DEAD_LETTER 的事件，不需要背景清理。
	dlq []EventID
	// inflight 是 per-merchant 的進行中投遞數，用於擋住毒藥端點。
	// 額度由 dispatcher 在交棒前預留，由 worker 在投遞結束後歸還。
	inflight map[string]int
	closed   bool

	seq atomic.Uint64

	// wake 通知 dispatcher 佇列內容有變（新事件或重排），緩衝 1 即足夠：
	// 訊號只代表「該重新檢視堆頂」，多次通知可合併。
	wake chan struct{}
	// dispatch 是 dispatcher 交棒給 worker 的有界佇列。
	dispatch chan *eventEntry
	// stop 在 Close 時關閉，作為所有迴圈的退出訊號。
	stop chan struct{}

	dispatcherWG sync.WaitGroup
	workerWG     sync.WaitGroup
	reaperWG     sync.WaitGroup
	closeOnce    sync.Once
	// live 是存活的內部 goroutine 數，供關閉後驗證無洩漏。
	live atomic.Int64
	// reapRounds 是回收器已完成的掃描輪數。
	reapRounds atomic.Int64
}

// New 建立並啟動引擎：一個 dispatcher、cfg.Workers 個 worker、一個 reaper。
func New(cfg Config) (*Engine, error) {
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	e := &Engine{
		cfg:      cfg,
		clock:    cfg.Clock,
		signer:   &signer{secrets: cfg.Secrets},
		records:  make(map[EventID]*eventEntry),
		inflight: make(map[string]int),
		wake:     make(chan struct{}, 1),
		dispatch: make(chan *eventEntry, cfg.QueueSize),
		stop:     make(chan struct{}),
	}

	e.dispatcherWG.Add(1)
	e.live.Add(1)
	go e.runDispatcher()

	e.workerWG.Add(cfg.Workers)
	e.live.Add(int64(cfg.Workers))
	for i := 0; i < cfg.Workers; i++ {
		go e.runWorker()
	}

	e.reaperWG.Add(1)
	e.live.Add(1)
	go e.runReaper()

	return e, nil
}

// PublishEvent 把事件登錄進 outbox 並回傳其 EventID。
//
// 這是業務交易的出口，必須極快：整個函式只在 Engine 寫鎖內做一次 map 寫入
// 與一次 heap push，完全不接觸網路。因此再慢或再多的 merchant 端點
// 都不可能拖慢事件發布（PRD 非功能需求 3）。
//
// ev.EventID 留空時由引擎產生；指定時會做去重，重複發布回 ErrDuplicateEvent。
func (e *Engine) PublishEvent(ev OutboxEvent) (EventID, error) {
	if err := validateEvent(ev); err != nil {
		return "", err
	}

	id := ev.EventID
	if id == "" {
		generated, err := e.cfg.NewEventID()
		if err != nil {
			return "", err
		}
		id = generated
	}

	now := e.clock.Now()
	entry := newEntry(ev, id, e.seq.Add(1), now)

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return "", ErrEngineClosed
	}
	if _, exists := e.records[id]; exists {
		e.mu.Unlock()
		return "", fmt.Errorf("%w: %s", ErrDuplicateEvent, id)
	}
	e.records[id] = entry
	heap.Push(&e.queue, entry)
	e.mu.Unlock()

	e.notifyDispatcher()
	return id, nil
}

// GetEventStatus 回傳事件的投遞稽核快照，含狀態、重試次數與最後的失敗細節。
func (e *Engine) GetEventStatus(id EventID) (EventRecord, error) {
	entry, err := e.lookup(id)
	if err != nil {
		return EventRecord{}, err
	}
	return entry.snapshot().clone(), nil
}

// ListDLQ 列出目前位於死信佇列的事件，依進入 DLQ 的順序排列。
// limit 為非正數時回傳全部。
//
// 這裡必須取讀鎖：走訪 dlq 索引與 records map 的同時，
// 其他 goroutine 可能正在 PublishEvent 寫入同一個 map。Go 的 runtime 會對
// 「並發讀寫 map」直接拋出不可捕獲的 fatal error，不是靜默的資料競爭而已。
// 讀鎖允許多個查詢同時進行，成本極低。
func (e *Engine) ListDLQ(limit int) ([]EventRecord, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	out := make([]EventRecord, 0, min(len(e.dlq), max(limit, 0)))
	for _, id := range e.dlq {
		if limit > 0 && len(out) >= limit {
			break
		}
		entry, ok := e.records[id]
		if !ok {
			// reaper 已回收，索引尚未同步（不該發生，兩者在同一個寫鎖內維護）。
			continue
		}
		// 防禦性過濾：索引理應只含 DEAD_LETTER，這裡再確認一次，
		// 確保任何未來的維護疏漏都不會讓非 DLQ 事件出現在報表上。
		if rec := entry.snapshot(); rec.Status == StatusDeadLetter {
			out = append(out, rec.clone())
		}
	}
	return out, nil
}

// ReplayEvent 把死信事件重置回 PENDING 並重新排入投遞佇列，重試計數歸零。
//
// 狀態流轉先在 entry 鎖內完成，才去取 Engine 鎖維護索引與佇列，
// 兩個鎖不曾同時持有，也未反向取得。並發呼叫時只有第一個能完成流轉，
// 其餘會得到 ErrNotDeadLettered，因此事件不可能被重複排入佇列。
func (e *Engine) ReplayEvent(id EventID) error {
	entry, err := e.lookup(id)
	if err != nil {
		return err
	}

	now := e.clock.Now()
	if err := entry.transition(StatusPending, now, func(rec *EventRecord) {
		rec.RetryCount = 0
		rec.LastError = ""
		rec.LastStatusCode = 0
		rec.DeadLetteredAt = time.Time{}
		// 立刻可投遞，不沿用上次的退避時間。
		rec.NextAttemptAt = now
	}); err != nil {
		return err
	}

	e.mu.Lock()
	if e.closed {
		// 引擎已關閉：狀態已重置供稽核，但不再排入佇列。
		e.mu.Unlock()
		return ErrEngineClosed
	}
	e.removeFromDLQLocked(id)
	entry.nextAttemptAt = now
	heap.Push(&e.queue, entry)
	e.mu.Unlock()

	e.notifyDispatcher()
	return nil
}

// Close 停止接受新事件、結束 dispatcher 與 reaper，並等待 worker 把已交棒的事件做完。
//
// 刻意不中斷進行中的 HTTP 請求：請求已經送出時，強行取消只會讓
// merchant 那邊已處理、我們這邊卻沒有結果——這正是 outbox 最該避免的狀態。
// 單次請求的時間上限由 Config.SendTimeout 控制，整體等待上限由
// Config.ShutdownTimeout 控制；逾時後 Close 直接返回，殘留的 worker 不再被等待。
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

		drained := make(chan struct{})
		go func() {
			e.workerWG.Wait()
			close(drained)
		}()

		timer := e.clock.NewTimer(e.clock.Now().Add(e.cfg.ShutdownTimeout))
		defer timer.Stop()
		select {
		case <-drained:
		case <-timer.C():
			// 逾時：不再等待，但也不強行中斷。狀態留在 IN_FLIGHT 供稽核。
		}

		e.reaperWG.Wait()
	})
}

// lookup 只用讀鎖取出 entry 指標後立刻釋放，後續狀態變更改由 entry 層的鎖負責，
// 因此外部 I/O 與狀態流轉都不會卡住 Engine 這一層。
func (e *Engine) lookup(id EventID) (*eventEntry, error) {
	e.mu.RLock()
	entry, ok := e.records[id]
	e.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return entry, nil
}

// reschedule 以新的到期時間把事件放回佇列（用於重試與併發額度已滿的延後）。
// 排程鍵只在持有 Engine 鎖時寫入，heap 比較因此完全不需要碰 entry 的鎖。
func (e *Engine) reschedule(entry *eventEntry, nextAttemptAt time.Time) {
	e.mu.Lock()
	if e.closed {
		// 引擎已關閉：事件狀態保留供稽核，不再排入佇列。
		e.mu.Unlock()
		return
	}
	entry.nextAttemptAt = nextAttemptAt
	heap.Push(&e.queue, entry)
	e.mu.Unlock()

	e.notifyDispatcher()
}

// enrollDLQ 把事件加進 DLQ 索引。呼叫端必須已完成往 DEAD_LETTER 的流轉。
func (e *Engine) enrollDLQ(id EventID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dlq = append(e.dlq, id)
}

// removeFromDLQLocked 從索引移除指定事件。呼叫端必須持有寫鎖。
//
// 線性搜尋是刻意的取捨：DLQ 的規模應該很小（大量死信本身就是該告警的事），
// 而 replay 與回收都是低頻操作。多維護一個 map 索引換取這點效能，
// 只會增加兩份資料不同步的風險。
func (e *Engine) removeFromDLQLocked(id EventID) {
	for i, existing := range e.dlq {
		if existing == id {
			e.dlq = append(e.dlq[:i], e.dlq[i+1:]...)
			return
		}
	}
}

// reserveInflight 為某 merchant 預留一個併發額度，額度已滿時回 false。
// 預留與歸還刻意成對出現在 dispatcher 與 worker 之間：
// 每個進入交接佇列的事件都持有一個額度，由 worker 在投遞結束後歸還。
func (e *Engine) reserveInflight(merchantID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.inflight[merchantID] >= e.cfg.MaxInflightPerMerchant {
		return false
	}
	e.inflight[merchantID]++
	return true
}

func (e *Engine) releaseInflight(merchantID string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.inflight[merchantID] <= 1 {
		// 歸零就刪 key，否則 merchant 數量一多，這個 map 會持續膨脹。
		delete(e.inflight, merchantID)
		return
	}
	e.inflight[merchantID]--
}

// notifyDispatcher 發出「請重新檢視堆頂」的訊號；訊號可合併，故不阻塞。
func (e *Engine) notifyDispatcher() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// activeGoroutines 回報存活的內部 goroutine 數，供測試驗證關閉後無洩漏。
func (e *Engine) activeGoroutines() int { return int(e.live.Load()) }
