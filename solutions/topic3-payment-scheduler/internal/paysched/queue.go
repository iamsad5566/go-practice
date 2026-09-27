package paysched

// paymentQueue 是依 (dueAt, priority, seq) 排序的 min-heap，實作 container/heap.Interface。
//
// 刻意不維護 heap index，因為取消採惰性策略：取消只改 payment 狀態，
// dispatcher 彈出後看到終態就直接丟棄。少了 index 維護，Push/Pop/Swap 都不會出錯。
//
// 所有操作都必須在持有 Engine 寫鎖時進行。
type paymentQueue []*payment

func (q paymentQueue) Len() int { return len(q) }

// Less 的排序鍵順序是刻意的：
// 時間永遠優先於優先權，否則高優先權付款會早於約定時間送出，違反定時付款的語意。
func (q paymentQueue) Less(i, j int) bool {
	a, b := q[i], q[j]
	if !a.dueAt.Equal(b.dueAt) {
		return a.dueAt.Before(b.dueAt)
	}
	if a.priority != b.priority {
		return a.priority < b.priority
	}
	return a.seq < b.seq
}

func (q paymentQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }

func (q *paymentQueue) Push(x any) {
	*q = append(*q, x.(*payment))
}

func (q *paymentQueue) Pop() any {
	old := *q
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // 避免殘留指標妨礙 GC
	*q = old[:n-1]
	return item
}

// peek 回傳最早到期的付款但不移除它。
func (q paymentQueue) peek() (*payment, bool) {
	if len(q) == 0 {
		return nil, false
	}
	return q[0], true
}
