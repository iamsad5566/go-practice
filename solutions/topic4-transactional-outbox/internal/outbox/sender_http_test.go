package outbox

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

// receivedWebhook 是 merchant 端收到的內容。
type receivedWebhook struct {
	Method      string
	ContentType string
	EventID     string
	DeliveryID  string
	Body        []byte
	Verified    bool
}

// merchantServer 是一個會像真實 merchant 那樣驗證簽章的測試伺服器。
func merchantServer(t *testing.T, secret string, status func(n int) int) (*httptest.Server, func() []receivedWebhook) {
	t.Helper()

	var mu sync.Mutex
	var received []receivedWebhook

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		// merchant 端的正確驗證流程：重算 HMAC，再以常數時間比較。
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(r.Header.Get(headerTimestamp) + "." + string(body)))
		expected := mac.Sum(nil)
		got, decodeErr := hex.DecodeString(r.Header.Get(headerSignature))
		verified := decodeErr == nil && hmac.Equal(expected, got)

		mu.Lock()
		received = append(received, receivedWebhook{
			Method:      r.Method,
			ContentType: r.Header.Get("Content-Type"),
			EventID:     r.Header.Get(headerEventID),
			DeliveryID:  r.Header.Get(headerDeliveryID),
			Body:        body,
			Verified:    verified,
		})
		n := len(received)
		mu.Unlock()

		w.WriteHeader(status(n))
	}))
	t.Cleanup(srv.Close)

	return srv, func() []receivedWebhook {
		mu.Lock()
		defer mu.Unlock()
		return append([]receivedWebhook(nil), received...)
	}
}

func TestHTTPSenderDeliversVerifiableWebhook(t *testing.T) {
	const secret = "http-secret"
	srv, received := merchantServer(t, secret, func(int) int { return http.StatusOK })

	secrets := NewMemorySecretStore()
	secrets.Put("merchant-1", []byte(secret))
	eng, err := New(Config{
		Sender:  NewHTTPSender(2 * time.Second),
		Secrets: secrets,
		Workers: 1,
	})
	if err != nil {
		t.Fatalf("New() 失敗: %v", err)
	}
	defer eng.Close()

	ev := validEvent()
	ev.EventID = "order-42-settled"
	ev.DestURL = srv.URL + "/webhooks"
	id, err := eng.PublishEvent(ev)
	if err != nil {
		t.Fatalf("PublishEvent() 失敗: %v", err)
	}
	waitForStatus(t, eng, id, StatusDelivered)

	got := received()
	if len(got) != 1 {
		t.Fatalf("merchant 收到 %d 次請求, 預期 1 次", len(got))
	}
	if got[0].Method != http.MethodPost {
		t.Errorf("Method = %s, 預期 POST", got[0].Method)
	}
	if got[0].ContentType != "application/json" {
		t.Errorf("Content-Type = %q, 預期 application/json", got[0].ContentType)
	}
	if !got[0].Verified {
		t.Error("merchant 無法驗證簽章")
	}
	if got[0].EventID != "order-42-settled" {
		t.Errorf("%s = %q", headerEventID, got[0].EventID)
	}
	if string(got[0].Body) != string(ev.Payload) {
		t.Errorf("負載 = %q, 預期 %q", got[0].Body, ev.Payload)
	}
}

// 真實 HTTP 路徑下的重試：同一事件的 Event-ID 恆定、Delivery-ID 每次不同。
// 這正是 merchant 能在 at-least-once 語意下維持冪等的依據。
func TestHTTPSenderRetryKeepsEventIDAndRotatesDeliveryID(t *testing.T) {
	const secret = "http-secret"
	srv, received := merchantServer(t, secret, func(n int) int {
		if n == 1 {
			return http.StatusServiceUnavailable
		}
		return http.StatusOK
	})

	secrets := NewMemorySecretStore()
	secrets.Put("merchant-1", []byte(secret))
	eng, err := New(Config{
		Sender:      NewHTTPSender(2 * time.Second),
		Secrets:     secrets,
		Workers:     1,
		BaseBackoff: time.Millisecond,
		MaxBackoff:  10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New() 失敗: %v", err)
	}
	defer eng.Close()

	ev := validEvent()
	ev.DestURL = srv.URL + "/webhooks"
	id, _ := eng.PublishEvent(ev)
	waitForStatus(t, eng, id, StatusDelivered)

	got := received()
	if len(got) != 2 {
		t.Fatalf("merchant 收到 %d 次請求, 預期 2 次", len(got))
	}
	if got[0].EventID != got[1].EventID {
		t.Errorf("兩次投遞的 Event-ID 不同: %q vs %q", got[0].EventID, got[1].EventID)
	}
	if got[0].DeliveryID == got[1].DeliveryID {
		t.Errorf("兩次投遞的 Delivery-ID 相同: %q", got[0].DeliveryID)
	}
	for i, w := range got {
		if !w.Verified {
			t.Errorf("第 %d 次投遞的簽章無法驗證", i+1)
		}
	}
}

// 逾時必須由 context 收斂，並被判為可重試。
func TestHTTPSenderRespectsContextTimeout(t *testing.T) {
	// 用測試自己控制的 channel 讓 handler 停住，而不是等 r.Context()：
	// 客戶端逾時不保證會立刻讓伺服器端的請求 context 取消，
	// 那樣 httptest.Server.Close() 會一直等這個 handler 而卡死測試。
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)

	sender := NewHTTPSender(time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	resp, err := sender.Send(ctx, srv.URL, nil, []byte("{}"))
	if err == nil {
		t.Fatal("預期逾時錯誤, 卻成功返回")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("逾時後經過 %v 才返回, context 未生效", elapsed)
	}
	if !DefaultIsRetryable(resp, err) {
		t.Error("逾時應被判為可重試")
	}
}

// 重導向不得被靜默跟隨：帶簽章的負載被送往未預期的主機是安全問題。
func TestHTTPSenderDoesNotFollowRedirects(t *testing.T) {
	var elsewhereHits int
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		elsewhereHits++
	}))
	defer elsewhere.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	resp, err := NewHTTPSender(2*time.Second).Send(context.Background(), srv.URL, nil, []byte("{}"))
	if err != nil {
		t.Fatalf("Send() 失敗: %v", err)
	}
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Errorf("StatusCode = %d, 預期原樣回報 307", resp.StatusCode)
	}
	if elsewhereHits != 0 {
		t.Errorf("負載被送往重導向目標 %d 次", elsewhereHits)
	}
	if DefaultIsRetryable(resp, err) {
		t.Error("未跟隨的重導向屬設定錯誤, 不該重試")
	}
}

// merchant 回傳巨大錯誤頁時，保留的內容必須有上限，
// 否則我們的記憶體用量會由外部系統決定。
func TestHTTPSenderCapsCapturedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write(make([]byte, 10*maxCapturedBody))
	}))
	defer srv.Close()

	resp, err := NewHTTPSender(2*time.Second).Send(context.Background(), srv.URL, nil, []byte("{}"))
	if err != nil {
		t.Fatalf("Send() 失敗: %v", err)
	}
	if len(resp.Body) > maxCapturedBody {
		t.Fatalf("保留了 %d 位元組的回應內容, 上限應為 %d", len(resp.Body), maxCapturedBody)
	}
}

// 送出的時間戳必須是可解析的 Unix 秒，merchant 才能據此設定重播窗口。
func TestHTTPSenderTimestampIsParsable(t *testing.T) {
	const secret = "http-secret"
	srv, received := merchantServer(t, secret, func(int) int { return http.StatusOK })

	secrets := NewMemorySecretStore()
	secrets.Put("merchant-1", []byte(secret))
	s := &signer{secrets: secrets}
	rec := newEntry(validEvent(), "evt-1", 1, time.Now()).snapshot()
	headers, _, err := s.buildHeaders(rec, time.Now())
	if err != nil {
		t.Fatalf("buildHeaders() 失敗: %v", err)
	}

	if _, err := NewHTTPSender(2*time.Second).Send(context.Background(), srv.URL, headers, rec.Event.Payload); err != nil {
		t.Fatalf("Send() 失敗: %v", err)
	}

	got := received()
	if len(got) != 1 || !got[0].Verified {
		t.Fatal("merchant 無法驗證這次投遞")
	}
	ts, err := strconv.ParseInt(headers[headerTimestamp], 10, 64)
	if err != nil {
		t.Fatalf("時間戳無法解析: %v", err)
	}
	if delta := time.Since(time.Unix(ts, 0)); delta > time.Minute || delta < -time.Minute {
		t.Errorf("時間戳與當下相差 %v, 超出合理範圍", delta)
	}
}
