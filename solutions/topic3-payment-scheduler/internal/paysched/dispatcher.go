package paysched

import (
	"container/heap"
	"time"
)

// runDispatcher 是唯一的排程協調者。
//
// 不論排在佇列裡的付款有幾十萬筆，這裡只有一個 goroutine 與一個 time.Timer：
// 計時器永遠對齊「最早到期的那一筆」，新排入更早的付款時由 wake 訊號喚醒並重設。
// 這正是 PRD 要求避免的「timer 爆炸」——絕不為每筆付款各開一個計時器。
func (e *Engine) runDispatcher() {
	defer e.dispatcherWG.Done()

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

		// wakeAt 為零值代表佇列為空，只需等待新排程或關閉訊號，連計時器都不用掛。
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
			// 可能排入了更早到期的付款，重新計算等待時間。
		case <-e.stop:
			return
		}
	}
}

// takeDue 在 Engine 寫鎖內完成全部工作：彈出一筆已到期的付款，或回報下一次該喚醒的時刻。
// 臨界區只有 heap 操作，沒有任何 I/O。
//
// 回傳值：(已到期的付款, 零值) 或 (nil, 下次喚醒時刻)；佇列為空時回 (nil, 零值)。
// 喚醒時刻刻意以絕對時間回傳，避免在計算與掛上計時器之間時間前進而錯過喚醒。
func (e *Engine) takeDue() (*payment, time.Time) {
	now := e.clock.Now()

	e.mu.Lock()
	defer e.mu.Unlock()

	top, ok := e.queue.peek()
	if !ok {
		return nil, time.Time{}
	}
	if top.dueAt.After(now) {
		return nil, top.dueAt
	}
	return heap.Pop(&e.queue).(*payment), time.Time{}
}

// handOff 把到期付款交給 worker。回傳 false 代表引擎正在關閉，dispatcher 應退出。
//
// 佇列滿時在此阻塞而非丟棄任務——金流系統不允許靜默掉單；
// 背壓因此自然地反映到排程速度上。
func (e *Engine) handOff(p *payment) bool {
	// 惰性取消：已取消或已終結的付款直接丟棄，連交接都不必。
	if p.status().IsTerminal() {
		return true
	}

	select {
	case e.dispatch <- p:
		return true
	case <-e.stop:
		return false
	}
}
