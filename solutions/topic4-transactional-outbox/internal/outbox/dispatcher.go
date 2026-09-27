package outbox

import (
	"container/heap"
	"time"
)

// runDispatcher 是唯一的派送協調者。
//
// 由「單一 goroutine 負責出隊」是防雙重派送的第一道閘（PRD 非功能需求 2）：
// worker 從不自己搶佇列，因此兩個 worker 根本不可能撿到同一筆事件。
// worker 端的 CAS 是第二道閘，防的是 replay 之類的其他來源。
//
// 不論有幾十萬筆事件在退避中，這裡只有一個 goroutine 與一個計時器：
// 計時器永遠對齊「最早到期的那一筆」，新事件排入時由 wake 訊號喚醒並重設。
func (e *Engine) runDispatcher() {
	defer e.dispatcherWG.Done()
	defer e.live.Add(-1)

	// 計時器延遲到真正需要等待時才建立，並在整個 dispatcher 生命週期中重複使用，
	// 因此不會有任何未回收的計時器。
	var timer Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		due, wakeAt := e.takeDue()

		if due != nil {
			if !e.handOff(due) {
				return
			}
			continue
		}

		// wakeAt 為零值代表佇列為空，只需等待新事件或關閉訊號，連計時器都不用掛。
		if wakeAt.IsZero() {
			select {
			case <-e.wake:
			case <-e.stop:
				return
			}
			continue
		}

		if timer == nil {
			timer = e.clock.NewTimer(wakeAt)
		} else {
			timer.Reset(wakeAt)
		}

		select {
		case <-timer.C():
		case <-e.wake:
			// 可能排入了更早到期的事件，重新計算等待時間。
		case <-e.stop:
			return
		}
	}
}

// takeDue 在 Engine 寫鎖內完成全部工作：彈出一筆已到期的事件，
// 或回報下一次該喚醒的時刻。臨界區只有 heap 操作，沒有任何 I/O。
//
// 回傳值：(已到期的事件, 零值) 或 (nil, 下次喚醒時刻)；佇列為空時回 (nil, 零值)。
// 喚醒時刻刻意以絕對時間回傳，避免在計算與掛上計時器之間時間前進而錯過喚醒。
func (e *Engine) takeDue() (*eventEntry, time.Time) {
	now := e.clock.Now()

	e.mu.Lock()
	defer e.mu.Unlock()

	top, ok := e.queue.peek()
	if !ok {
		return nil, time.Time{}
	}
	if top.nextAttemptAt.After(now) {
		return nil, top.nextAttemptAt
	}
	return heap.Pop(&e.queue).(*eventEntry), time.Time{}
}

// handOff 把到期事件交給 worker。回傳 false 代表引擎正在關閉，dispatcher 應退出。
//
// 佇列滿時在此阻塞而非丟棄事件——outbox 的存在意義就是不掉事件；
// 背壓因此自然地反映到派送速度上，而非反映到事件發布上（PublishEvent 永不阻塞）。
func (e *Engine) handOff(entry *eventEntry) bool {
	// 惰性丟棄：狀態已不可派送（已送達、已進 DLQ、或已被 claim）就直接丟掉。
	// 有了這個檢查，取消與回收都不必從 heap 中間移除元素，
	// entry 也就不需要維護 heap index。
	if !entry.status().IsDispatchable() {
		return true
	}

	// 併發額度在交棒前預留，而不是等 worker 拿到才檢查。
	//
	// 這個順序是關鍵：額度滿的事件連交接佇列都不該進入，否則它會佔著
	// 佇列位置，把後面其他 merchant 的事件一起卡住——毒藥端點的傷害
	// 就從「吃掉 worker」變成「吃掉整個佇列」。
	merchantID := entry.snapshot().Event.MerchantID
	if !e.reserveInflight(merchantID) {
		// 稍後再試。這裡不動 RetryCount：併發受限是我們自己的節流，
		// 不是投遞失敗，不該消耗事件的重試額度，否則一次尖峰就會把
		// 本來會成功的事件推進 DLQ。
		e.reschedule(entry, e.clock.Now().Add(e.cfg.MerchantBusyBackoff))
		return true
	}

	select {
	case e.dispatch <- entry:
		return true
	case <-e.stop:
		// 交棒失敗，歸還額度，避免關閉過程留下不一致的計數。
		e.releaseInflight(merchantID)
		return false
	}
}
