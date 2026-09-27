package outbox

import "time"

// reapCount 回報回收器已完成的掃描輪數，供測試確認回收確實跑過。
func (e *Engine) reapCount() int { return int(e.reapRounds.Load()) }

// runReaper 定期回收超過保留期的終態事件。
//
// 沒有這個迴圈，成功送達的事件會永久留在 records map 裡，
// 記憶體只增不減——對一個長期運行的通知服務而言這是必然的記憶體洩漏。
// 保留一段時間而非投遞完就刪，是為了讓 GetEventStatus 在事件送達後仍查得到結果。
//
// 注意 DEAD_LETTER 同樣受保留期約束：DLQ 不是永久儲存，
// 死信必須在保留期內被 replay 或匯出，否則一併回收。
func (e *Engine) runReaper() {
	defer e.reaperWG.Done()
	defer e.live.Add(-1)

	timer := e.clock.NewTimer(e.clock.Now().Add(e.cfg.ReapInterval))
	defer timer.Stop()

	for {
		select {
		case <-timer.C():
			e.reap(e.clock.Now())
			e.reapRounds.Add(1)
			timer.Reset(e.clock.Now().Add(e.cfg.ReapInterval))
		case <-e.stop:
			return
		}
	}
}

// reap 刪除在 cutoff 之前就進入終態的事件。
//
// 刻意分成兩個階段：先在讀鎖下挑出候選，再在寫鎖下逐一複查並刪除。
// 走訪整個 map 是 O(n)，若整段都持有寫鎖，事件發布會被回收掃描卡住，
// 直接違反「發布不被阻塞」的要求；讀鎖階段則允許查詢與其他讀取繼續進行。
//
// 規模再大時的正解是另外維護一個依終態時刻排序的到期索引，
// 讓回收只需看堆頂而不必掃全表。這裡選擇不預先實作：
// 那份索引必須與狀態流轉同步維護，複雜度明顯上升，
// 而本引擎的定位是記憶體內原型。
func (e *Engine) reap(now time.Time) int {
	cutoff := now.Add(-e.cfg.Retention)

	e.mu.RLock()
	candidates := make([]EventID, 0, 16)
	for id, entry := range e.records {
		if rec := entry.snapshot(); isExpired(rec, cutoff) {
			candidates = append(candidates, id)
		}
	}
	e.mu.RUnlock()

	if len(candidates) == 0 {
		return 0
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	reaped := 0
	for _, id := range candidates {
		entry, ok := e.records[id]
		if !ok {
			continue
		}
		// 複查：兩階段之間事件可能已被 replay 回 PENDING，那就不能刪。
		if !isExpired(entry.snapshot(), cutoff) {
			continue
		}
		delete(e.records, id)
		e.removeFromDLQLocked(id)
		reaped++
	}
	return reaped
}

// isExpired 判斷終態事件是否已超過保留期。
// 以進入終態的時刻為基準，而非建立時刻：一個退避了很久才送達的事件，
// 保留期應該從它送達那一刻起算。
func isExpired(rec EventRecord, cutoff time.Time) bool {
	if !rec.Status.IsTerminal() {
		return false
	}
	settledAt := rec.DeliveredAt
	if settledAt.IsZero() {
		settledAt = rec.DeadLetteredAt
	}
	if settledAt.IsZero() {
		settledAt = rec.UpdatedAt
	}
	return settledAt.Before(cutoff)
}
