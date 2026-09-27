package outbox

// eventQueue 是依 (nextAttemptAt, sequence) 排序的 min-heap，實作 container/heap.Interface。
//
// 這個佇列是整個重試策略的核心。失敗的事件不是被塞回隊尾，而是以
// 「now + 退避時間」重新入列，因此它的位置由到期時刻決定：
// 新進事件（到期時刻為當下）自然排在退避中的事件之前，而退避中的事件
// 時間到了就會浮上堆頂。這讓「重試 vs 新事件」的優先權不需要任何額外邏輯。
//
// 刻意不維護 heap index：狀態變更採惰性策略，dispatcher 彈出後才檢查狀態並
// 丟棄不該派送的事件。少了 index 維護，Push/Pop/Swap 都不會出錯。
//
// 所有操作都必須在持有 Engine 寫鎖時進行。
type eventQueue []*eventEntry

func (q eventQueue) Len() int { return len(q) }

// Less 以到期時刻為主鍵，序號為決勝鍵。
// 加上序號是為了讓同時到期的事件有穩定順序，否則 heap 的輸出順序不可預測，
// 除錯與測試都會變得困難。
func (q eventQueue) Less(i, j int) bool {
	a, b := q[i], q[j]
	if !a.nextAttemptAt.Equal(b.nextAttemptAt) {
		return a.nextAttemptAt.Before(b.nextAttemptAt)
	}
	return a.rec.Event.Sequence < b.rec.Event.Sequence
}

func (q eventQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }

func (q *eventQueue) Push(x any) {
	*q = append(*q, x.(*eventEntry))
}

func (q *eventQueue) Pop() any {
	old := *q
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // 避免殘留指標妨礙 GC
	*q = old[:n-1]
	return item
}

// peek 回傳最早到期的事件但不移除它。
func (q eventQueue) peek() (*eventEntry, bool) {
	if len(q) == 0 {
		return nil, false
	}
	return q[0], true
}
