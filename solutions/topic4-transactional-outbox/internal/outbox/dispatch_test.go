package outbox

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDeliverySuccess(t *testing.T) {
	sender := newFakeSender()
	eng := newTestEngine(t, newFakeClock(time.Now()), sender, nil)

	id, _ := eng.PublishEvent(validEvent())
	rec := waitForStatus(t, eng, id, StatusDelivered)

	if rec.RetryCount != 0 {
		t.Errorf("RetryCount = %d, 預期 0", rec.RetryCount)
	}
	if rec.LastStatusCode != 200 {
		t.Errorf("LastStatusCode = %d, 預期 200", rec.LastStatusCode)
	}
	if rec.DeliveredAt.IsZero() {
		t.Error("DeliveredAt 未被填入")
	}
	if n := sender.attemptsFor(id); n != 1 {
		t.Errorf("投遞次數 = %d, 預期恰好 1 次", n)
	}
}

// 實際送出的請求必須帶齊 PRD 要求的所有 header，且簽章可被 merchant 驗證。
func TestDeliveryRequestContents(t *testing.T) {
	sender := newFakeSender()
	clock := newFakeClock(time.Unix(1700000000, 0))
	eng := newTestEngine(t, clock, sender, nil)

	ev := validEvent()
	ev.EventID = "order-42-settled"
	id, _ := eng.PublishEvent(ev)
	waitForStatus(t, eng, id, StatusDelivered)

	attempts := sender.snapshotAttempts()
	if len(attempts) != 1 {
		t.Fatalf("嘗試次數 = %d, 預期 1", len(attempts))
	}
	got := attempts[0]

	if got.DestURL != ev.DestURL {
		t.Errorf("DestURL = %q, 預期 %q", got.DestURL, ev.DestURL)
	}
	if string(got.Payload) != string(ev.Payload) {
		t.Errorf("Payload = %q, 預期 %q", got.Payload, ev.Payload)
	}
	for _, h := range []string{headerDeliveryID, headerEventID, headerEventType, headerTimestamp, headerSignature, headerSequence} {
		if got.Headers[h] == "" {
			t.Errorf("header %s 缺失", h)
		}
	}
	if got.Headers[headerEventID] != "order-42-settled" {
		t.Errorf("%s = %q, 預期 order-42-settled", headerEventID, got.Headers[headerEventID])
	}

	// 站在 merchant 的位置驗一次簽章，確認端到端真的對得上。
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write([]byte(got.Headers[headerTimestamp] + "." + string(got.Payload)))
	if want := hex.EncodeToString(mac.Sum(nil)); got.Headers[headerSignature] != want {
		t.Errorf("簽章無法被 merchant 驗證: %q, 預期 %q", got.Headers[headerSignature], want)
	}
}

// 5xx 應退避重試，退避結束後成功送達。
func TestDeliveryRetriesOnServerError(t *testing.T) {
	sender := newFakeSender()
	sender.respond = func(_ EventID, attempt int) (HTTPResponse, error) {
		if attempt == 1 {
			return HTTPResponse{StatusCode: 503, Body: []byte("deploying")}, nil
		}
		return HTTPResponse{StatusCode: 200}, nil
	}
	clock := newFakeClock(time.Now())
	eng := newTestEngine(t, clock, sender, nil)

	id, _ := eng.PublishEvent(validEvent())
	rec := waitForStatus(t, eng, id, StatusRetrying)
	if rec.RetryCount != 1 {
		t.Errorf("RetryCount = %d, 預期 1", rec.RetryCount)
	}
	if rec.LastStatusCode != 503 {
		t.Errorf("LastStatusCode = %d, 預期 503", rec.LastStatusCode)
	}
	if rec.LastError == "" {
		t.Error("LastError 應記錄失敗原因")
	}

	clock.Advance(time.Second)
	rec = waitForStatus(t, eng, id, StatusDelivered)
	if rec.RetryCount != 1 {
		t.Errorf("送達後 RetryCount = %d, 預期保留 1 供稽核", rec.RetryCount)
	}
	if n := sender.attemptsFor(id); n != 2 {
		t.Errorf("投遞次數 = %d, 預期 2", n)
	}
}

// 網路層錯誤（連線被拒、逾時）同樣屬於可重試。
func TestDeliveryRetriesOnNetworkError(t *testing.T) {
	sender := newFakeSender()
	sender.respond = func(_ EventID, attempt int) (HTTPResponse, error) {
		if attempt == 1 {
			return HTTPResponse{}, errors.New("connection refused")
		}
		return HTTPResponse{StatusCode: 200}, nil
	}
	clock := newFakeClock(time.Now())
	eng := newTestEngine(t, clock, sender, nil)

	id, _ := eng.PublishEvent(validEvent())
	rec := waitForStatus(t, eng, id, StatusRetrying)
	if rec.LastStatusCode != 0 {
		t.Errorf("網路層失敗的 LastStatusCode = %d, 預期 0", rec.LastStatusCode)
	}

	clock.Advance(time.Second)
	waitForStatus(t, eng, id, StatusDelivered)
}

// 退避間隔必須隨次數指數成長，且每次都是「以新的到期時刻重排」而非立刻重送。
func TestDeliveryBackoffGrowsExponentially(t *testing.T) {
	sender := newFakeSender()
	sender.respond = func(EventID, int) (HTTPResponse, error) {
		return HTTPResponse{StatusCode: 503}, nil
	}
	clock := newFakeClock(time.Now())
	eng := newTestEngine(t, clock, sender, func(cfg *Config) { cfg.MaxRetries = 3 })

	id, _ := eng.PublishEvent(validEvent())

	// Rand 取上界，因此退避恰為 1s、2s、4s。
	for i, wantBackoff := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		rec := waitForRetryCount(t, eng, id, i+1)
		if got := rec.NextAttemptAt.Sub(clock.Now()); got != wantBackoff {
			t.Fatalf("第 %d 輪退避 = %v, 預期 %v", i+1, got, wantBackoff)
		}
		clock.Advance(wantBackoff)
	}

	rec := waitForStatus(t, eng, id, StatusDeadLetter)
	if !containsSentinel(rec.LastError, ErrRetriesExhausted) {
		t.Errorf("LastError = %q, 預期包含重試耗盡的原因", rec.LastError)
	}
	// 首次 + 3 次重試 = 4 次嘗試。
	if n := sender.attemptsFor(id); n != 4 {
		t.Errorf("投遞次數 = %d, 預期 4", n)
	}
}

// 不可重試的 4xx 必須立刻進 DLQ，不得浪費重試額度。
func TestDeliveryDeadLettersOnNonRetryableStatus(t *testing.T) {
	sender := newFakeSender()
	sender.respond = func(EventID, int) (HTTPResponse, error) {
		return HTTPResponse{StatusCode: 401, Body: []byte("bad signature")}, nil
	}
	eng := newTestEngine(t, newFakeClock(time.Now()), sender, nil)

	id, _ := eng.PublishEvent(validEvent())
	rec := waitForStatus(t, eng, id, StatusDeadLetter)

	if rec.RetryCount != 0 {
		t.Errorf("RetryCount = %d, 預期 0（不該重試）", rec.RetryCount)
	}
	if rec.LastStatusCode != 401 {
		t.Errorf("LastStatusCode = %d, 預期 401", rec.LastStatusCode)
	}
	if !containsSentinel(rec.LastError, ErrNonRetryableStatus) {
		t.Errorf("LastError = %q, 預期標示為不可重試", rec.LastError)
	}
	if rec.DeadLetteredAt.IsZero() {
		t.Error("DeadLetteredAt 未被填入")
	}
	if n := sender.attemptsFor(id); n != 1 {
		t.Errorf("投遞次數 = %d, 預期恰好 1 次", n)
	}
}

// 密鑰缺失是設定問題：必須直接進 DLQ，而且連一次請求都不該發出
// ——沒有簽章的 webhook 送出去只會被 merchant 拒絕，還洩漏了負載。
func TestDeliveryDeadLettersWhenSecretMissing(t *testing.T) {
	sender := newFakeSender()
	eng := newTestEngine(t, newFakeClock(time.Now()), sender, nil)

	ev := validEvent()
	ev.MerchantID = "merchant-without-secret"
	id, _ := eng.PublishEvent(ev)

	rec := waitForStatus(t, eng, id, StatusDeadLetter)
	if !containsSentinel(rec.LastError, ErrSecretNotFound) {
		t.Errorf("LastError = %q, 預期包含密鑰缺失", rec.LastError)
	}
	if n := sender.totalAttempts(); n != 0 {
		t.Errorf("發出了 %d 次請求, 預期 0 次", n)
	}
}

func TestListDLQ(t *testing.T) {
	sender := newFakeSender()
	sender.respond = func(EventID, int) (HTTPResponse, error) {
		return HTTPResponse{StatusCode: 400}, nil
	}
	eng := newTestEngine(t, newFakeClock(time.Now()), sender, nil)

	ids := make([]EventID, 3)
	for i := range ids {
		ev := validEvent()
		ev.EventID = EventID("evt-" + string(rune('a'+i)))
		ids[i], _ = eng.PublishEvent(ev)
		waitForStatus(t, eng, ids[i], StatusDeadLetter)
	}

	all, err := eng.ListDLQ(0)
	if err != nil {
		t.Fatalf("ListDLQ() 失敗: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("ListDLQ(0) 回傳 %d 筆, 預期 3", len(all))
	}
	// 依進入 DLQ 的順序排列，最舊的在前，方便維運由頭處理。
	for i, rec := range all {
		if rec.Event.EventID != ids[i] {
			t.Errorf("第 %d 筆 = %s, 預期 %s", i, rec.Event.EventID, ids[i])
		}
		if rec.Status != StatusDeadLetter {
			t.Errorf("第 %d 筆狀態 = %s, 預期 %s", i, rec.Status, StatusDeadLetter)
		}
	}

	limited, _ := eng.ListDLQ(2)
	if len(limited) != 2 {
		t.Errorf("ListDLQ(2) 回傳 %d 筆, 預期 2", len(limited))
	}
}

// 尚未死信的事件不該出現在 DLQ 裡。
func TestListDLQExcludesHealthyEvents(t *testing.T) {
	sender := newFakeSender()
	eng := newTestEngine(t, newFakeClock(time.Now()), sender, nil)

	id, _ := eng.PublishEvent(validEvent())
	waitForStatus(t, eng, id, StatusDelivered)

	dlq, _ := eng.ListDLQ(0)
	if len(dlq) != 0 {
		t.Fatalf("ListDLQ 回傳 %d 筆, 預期 0", len(dlq))
	}
}

// replay 必須重置計數、退出 DLQ 索引，並真的再投遞一次。
func TestReplayEvent(t *testing.T) {
	sender := newFakeSender()
	sender.respond = func(_ EventID, attempt int) (HTTPResponse, error) {
		if attempt == 1 {
			return HTTPResponse{StatusCode: 400}, nil
		}
		return HTTPResponse{StatusCode: 200}, nil
	}
	eng := newTestEngine(t, newFakeClock(time.Now()), sender, nil)

	id, _ := eng.PublishEvent(validEvent())
	waitForStatus(t, eng, id, StatusDeadLetter)

	if err := eng.ReplayEvent(id); err != nil {
		t.Fatalf("ReplayEvent() 失敗: %v", err)
	}

	rec := waitForStatus(t, eng, id, StatusDelivered)
	if rec.RetryCount != 0 {
		t.Errorf("replay 後 RetryCount = %d, 預期歸零", rec.RetryCount)
	}
	if rec.LastError != "" {
		t.Errorf("replay 後 LastError = %q, 預期清空", rec.LastError)
	}
	if !rec.DeadLetteredAt.IsZero() {
		t.Error("replay 後 DeadLetteredAt 應被清空")
	}
	if n := sender.attemptsFor(id); n != 2 {
		t.Errorf("投遞次數 = %d, 預期 2", n)
	}

	// 索引必須同步清掉，否則 DLQ 報表會留下幽靈項目。
	if dlq, _ := eng.ListDLQ(0); len(dlq) != 0 {
		t.Fatalf("replay 後 ListDLQ 仍有 %d 筆", len(dlq))
	}
}

func TestReplayEventRejectsNonDeadLettered(t *testing.T) {
	sender := newFakeSender()
	eng := newTestEngine(t, newFakeClock(time.Now()), sender, nil)

	if err := eng.ReplayEvent("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ReplayEvent(不存在) = %v, 預期 ErrNotFound", err)
	}

	id, _ := eng.PublishEvent(validEvent())
	waitForStatus(t, eng, id, StatusDelivered)
	if err := eng.ReplayEvent(id); !errors.Is(err, ErrAlreadyDelivered) {
		t.Errorf("ReplayEvent(已送達) = %v, 預期 ErrAlreadyDelivered", err)
	}
}

// 尚未投遞的事件也不該能 replay——它本來就還在佇列裡等，
// 再排一次只會讓同一筆事件在佇列中出現兩份。
func TestReplayEventRejectsPending(t *testing.T) {
	sender := newFakeSender()
	sender.block = make(chan struct{})
	sender.started = make(chan EventID, 1)
	defer close(sender.block)

	eng := newTestEngine(t, newFakeClock(time.Now()), sender, func(cfg *Config) {
		// 併發額度設為 1，第二筆事件會一直被延後而停在 PENDING。
		cfg.MaxInflightPerMerchant = 1
	})

	first := validEvent()
	first.EventID = "first"
	if _, err := eng.PublishEvent(first); err != nil {
		t.Fatalf("PublishEvent() 失敗: %v", err)
	}
	<-sender.started

	second := validEvent()
	second.EventID = "second"
	id, _ := eng.PublishEvent(second)

	if err := eng.ReplayEvent(id); !errors.Is(err, ErrNotDeadLettered) {
		t.Fatalf("ReplayEvent(PENDING) = %v, 預期 ErrNotDeadLettered", err)
	}
}

// 毒藥端點測試：一個卡住不回應的 merchant 不得餓死其他 merchant。
func TestSlowMerchantDoesNotStarveOthers(t *testing.T) {
	const slowURL = "https://slow.example.com/webhooks"

	sender := newFakeSender()
	sender.block = make(chan struct{})
	sender.blockIf = func(destURL string) bool { return destURL == slowURL }
	sender.started = make(chan EventID, 8)
	defer close(sender.block)

	eng := newTestEngine(t, newFakeClock(time.Now()), sender, func(cfg *Config) {
		cfg.Workers = 2
		cfg.MaxInflightPerMerchant = 1
	})

	// 塞滿慢速 merchant 的事件：若沒有 per-merchant 併發上限，
	// 它們會佔住全部 worker，後面那筆健康的事件就永遠等不到人處理。
	for i := 0; i < 5; i++ {
		ev := validEvent()
		ev.EventID = EventID("slow-" + string(rune('a'+i)))
		ev.DestURL = slowURL
		if _, err := eng.PublishEvent(ev); err != nil {
			t.Fatalf("PublishEvent() 失敗: %v", err)
		}
	}
	<-sender.started // 確認慢速 merchant 已佔住一個 worker

	healthy := validEvent()
	healthy.EventID = "healthy"
	healthy.MerchantID = "merchant-2"
	id, _ := eng.PublishEvent(healthy)

	waitForStatus(t, eng, id, StatusDelivered)
}

// 回收器必須清掉超過保留期的終態事件，否則記憶體只增不減。
func TestReaperRemovesExpiredTerminalEvents(t *testing.T) {
	sender := newFakeSender()
	clock := newFakeClock(time.Now())
	eng := newTestEngine(t, clock, sender, func(cfg *Config) {
		cfg.Retention = time.Minute
		cfg.ReapInterval = time.Minute
	})

	id, _ := eng.PublishEvent(validEvent())
	waitForStatus(t, eng, id, StatusDelivered)

	clock.Advance(3 * time.Minute)

	if !eventually(func() bool {
		_, err := eng.GetEventStatus(id)
		return errors.Is(err, ErrNotFound)
	}) {
		t.Fatal("超過保留期的已送達事件仍未被回收")
	}
}

// 回收不得動到還在投遞流程中的事件。
func TestReaperKeepsActiveEvents(t *testing.T) {
	sender := newFakeSender()
	sender.respond = func(EventID, int) (HTTPResponse, error) {
		return HTTPResponse{StatusCode: 503}, nil
	}
	clock := newFakeClock(time.Now())
	eng := newTestEngine(t, clock, sender, func(cfg *Config) {
		cfg.MaxRetries = 100
		cfg.BaseBackoff = time.Hour
		cfg.MaxBackoff = time.Hour
		cfg.Retention = time.Minute
		cfg.ReapInterval = time.Minute
	})

	id, _ := eng.PublishEvent(validEvent())
	waitForStatus(t, eng, id, StatusRetrying)

	// 推進到超過保留期，但仍在退避窗口內。
	clock.Advance(2 * time.Minute)

	if !eventually(func() bool { return eng.reapCount() > 0 }) {
		t.Fatal("回收器未執行")
	}
	if _, err := eng.GetEventStatus(id); err != nil {
		t.Fatalf("退避中的事件被回收了: %v", err)
	}
}

// DLQ 事件同樣受保留期約束，回收時索引必須一起清掉。
func TestReaperRemovesExpiredDeadLetters(t *testing.T) {
	sender := newFakeSender()
	sender.respond = func(EventID, int) (HTTPResponse, error) {
		return HTTPResponse{StatusCode: 400}, nil
	}
	clock := newFakeClock(time.Now())
	eng := newTestEngine(t, clock, sender, func(cfg *Config) {
		cfg.Retention = time.Minute
		cfg.ReapInterval = time.Minute
	})

	id, _ := eng.PublishEvent(validEvent())
	waitForStatus(t, eng, id, StatusDeadLetter)

	clock.Advance(3 * time.Minute)

	if !eventually(func() bool {
		dlq, _ := eng.ListDLQ(0)
		return len(dlq) == 0
	}) {
		t.Fatal("超過保留期的死信仍留在 DLQ 索引中")
	}
	if _, err := eng.GetEventStatus(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetEventStatus() = %v, 預期 ErrNotFound", err)
	}
}

// containsSentinel 檢查記錄下來的錯誤字串是否源自指定的 sentinel error。
// LastError 刻意存成字串而非 error：它要能被序列化進稽核日誌與 DLQ 報表，
// 代價是只能比對訊息內容。
func containsSentinel(lastError string, sentinel error) bool {
	return strings.Contains(lastError, sentinel.Error())
}
