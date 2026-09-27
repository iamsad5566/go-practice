package outbox

// Status 是 outbox 事件的投遞生命週期狀態。
type Status string

const (
	// StatusUnknown 是零值哨兵。它永遠不該被寫入任何 record——
	// 一旦在狀態機裡看到它，代表有人繞過 PublishEvent 自行組了 record。
	StatusUnknown Status = ""
	// StatusPending 已登錄於 outbox，等待首次派送。
	StatusPending Status = "PENDING"
	// StatusInFlight 已被某個 worker claim，正在呼叫 merchant 端點。
	StatusInFlight Status = "IN_FLIGHT"
	// StatusRetrying 上次投遞失敗且可重試，正在退避等待 NextAttemptAt。
	StatusRetrying Status = "RETRYING"
	// StatusDelivered merchant 回報 2xx。
	StatusDelivered Status = "DELIVERED"
	// StatusDeadLetter 不可重試的錯誤，或重試次數耗盡。可由 ReplayEvent 救回。
	StatusDeadLetter Status = "DEAD_LETTER"
)

// IsTerminal 回報派送迴圈是否已不再處理此事件。
//
// DEAD_LETTER 算終態是刻意的：它不會再被 dispatcher 自動撿起，
// 回到 PENDING 的唯一途徑是 ReplayEvent 這個明確的人工介入。
func (s Status) IsTerminal() bool {
	switch s {
	case StatusDelivered, StatusDeadLetter:
		return true
	default:
		return false
	}
}

// IsDispatchable 回報事件是否處於「等待被 claim」的狀態。
// dispatcher 用它做惰性丟棄，worker 用它作為 CAS 的前置條件。
func (s Status) IsDispatchable() bool {
	switch s {
	case StatusPending, StatusRetrying:
		return true
	default:
		return false
	}
}

// allowedTransitions 是狀態機的唯一真實來源。
// 新增狀態或投遞路徑時只改這張表，不需要動任何呼叫點。
var allowedTransitions = map[Status]map[Status]bool{
	StatusPending: {
		StatusInFlight: true,
	},
	StatusRetrying: {
		StatusInFlight: true,
	},
	StatusInFlight: {
		StatusDelivered: true,
		// 可重試錯誤：退回等待，由 NextAttemptAt 決定何時再試。
		StatusRetrying: true,
		// 不可重試錯誤或次數耗盡。
		StatusDeadLetter: true,
	},
	StatusDeadLetter: {
		// ReplayEvent 是唯一能回到 PENDING 的路徑。
		StatusPending: true,
	},
}

// canTransition 檢查流轉是否合法，並回傳對呼叫端有意義的錯誤。
//
// 錯誤語意主要取決於「當前狀態」而非目標狀態，讓 replay、claim、
// 投遞結果回寫三條路徑共用同一套裁決，不必各自重複判斷。
func canTransition(from, to Status) error {
	if allowedTransitions[from][to] {
		return nil
	}
	switch from {
	case StatusDelivered:
		return ErrAlreadyDelivered
	case StatusDeadLetter:
		return ErrDeadLettered
	case StatusInFlight:
		return ErrInFlight
	case StatusPending, StatusRetrying:
		// 這兩個狀態唯一的合法出口是 IN_FLIGHT，所以走到這裡只剩一種可能：
		// 有人想把非 DLQ 的事件 replay 回 PENDING。
		if to == StatusPending {
			return ErrNotDeadLettered
		}
		return ErrInvalidTransition
	default:
		return ErrInvalidTransition
	}
}
