package outbox

import "errors"

// 引擎對呼叫端的錯誤契約。全部以 sentinel error 表達，呼叫端用 errors.Is 判斷。
var (
	// ErrNotFound 表示該 EventID 不存在於 outbox。
	ErrNotFound = errors.New("outbox: event not found")
	// ErrDuplicateEvent 表示該 EventID 已登錄過。
	// 這是 outbox 的第一道去重閘：同一筆業務事件重複發布不會造成兩次投遞。
	ErrDuplicateEvent = errors.New("outbox: duplicate event id")
	// ErrInvalidEvent 表示事件本身不合法（缺 MerchantID、DestURL、Payload 等）。
	ErrInvalidEvent = errors.New("outbox: invalid event")
	// ErrEngineClosed 表示引擎已關閉，不再接受新事件。
	ErrEngineClosed = errors.New("outbox: engine closed")
	// ErrInvalidConfig 表示引擎設定不合法。
	ErrInvalidConfig = errors.New("outbox: invalid config")
)

// 狀態流轉相關錯誤。
var (
	// ErrNotDeadLettered 表示嘗試 replay 一個不在 DLQ 裡的事件。
	ErrNotDeadLettered = errors.New("outbox: event is not dead-lettered")
	// ErrDeadLettered 表示事件已在 DLQ，除 replay 外不接受其他流轉。
	ErrDeadLettered = errors.New("outbox: event is dead-lettered")
	// ErrAlreadyDelivered 表示事件已成功送達，這是真正的終點。
	ErrAlreadyDelivered = errors.New("outbox: event already delivered")
	// ErrInFlight 表示事件已被其他 worker claim，正在投遞中。
	ErrInFlight = errors.New("outbox: event in flight")
	// ErrInvalidTransition 表示狀態流轉違反狀態機。
	ErrInvalidTransition = errors.New("outbox: invalid status transition")
)

// 簽章與投遞相關錯誤。
var (
	// ErrSecretNotFound 表示找不到該 merchant 的簽章密鑰。
	// 這是設定缺失而非暫時性故障，事件會直接進 DLQ 而不重試。
	ErrSecretNotFound = errors.New("outbox: merchant secret not found")
	// ErrNonRetryableStatus 表示 merchant 回了不可重試的 4xx。
	ErrNonRetryableStatus = errors.New("outbox: non-retryable http status")
	// ErrRetriesExhausted 表示重試次數已用盡。
	ErrRetriesExhausted = errors.New("outbox: max retries exhausted")
)
