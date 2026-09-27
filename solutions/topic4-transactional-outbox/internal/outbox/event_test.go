package outbox

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func validEvent() OutboxEvent {
	return OutboxEvent{
		MerchantID: "merchant-1",
		EventType:  "payment.settled",
		Payload:    []byte(`{"amount":100}`),
		DestURL:    "https://merchant.example.com/webhooks",
	}
}

func TestValidateEvent(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*OutboxEvent)
		wantErr error
	}{
		{"合法事件", func(*OutboxEvent) {}, nil},
		{"缺 MerchantID", func(e *OutboxEvent) { e.MerchantID = "" }, ErrInvalidEvent},
		{"缺 EventType", func(e *OutboxEvent) { e.EventType = "" }, ErrInvalidEvent},
		{"缺 DestURL", func(e *OutboxEvent) { e.DestURL = "" }, ErrInvalidEvent},
		{"DestURL 非 http(s)", func(e *OutboxEvent) { e.DestURL = "ftp://x/y" }, ErrInvalidEvent},
		{"DestURL 無法解析", func(e *OutboxEvent) { e.DestURL = "ht tp://x" }, ErrInvalidEvent},
		{"Payload 為 nil", func(e *OutboxEvent) { e.Payload = nil }, ErrInvalidEvent},
		// 空的 JSON 物件是合法負載，不該被當成缺漏。
		{"Payload 為空物件", func(e *OutboxEvent) { e.Payload = []byte("{}") }, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev := validEvent()
			c.mutate(&ev)
			err := validateEvent(ev)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("validateEvent() = %v, 預期 %v", err, c.wantErr)
			}
		})
	}
}

// Payload 必須被複製一份，否則呼叫端事後改動 slice 會讓已簽章的內容與送出的不一致。
func TestNewEntryClonesPayload(t *testing.T) {
	ev := validEvent()
	now := time.Now()
	entry := newEntry(ev, "evt-1", 1, now)

	ev.Payload[0] = 'X'
	if got := entry.snapshot().Event.Payload[0]; got != '{' {
		t.Fatalf("entry 的 payload 被外部改動了：第一個位元組 = %q", got)
	}
}

func TestNewEntryInitialState(t *testing.T) {
	now := time.Now()
	entry := newEntry(validEvent(), "evt-1", 7, now)
	rec := entry.snapshot()

	if rec.Status != StatusPending {
		t.Errorf("Status = %s, 預期 %s", rec.Status, StatusPending)
	}
	if rec.Event.EventID != "evt-1" {
		t.Errorf("EventID = %q, 預期 evt-1", rec.Event.EventID)
	}
	if rec.Event.Sequence != 7 {
		t.Errorf("Sequence = %d, 預期 7", rec.Event.Sequence)
	}
	if !rec.Event.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, 預期 %v", rec.Event.CreatedAt, now)
	}
	// 首次投遞不必等待，NextAttemptAt 應為建立時刻。
	if !rec.NextAttemptAt.Equal(now) {
		t.Errorf("NextAttemptAt = %v, 預期 %v", rec.NextAttemptAt, now)
	}
	if rec.RetryCount != 0 {
		t.Errorf("RetryCount = %d, 預期 0", rec.RetryCount)
	}
}

// 呼叫端指定的 CreatedAt 應被保留，讓 outbox 能反映業務事件的真實發生時間。
func TestNewEntryKeepsCallerCreatedAt(t *testing.T) {
	ev := validEvent()
	ev.CreatedAt = time.Now().Add(-time.Hour)
	entry := newEntry(ev, "evt-1", 1, time.Now())

	if !entry.snapshot().Event.CreatedAt.Equal(ev.CreatedAt) {
		t.Fatalf("CreatedAt 被覆寫成 %v，預期保留 %v", entry.snapshot().Event.CreatedAt, ev.CreatedAt)
	}
}

func TestEntryTransitionAppliesChanges(t *testing.T) {
	now := time.Now()
	entry := newEntry(validEvent(), "evt-1", 1, now)

	later := now.Add(time.Second)
	if err := entry.transition(StatusInFlight, later, nil); err != nil {
		t.Fatalf("claim 失敗: %v", err)
	}
	if err := entry.transition(StatusRetrying, later, func(rec *EventRecord) {
		rec.RetryCount++
		rec.LastError = "boom"
		rec.LastStatusCode = 503
	}); err != nil {
		t.Fatalf("退回重試失敗: %v", err)
	}

	rec := entry.snapshot()
	if rec.Status != StatusRetrying || rec.RetryCount != 1 || rec.LastError != "boom" || rec.LastStatusCode != 503 {
		t.Fatalf("流轉後的 record 不符預期: %+v", rec)
	}
	if !rec.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, 預期 %v", rec.UpdatedAt, later)
	}
}

// 非法流轉必須讓 record 完全不變，否則失敗的 CAS 會留下髒資料。
func TestEntryTransitionRejectedLeavesRecordIntact(t *testing.T) {
	now := time.Now()
	entry := newEntry(validEvent(), "evt-1", 1, now)
	before := entry.snapshot()

	err := entry.transition(StatusDelivered, now.Add(time.Second), func(rec *EventRecord) {
		rec.RetryCount = 999
	})
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("transition() = %v, 預期 ErrInvalidTransition", err)
	}
	// EventRecord 內含 Payload []byte，不可用 == 比較。
	if !reflect.DeepEqual(entry.snapshot(), before) {
		t.Fatalf("record 在非法流轉後被改動了: %+v", entry.snapshot())
	}
}

// claim 是防雙重派送的最後一道閘：同一事件只有第一個 worker 能成功。
func TestEntryClaimOnlyOnce(t *testing.T) {
	now := time.Now()
	entry := newEntry(validEvent(), "evt-1", 1, now)

	if err := entry.transition(StatusInFlight, now, nil); err != nil {
		t.Fatalf("第一次 claim 應成功，卻得到 %v", err)
	}
	if err := entry.transition(StatusInFlight, now, nil); !errors.Is(err, ErrInFlight) {
		t.Fatalf("第二次 claim = %v, 預期 ErrInFlight", err)
	}
}
