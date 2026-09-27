package outbox

import (
	"context"
	"fmt"
	"time"
)

// runWorker 消化交接佇列。Close 時佇列被關閉，worker 把手上與剩餘的事件做完才退出。
func (e *Engine) runWorker() {
	defer e.workerWG.Done()
	defer e.live.Add(-1)
	for entry := range e.dispatch {
		e.deliver(entry)
	}
}

// deliver 投遞單一事件。
//
// 全程只在狀態流轉的瞬間持有 entry 鎖，HTTP 呼叫期間不持有任何鎖——
// 這是「慢的 merchant 不會拖垮引擎」的前提。
func (e *Engine) deliver(entry *eventEntry) {
	rec := entry.snapshot()
	// 額度由 dispatcher 預留，無論投遞結果如何都必須歸還。
	defer e.releaseInflight(rec.Event.MerchantID)

	now := e.clock.Now()

	// claim：PENDING|RETRYING → IN_FLIGHT 的原子流轉。
	// 檢查與寫入在同一個臨界區內完成，因此即使有其他來源同時想派送這筆事件，
	// 也只有一個能成功，其餘會拿到錯誤並直接放棄（PRD 非功能需求 2）。
	if err := entry.transition(StatusInFlight, now, func(r *EventRecord) {
		rec = *r
	}); err != nil {
		return
	}

	headers, deliveryID, err := e.signer.buildHeaders(rec, now)
	if err != nil {
		// 密鑰缺失或亂數失敗都不是暫時性問題，重送一萬次也一樣。
		// 直接進 DLQ 讓人來處理，比燒掉重試額度後才進 DLQ 有意義。
		e.deadLetter(entry, "", 0, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), e.cfg.SendTimeout)
	defer cancel()
	resp, sendErr := e.cfg.Sender.Send(ctx, rec.Event.DestURL, headers, rec.Event.Payload)

	switch {
	case sendErr == nil && resp.IsSuccess():
		e.markDelivered(entry, deliveryID, resp)

	case !e.cfg.IsRetryable(resp, sendErr):
		// 不可重試的錯誤（4xx、未跟隨的重導向）：立刻進 DLQ，不等次數耗盡。
		e.deadLetter(entry, deliveryID, resp.StatusCode, nonRetryableReason(resp, sendErr))

	case rec.RetryCount >= e.cfg.MaxRetries:
		e.deadLetter(entry, deliveryID, resp.StatusCode,
			fmt.Errorf("%w: %d attempts, last error: %v", ErrRetriesExhausted, rec.RetryCount+1, deliveryError(resp, sendErr)))

	default:
		e.scheduleRetry(entry, deliveryID, resp, sendErr)
	}
}

func (e *Engine) markDelivered(entry *eventEntry, deliveryID string, resp HTTPResponse) {
	now := e.clock.Now()
	_ = entry.transition(StatusDelivered, now, func(rec *EventRecord) {
		rec.LastDeliveryID = deliveryID
		rec.LastStatusCode = resp.StatusCode
		rec.LastError = ""
		rec.DeliveredAt = now
	})
}

// scheduleRetry 讓事件退回 RETRYING，並以指數退避 + jitter 重排。
//
// 重排的方式是「以新的到期時刻放回同一個佇列」，而不是塞到隊尾：
// 因此退避中的事件既不會插隊，也不會被無限延後——時間到了它自然浮上堆頂。
func (e *Engine) scheduleRetry(entry *eventEntry, deliveryID string, resp HTTPResponse, sendErr error) {
	now := e.clock.Now()

	var nextAttemptAt time.Time
	_ = entry.transition(StatusRetrying, now, func(rec *EventRecord) {
		rec.RetryCount++
		rec.LastDeliveryID = deliveryID
		rec.LastStatusCode = resp.StatusCode
		rec.LastError = deliveryError(resp, sendErr).Error()
		rec.NextAttemptAt = now.Add(e.cfg.backoffFor(rec.RetryCount))
		nextAttemptAt = rec.NextAttemptAt
	})
	e.reschedule(entry, nextAttemptAt)
}

// deadLetter 把事件移入死信佇列。
//
// 流轉先完成、再登錄索引，順序不可顛倒：反過來的話，索引裡會短暫出現
// 一筆狀態還不是 DEAD_LETTER 的事件，並發的 ListDLQ 就會漏掉它。
func (e *Engine) deadLetter(entry *eventEntry, deliveryID string, statusCode int, cause error) {
	now := e.clock.Now()

	var id EventID
	if err := entry.transition(StatusDeadLetter, now, func(rec *EventRecord) {
		rec.LastDeliveryID = deliveryID
		rec.LastStatusCode = statusCode
		rec.LastError = cause.Error()
		rec.DeadLetteredAt = now
		id = rec.Event.EventID
	}); err != nil {
		return
	}
	e.enrollDLQ(id)
}

// deliveryError 把「網路層錯誤」與「HTTP 狀態碼」統一成一個可讀的原因。
func deliveryError(resp HTTPResponse, sendErr error) error {
	if sendErr != nil {
		return sendErr
	}
	if len(resp.Body) > 0 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, truncate(resp.Body, 256))
	}
	return fmt.Errorf("http %d", resp.StatusCode)
}

// nonRetryableReason 在原因外再包一層 ErrNonRetryableStatus，
// 讓呼叫端能用 errors.Is 區分「對方壞了」與「我們送錯了」。
func nonRetryableReason(resp HTTPResponse, sendErr error) error {
	return fmt.Errorf("%w: %v", ErrNonRetryableStatus, deliveryError(resp, sendErr))
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
