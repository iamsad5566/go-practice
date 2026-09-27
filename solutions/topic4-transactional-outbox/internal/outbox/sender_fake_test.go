package outbox

import (
	"context"
	"sync"
)

// deliveryAttempt 記錄一次投遞嘗試的完整內容，供測試斷言 header 與負載。
type deliveryAttempt struct {
	DestURL string
	Headers map[string]string
	Payload []byte
}

// fakeSender 是可程式化的投遞管道，讓投遞結果在測試中完全確定。
//
// 回應由 respond 函式決定：它收到目前是第幾次針對該事件的嘗試，
// 因此測試能輕易表達「前兩次 503、第三次 200」這類劇本。
type fakeSender struct {
	mu       sync.Mutex
	attempts []deliveryAttempt
	perEvent map[EventID]int

	// respond 為 nil 時一律回 200。
	respond func(eventID EventID, attempt int) (HTTPResponse, error)
	// block 若非 nil，Send 會在此等待，用於模擬緩慢或懸掛的 merchant 端點。
	block chan struct{}
	// blockIf 限定只有符合條件的端點會被 block；為 nil 時代表全部都 block。
	// 用於「某個 merchant 很慢，其他 merchant 不該被餓死」這類情境。
	blockIf func(destURL string) bool
	// started 在每次進入 Send 時收到一個訊號，讓測試能確知投遞已開始。
	started chan EventID
}

func newFakeSender() *fakeSender {
	return &fakeSender{perEvent: make(map[EventID]int)}
}

func (f *fakeSender) Send(ctx context.Context, destURL string, headers map[string]string, payload []byte) (HTTPResponse, error) {
	eventID := EventID(headers[headerEventID])

	f.mu.Lock()
	f.perEvent[eventID]++
	attempt := f.perEvent[eventID]
	f.attempts = append(f.attempts, deliveryAttempt{
		DestURL: destURL,
		Headers: headers,
		Payload: append([]byte(nil), payload...),
	})
	respond, block, started, blockIf := f.respond, f.block, f.started, f.blockIf
	f.mu.Unlock()

	if block != nil && blockIf != nil && !blockIf(destURL) {
		block = nil
	}

	if started != nil {
		select {
		case started <- eventID:
		case <-ctx.Done():
			return HTTPResponse{}, ctx.Err()
		}
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return HTTPResponse{}, ctx.Err()
		}
	}

	if respond == nil {
		return HTTPResponse{StatusCode: 200}, nil
	}
	return respond(eventID, attempt)
}

// snapshotAttempts 回傳目前所有嘗試的副本。
func (f *fakeSender) snapshotAttempts() []deliveryAttempt {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]deliveryAttempt(nil), f.attempts...)
}

// attemptsFor 回傳針對某事件的嘗試次數。
func (f *fakeSender) attemptsFor(eventID EventID) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.perEvent[eventID]
}

func (f *fakeSender) totalAttempts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.attempts)
}
