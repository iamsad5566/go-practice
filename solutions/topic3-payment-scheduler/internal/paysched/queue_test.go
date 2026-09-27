package paysched

import (
	"container/heap"
	"testing"
	"time"
)

// 排序鍵為 (dueAt, priority, seq)：時間永遠是第一排序鍵，
// 高優先權不得提前於約定時間送出；同時刻才比優先權，最後以 seq 保證 FIFO 且可重現。
func TestQueueOrdering(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	newItem := func(offset time.Duration, p Priority, seq uint64) *payment {
		return &payment{dueAt: base.Add(offset), priority: p, seq: seq}
	}

	// 故意以亂序推入
	items := []*payment{
		newItem(2*time.Second, PriorityHigh, 4),
		newItem(time.Second, PriorityLow, 1),
		newItem(time.Second, PriorityHigh, 3),
		newItem(time.Second, PriorityHigh, 2),
		newItem(3*time.Second, PriorityNormal, 5),
	}

	q := &paymentQueue{}
	for _, it := range items {
		heap.Push(q, it)
	}

	wantSeq := []uint64{2, 3, 1, 4, 5}
	for i, want := range wantSeq {
		got := heap.Pop(q).(*payment)
		if got.seq != want {
			t.Fatalf("第 %d 個彈出的 seq = %d, want %d", i, got.seq, want)
		}
	}
	if q.Len() != 0 {
		t.Fatalf("佇列應已清空, 剩 %d", q.Len())
	}
}

func TestQueuePeek(t *testing.T) {
	q := &paymentQueue{}
	if _, ok := q.peek(); ok {
		t.Fatal("空佇列 peek 應回 false")
	}

	base := time.Now()
	heap.Push(q, &payment{dueAt: base.Add(time.Minute), seq: 1})
	heap.Push(q, &payment{dueAt: base, seq: 2})

	top, ok := q.peek()
	if !ok || top.seq != 2 {
		t.Fatalf("peek = %v(ok=%v), want seq 2", top, ok)
	}
	if q.Len() != 2 {
		t.Fatal("peek 不應移除元素")
	}
}
