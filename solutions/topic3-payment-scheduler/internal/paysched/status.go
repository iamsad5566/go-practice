package paysched

// Status 是付款的生命週期狀態。
type Status string

const (
	// StatusScheduled 已排程，等待到期；重試退避期間也停留在此狀態，因此仍可取消。
	StatusScheduled Status = "SCHEDULED"
	// StatusDispatched 已交付 worker，正在呼叫外部清算通道，此時不可取消。
	StatusDispatched Status = "DISPATCHED"
	// StatusCompleted 外部通道回報成功。
	StatusCompleted Status = "COMPLETED"
	// StatusCancelled 由呼叫端取消。
	StatusCancelled Status = "CANCELLED"
	// StatusFailed 不可重試的錯誤，或重試次數耗盡。
	StatusFailed Status = "FAILED"
)

// IsTerminal 回報此狀態是否為不可再流轉的終態。
func (s Status) IsTerminal() bool {
	switch s {
	case StatusCompleted, StatusCancelled, StatusFailed:
		return true
	default:
		return false
	}
}

// allowedTransitions 是狀態機的唯一真實來源。
// 新增狀態或流轉時只改這張表，不需要動任何呼叫點。
var allowedTransitions = map[Status]map[Status]bool{
	StatusScheduled: {
		StatusDispatched: true,
		StatusCancelled:  true,
	},
	StatusDispatched: {
		StatusCompleted: true,
		StatusFailed:    true,
		// 可重試錯誤或被限流時退回排程，等退避時間到再派送。
		StatusScheduled: true,
	},
}

// canTransition 檢查流轉是否合法，並回傳對呼叫端有意義的錯誤。
// 錯誤語意取決於「當前狀態」而非目標狀態，讓取消與派送共用同一套裁決。
func canTransition(from, to Status) error {
	if allowedTransitions[from][to] {
		return nil
	}
	switch from {
	case StatusCancelled:
		return ErrAlreadyCancelled
	case StatusCompleted, StatusFailed:
		return ErrAlreadyExecuted
	case StatusDispatched:
		return ErrInProgress
	default:
		return ErrInvalidRequest
	}
}
