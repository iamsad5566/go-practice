package paysched

import "errors"

// 引擎對呼叫端的錯誤契約。全部以 sentinel error 表達，呼叫端用 errors.Is 判斷。
var (
	// ErrNotFound 表示該 ScheduleID 不存在。
	ErrNotFound = errors.New("paysched: schedule not found")
	// ErrInProgress 表示付款已交付 worker 並正在呼叫外部通道，此刻不可取消。
	ErrInProgress = errors.New("paysched: payment in progress")
	// ErrAlreadyCancelled 表示付款先前已被取消。
	ErrAlreadyCancelled = errors.New("paysched: payment already cancelled")
	// ErrAlreadyExecuted 表示付款已進入 COMPLETED / FAILED 終態。
	ErrAlreadyExecuted = errors.New("paysched: payment already executed")
	// ErrInvalidRequest 表示排程請求本身不合法（缺欄位、金額非正數等）。
	ErrInvalidRequest = errors.New("paysched: invalid payment request")
	// ErrPastExecuteAt 表示 ExecuteAt 為零值或已過期；本引擎不支援立即執行。
	ErrPastExecuteAt = errors.New("paysched: execute_at must be in the future")
	// ErrEngineClosed 表示引擎已關閉，不再接受新排程。
	ErrEngineClosed = errors.New("paysched: engine closed")
	// ErrRateLimitExceeded 表示限流拒絕。
	ErrRateLimitExceeded = errors.New("paysched: rate limit exceeded")
	// ErrCostExceedsCapacity 表示單筆成本大於桶容量，永遠不可能通過，屬呼叫端設定錯誤。
	ErrCostExceedsCapacity = errors.New("paysched: cost exceeds bucket capacity")
)

// 外部通道常見的暫時性錯誤，預設會觸發指數退避重試。
var (
	// ErrChannelBusy 表示清算通道忙碌，可重試。
	ErrChannelBusy = errors.New("paysched: channel busy")
	// ErrNetworkTimeout 表示網路逾時，可重試。
	ErrNetworkTimeout = errors.New("paysched: network timeout")
)

// RetryableError 讓呼叫端自訂的錯誤型別能自行宣告是否可重試，
// 無須修改引擎的預設判斷邏輯（對擴充開放）。
type RetryableError interface {
	error
	Retryable() bool
}

// DefaultIsRetryable 是預設的重試判斷：實作 RetryableError 者自行決定，
// 其餘僅認得引擎內建的兩種暫時性錯誤。
func DefaultIsRetryable(err error) bool {
	if err == nil {
		return false
	}
	var re RetryableError
	if errors.As(err, &re) {
		return re.Retryable()
	}
	return errors.Is(err, ErrChannelBusy) || errors.Is(err, ErrNetworkTimeout)
}
