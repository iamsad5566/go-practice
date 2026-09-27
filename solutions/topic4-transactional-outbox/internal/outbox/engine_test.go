package outbox

import (
	"errors"
	"testing"
	"time"
)

func TestNewRejectsInvalidConfig(t *testing.T) {
	if _, err := New(Config{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New(Config{}) = %v, 預期 ErrInvalidConfig", err)
	}
}

func TestPublishEventGeneratesID(t *testing.T) {
	eng := newTestEngine(t, newFakeClock(time.Now()), newFakeSender(), nil)

	id, err := eng.PublishEvent(validEvent())
	if err != nil {
		t.Fatalf("PublishEvent() 失敗: %v", err)
	}
	if id == "" {
		t.Fatal("PublishEvent() 回傳空的 EventID")
	}
	if _, err := eng.GetEventStatus(id); err != nil {
		t.Fatalf("剛發布的事件查不到: %v", err)
	}
}

// 呼叫端指定的 EventID 必須被沿用——那是它在業務交易裡產生的唯一鍵。
func TestPublishEventKeepsCallerID(t *testing.T) {
	eng := newTestEngine(t, newFakeClock(time.Now()), newFakeSender(), nil)

	ev := validEvent()
	ev.EventID = "order-42-settled"
	id, err := eng.PublishEvent(ev)
	if err != nil {
		t.Fatalf("PublishEvent() 失敗: %v", err)
	}
	if id != "order-42-settled" {
		t.Fatalf("EventID = %q, 預期沿用呼叫端指定的值", id)
	}
}

// 重複發布同一個 EventID 必須被拒絕：這是 outbox 的第一道去重閘，
// 讓業務端的重試（例如交易重跑）不會造成兩次 webhook 投遞。
func TestPublishEventRejectsDuplicateID(t *testing.T) {
	eng := newTestEngine(t, newFakeClock(time.Now()), newFakeSender(), nil)

	ev := validEvent()
	ev.EventID = "order-42-settled"
	if _, err := eng.PublishEvent(ev); err != nil {
		t.Fatalf("第一次發布失敗: %v", err)
	}
	if _, err := eng.PublishEvent(ev); !errors.Is(err, ErrDuplicateEvent) {
		t.Fatalf("第二次發布 = %v, 預期 ErrDuplicateEvent", err)
	}
}

func TestPublishEventValidates(t *testing.T) {
	eng := newTestEngine(t, newFakeClock(time.Now()), newFakeSender(), nil)

	ev := validEvent()
	ev.MerchantID = ""
	if _, err := eng.PublishEvent(ev); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("PublishEvent() = %v, 預期 ErrInvalidEvent", err)
	}
}

func TestPublishEventAfterCloseIsRejected(t *testing.T) {
	eng := newTestEngine(t, newFakeClock(time.Now()), newFakeSender(), nil)
	eng.Close()

	if _, err := eng.PublishEvent(validEvent()); !errors.Is(err, ErrEngineClosed) {
		t.Fatalf("PublishEvent() = %v, 預期 ErrEngineClosed", err)
	}
}

func TestGetEventStatusNotFound(t *testing.T) {
	eng := newTestEngine(t, newFakeClock(time.Now()), newFakeSender(), nil)

	if _, err := eng.GetEventStatus("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetEventStatus() = %v, 預期 ErrNotFound", err)
	}
}

// 回傳的必須是快照：呼叫端改動它不得影響引擎內部狀態。
func TestGetEventStatusReturnsSnapshot(t *testing.T) {
	sender := newFakeSender()
	eng := newTestEngine(t, newFakeClock(time.Now()), sender, nil)

	id, _ := eng.PublishEvent(validEvent())
	waitForStatus(t, eng, id, StatusDelivered)

	rec, _ := eng.GetEventStatus(id)
	rec.Status = StatusDeadLetter
	rec.Event.Payload[0] = 'X'

	again, _ := eng.GetEventStatus(id)
	if again.Status != StatusDelivered {
		t.Errorf("內部狀態被外部快照改動了: %s", again.Status)
	}
	if again.Event.Payload[0] != '{' {
		t.Error("內部 payload 被外部快照改動了")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	eng := newTestEngine(t, newFakeClock(time.Now()), newFakeSender(), nil)

	eng.Close()
	eng.Close() // 重複呼叫不得 panic 或阻塞
}

// Close 必須讓進行中的投遞跑完，不可中途丟棄——事件已經送出去了，
// 本地卻沒記錄結果，是 outbox 最不該出現的狀態。
func TestCloseWaitsForInFlightDelivery(t *testing.T) {
	sender := newFakeSender()
	sender.block = make(chan struct{})
	sender.started = make(chan EventID, 1)
	eng := newTestEngine(t, newFakeClock(time.Now()), sender, nil)

	id, _ := eng.PublishEvent(validEvent())
	<-sender.started // 確認投遞已開始

	closed := make(chan struct{})
	go func() {
		eng.Close()
		close(closed)
	}()

	select {
	case <-closed:
		t.Fatal("Close 在投遞完成前就返回了")
	case <-time.After(50 * time.Millisecond):
	}

	close(sender.block)
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close 在投遞完成後仍未返回")
	}

	rec, err := eng.GetEventStatus(id)
	if err != nil {
		t.Fatalf("關閉後查詢失敗: %v", err)
	}
	if rec.Status != StatusDelivered {
		t.Fatalf("Status = %s, 預期關閉前就記錄完投遞結果 (%s)", rec.Status, StatusDelivered)
	}
}

// 引擎不得洩漏 goroutine：Close 之後 dispatcher、worker、reaper 都要收工。
func TestCloseLeavesNoGoroutines(t *testing.T) {
	sender := newFakeSender()
	eng := newTestEngine(t, newFakeClock(time.Now()), sender, func(cfg *Config) {
		cfg.Workers = 4
	})

	for i := 0; i < 20; i++ {
		if _, err := eng.PublishEvent(validEvent()); err != nil {
			t.Fatalf("PublishEvent() 失敗: %v", err)
		}
	}
	eng.Close()

	if n := eng.activeGoroutines(); n != 0 {
		t.Fatalf("關閉後仍有 %d 個 goroutine 存活", n)
	}
}
