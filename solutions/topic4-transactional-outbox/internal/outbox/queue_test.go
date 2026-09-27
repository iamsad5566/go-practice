package outbox

import (
	"container/heap"
	"testing"
	"time"
)

func entryDueAt(id EventID, seq uint64, dueAt time.Time) *eventEntry {
	e := newEntry(validEvent(), id, seq, dueAt)
	e.nextAttemptAt = dueAt
	return e
}

// 佇列必須依 NextAttemptAt 排序，而不是入列順序：
// 這正是「退避中的事件不插隊、時間到了才輪到它」的實作基礎。
func TestEventQueueOrdersByNextAttemptAt(t *testing.T) {
	base := time.Now()
	var q eventQueue

	// 刻意以亂序入列。
	heap.Push(&q, entryDueAt("third", 3, base.Add(3*time.Second)))
	heap.Push(&q, entryDueAt("first", 1, base.Add(time.Second)))
	heap.Push(&q, entryDueAt("second", 2, base.Add(2*time.Second)))

	want := []EventID{"first", "second", "third"}
	for _, id := range want {
		got := heap.Pop(&q).(*eventEntry)
		if got.rec.Event.EventID != id {
			t.Fatalf("出列順序錯誤: 得到 %s, 預期 %s", got.rec.Event.EventID, id)
		}
	}
	if q.Len() != 0 {
		t.Errorf("佇列應已清空, 剩下 %d 筆", q.Len())
	}
}

// 同一時刻到期時以序號決勝，讓派送順序穩定可預測（便於除錯與測試）。
func TestEventQueueBreaksTiesBySequence(t *testing.T) {
	due := time.Now()
	var q eventQueue

	heap.Push(&q, entryDueAt("late", 9, due))
	heap.Push(&q, entryDueAt("early", 2, due))

	if got := heap.Pop(&q).(*eventEntry); got.rec.Event.EventID != "early" {
		t.Fatalf("同時到期應以較小序號優先, 得到 %s", got.rec.Event.EventID)
	}
}

func TestEventQueuePeek(t *testing.T) {
	var q eventQueue

	if _, ok := q.peek(); ok {
		t.Error("空佇列的 peek 應回 false")
	}

	base := time.Now()
	heap.Push(&q, entryDueAt("later", 2, base.Add(time.Minute)))
	heap.Push(&q, entryDueAt("sooner", 1, base))

	top, ok := q.peek()
	if !ok {
		t.Fatal("peek 應回 true")
	}
	if top.rec.Event.EventID != "sooner" {
		t.Errorf("peek = %s, 預期 sooner", top.rec.Event.EventID)
	}
	// peek 不得移除元素。
	if q.Len() != 2 {
		t.Errorf("peek 後長度 = %d, 預期 2", q.Len())
	}
}

// Pop 必須清掉底層陣列殘留的指標，否則已出列的事件會被佇列一直引用而無法回收。
func TestEventQueuePopClearsSlot(t *testing.T) {
	q := eventQueue{entryDueAt("only", 1, time.Now())}
	heap.Pop(&q)

	if cap(q) > 0 && q[:cap(q)][0] != nil {
		t.Fatal("Pop 後底層陣列仍殘留指標，會妨礙 GC")
	}
}
