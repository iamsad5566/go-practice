package outbox

import "time"

// Clock 把時間來源抽象出來，讓退避與保留期的時間行為可在測試中精確驅動，
// 不必靠 sleep 換取穩定性。生產環境一律使用 realClock。
type Clock interface {
	Now() time.Time
	// NewTimer 掛上一個在 deadline 觸發的計時器。
	//
	// 刻意以「絕對時刻」而非「相對時長」為介面：NextAttemptAt 在引擎裡本來就是
	// 絕對的，而相對時長必須在呼叫端算好，一旦計算與掛上之間時間前進，
	// 就會掛成偏晚的時刻而錯過喚醒。
	// 同理也不提供 After()——time.After 的計時器在觸發前無法回收，
	// 大量退避中的事件會造成記憶體堆積。
	NewTimer(deadline time.Time) Timer
}

// Timer 是可重複使用的單次計時器。
type Timer interface {
	// C 回傳觸發通道。
	C() <-chan time.Time
	// Reset 改以新的絕對時刻觸發。若該時刻已過，計時器會立即觸發。
	Reset(deadline time.Time) bool
	// Stop 停止計時器並釋放資源。
	Stop() bool
}

type realClock struct{}

// NewRealClock 回傳以系統時間為基準的 Clock。
func NewRealClock() Clock { return realClock{} }

func (realClock) Now() time.Time { return time.Now() }

func (realClock) NewTimer(deadline time.Time) Timer {
	return &realTimer{t: time.NewTimer(untilNow(deadline))}
}

type realTimer struct{ t *time.Timer }

func (r *realTimer) C() <-chan time.Time { return r.t.C }

func (r *realTimer) Reset(deadline time.Time) bool {
	wasActive := r.t.Stop()
	// 清掉上一輪可能殘留的觸發訊號，避免舊事件汙染新一輪等待。
	// 由於重掛的是絕對時刻，即使訊號被清掉、而該時刻已過，計時器仍會立刻再次觸發，不會漏掉喚醒。
	select {
	case <-r.t.C:
	default:
	}
	r.t.Reset(untilNow(deadline))
	return wasActive
}

func (r *realTimer) Stop() bool { return r.t.Stop() }

// untilNow 把絕對時刻換算成非負的相對時長，換算的時間點刻意落在最接近掛上計時器的一刻。
func untilNow(deadline time.Time) time.Duration {
	if d := time.Until(deadline); d > 0 {
		return d
	}
	return 0
}
