package outbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPResponse 是 merchant 端點回應的精簡視圖。
// 只保留引擎裁決所需的資訊，讓 WebhookSender 的實作不必綁在 net/http 上。
type HTTPResponse struct {
	StatusCode int
	// Body 是回應內容的前段，僅供稽核與錯誤訊息使用。
	Body []byte
}

// IsSuccess 回報 merchant 是否已確認收下事件。
func (r HTTPResponse) IsSuccess() bool {
	return r.StatusCode >= 200 && r.StatusCode < 300
}

// WebhookSender 是對 merchant 端點發出 HTTP 請求的抽象。
//
// 抽成介面的目的不只是為了測試：它也是未來換成 gRPC、訊息佇列或
// 帶 mTLS 的專屬通道時唯一需要改動的地方。
//
// 實作必須尊重 ctx 的逾時；引擎會為每次投遞設定 Config.SendTimeout。
type WebhookSender interface {
	Send(ctx context.Context, destURL string, headers map[string]string, payload []byte) (HTTPResponse, error)
}

// maxCapturedBody 是保留的回應內容上限。
// merchant 可能回傳一整頁 HTML 錯誤頁，全部留著會讓 outbox 的記憶體用量
// 由外部系統決定——這是不可接受的。
const maxCapturedBody = 1 << 12 // 4 KiB

// httpSender 是以 net/http 實作的生產用 sender。
type httpSender struct {
	client *http.Client
}

// NewHTTPSender 建立以 net/http 為底的 sender。
//
// 刻意不跟隨重導向：webhook 端點若回 3xx，通常代表設定錯誤（例如少了尾斜線
// 或 http/https 混用），而非真的要我們去別處投遞。靜默跟隨會讓帶簽章的
// 負載被送往未預期的主機，這是安全問題而非便利問題。
func NewHTTPSender(timeout time.Duration) WebhookSender {
	return &httpSender{
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (s *httpSender) Send(ctx context.Context, destURL string, headers map[string]string, payload []byte) (HTTPResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, destURL, bytes.NewReader(payload))
	if err != nil {
		return HTTPResponse{}, fmt.Errorf("outbox: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		// 網路層失敗沒有狀態碼，交由 IsRetryable 依 error 判斷。
		return HTTPResponse{}, fmt.Errorf("outbox: send webhook: %w", err)
	}
	defer resp.Body.Close()

	// 即使不在意內容也必須讀完並關閉，否則連線無法回到連線池重用。
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxCapturedBody))
	return HTTPResponse{StatusCode: resp.StatusCode, Body: body}, nil
}

// DefaultIsRetryable 是預設的重試判斷。
//
// 判斷準則是「重送有機會成功嗎」：
//   - 網路層錯誤（連線被拒、逾時、TLS 交握失敗）：對方可能只是正在重啟，可重試。
//   - 5xx：對方自己說它壞了，可重試。
//   - 429：對方要我們慢一點，退避後重試正是它期待的行為。
//   - 408：請求逾時，可重試。
//   - 其他 4xx：請求本身有問題（簽章錯、路徑錯、未授權），重送一萬次也一樣，
//     直接進 DLQ 讓人來看，比燒掉重試額度有意義。
//   - 3xx：未跟隨的重導向視為設定錯誤，不重試。
func DefaultIsRetryable(resp HTTPResponse, err error) bool {
	if err != nil {
		return true
	}
	switch {
	case resp.StatusCode >= 500:
		return true
	case resp.StatusCode == http.StatusTooManyRequests:
		return true
	case resp.StatusCode == http.StatusRequestTimeout:
		return true
	default:
		return false
	}
}
