package paysched

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestDuePaymentIsExecuted(t *testing.T) {
	clock := newFakeClock(testEpoch)
	exec := &stubExecutor{}
	eng := newTestEngine(t, clock, exec, nil)

	id, _ := eng.SchedulePayment(validRequest(testEpoch.Add(time.Minute)))

	// 還沒到期就不該執行。
	clock.waitForArmedTimers(t, 1)
	clock.Advance(30 * time.Second)
	if eventually(t, func() bool { return exec.callCount() > 0 }) {
		t.Fatal("未到期的付款不應被執行")
	}

	clock.Advance(30 * time.Second)
	rec := waitForStatus(t, eng, id, StatusCompleted)
	if rec.Result.ExternalRef != "ext-ref" {
		t.Errorf("ExternalRef = %q, want ext-ref", rec.Result.ExternalRef)
	}
	if exec.call(0).IdempotencyKey != string(id) {
		t.Errorf("外部通道未收到冪等鍵: %+v", exec.call(0))
	}
}

// 同一到期時刻才比優先權；時間仍是第一排序鍵。
func TestSameDueTimeRespectsPriority(t *testing.T) {
	clock := newFakeClock(testEpoch)
	exec := &stubExecutor{}
	eng := newTestEngine(t, clock, exec, nil)

	due := testEpoch.Add(time.Minute)
	// 刻意以與預期執行順序相反的順序排入。
	for _, p := range []Priority{PriorityLow, PriorityHigh, PriorityNormal} {
		req := validRequest(due)
		req.Priority = p
		if _, err := eng.SchedulePayment(req); err != nil {
			t.Fatalf("SchedulePayment() 失敗: %v", err)
		}
	}

	clock.waitForArmedTimers(t, 1)
	clock.Advance(time.Minute)

	if !eventually(t, func() bool { return exec.callCount() == 3 }) {
		t.Fatalf("三筆付款應全部執行, got %d", exec.callCount())
	}
	want := []Priority{PriorityHigh, PriorityNormal, PriorityLow}
	for i, p := range want {
		if got := exec.call(i).Priority; got != p {
			t.Fatalf("第 %d 筆執行的優先權 = %s, want %s", i, got, p)
		}
	}
}

// 高優先權不得提前於預定時間送出。
func TestHighPriorityDoesNotJumpAheadOfTime(t *testing.T) {
	clock := newFakeClock(testEpoch)
	exec := &stubExecutor{}
	eng := newTestEngine(t, clock, exec, nil)

	early := validRequest(testEpoch.Add(time.Minute))
	early.Priority = PriorityLow
	lowID, _ := eng.SchedulePayment(early)

	late := validRequest(testEpoch.Add(2 * time.Minute))
	late.Priority = PriorityHigh
	eng.SchedulePayment(late)

	clock.waitForArmedTimers(t, 1)
	clock.Advance(time.Minute)
	waitForStatus(t, eng, lowID, StatusCompleted)

	if got := exec.call(0).Priority; got != PriorityLow {
		t.Fatalf("先執行的應是較早到期的 LOW, got %s", got)
	}
}

func TestRetryOnTransientErrorThenSucceeds(t *testing.T) {
	clock := newFakeClock(testEpoch)
	exec := &stubExecutor{fn: func(attempt int, req PaymentRequest) (ExecutionResult, error) {
		if attempt == 0 {
			return ExecutionResult{}, ErrChannelBusy
		}
		return ExecutionResult{ExternalRef: "ext-ref"}, nil
	}}
	eng := newTestEngine(t, clock, exec, nil)

	id, _ := eng.SchedulePayment(validRequest(testEpoch.Add(time.Minute)))
	clock.waitForArmedTimers(t, 1)
	clock.Advance(time.Minute)

	// 第一次失敗後退回 SCHEDULED 等待退避，此時仍可取消。
	if !eventually(t, func() bool {
		rec, _ := eng.GetPaymentStatus(id)
		return rec.Status == StatusScheduled && rec.RetryAttempts == 1
	}) {
		rec, _ := eng.GetPaymentStatus(id)
		t.Fatalf("重試後應回到 SCHEDULED 且 RetryAttempts=1, got %s/%d", rec.Status, rec.RetryAttempts)
	}
	rec, _ := eng.GetPaymentStatus(id)
	if rec.LastError == "" {
		t.Error("重試後應記錄上次錯誤細節")
	}
	// BaseBackoff=1s、jitter 取上界，故下次執行時間為失敗後 1 秒。
	if want := testEpoch.Add(time.Minute + time.Second); !rec.ExecuteAt.Equal(want) {
		t.Errorf("ExecuteAt = %v, want %v", rec.ExecuteAt, want)
	}

	clock.waitForArmedTimers(t, 1)
	clock.Advance(time.Second)
	final := waitForStatus(t, eng, id, StatusCompleted)
	if final.RetryAttempts != 1 {
		t.Errorf("RetryAttempts = %d, want 1", final.RetryAttempts)
	}
	// 所有重試都必須帶同一個冪等鍵，否則下游會重複扣款。
	if exec.call(0).IdempotencyKey != exec.call(1).IdempotencyKey {
		t.Error("重試的冪等鍵不一致")
	}
}

func TestRetryBackoffIsExponential(t *testing.T) {
	clock := newFakeClock(testEpoch)
	exec := &stubExecutor{fn: func(attempt int, req PaymentRequest) (ExecutionResult, error) {
		return ExecutionResult{}, ErrNetworkTimeout
	}}
	eng := newTestEngine(t, clock, exec, func(c *Config) { c.MaxRetries = 3 })

	due := testEpoch.Add(time.Minute)
	id, _ := eng.SchedulePayment(validRequest(due))
	clock.waitForArmedTimers(t, 1)
	clock.Advance(time.Minute)

	// 退避序列為 1s、2s、4s（BaseBackoff * 2^(n-1)，jitter 取上界）。
	elapsed := time.Duration(0)
	for attempt, backoff := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		if !eventually(t, func() bool {
			rec, _ := eng.GetPaymentStatus(id)
			return rec.RetryAttempts == attempt+1 && rec.Status == StatusScheduled
		}) {
			rec, _ := eng.GetPaymentStatus(id)
			t.Fatalf("第 %d 次重試未就緒: %s/%d", attempt+1, rec.Status, rec.RetryAttempts)
		}
		rec, _ := eng.GetPaymentStatus(id)
		want := due.Add(elapsed + backoff)
		if !rec.ExecuteAt.Equal(want) {
			t.Fatalf("第 %d 次重試的 ExecuteAt = %v, want %v", attempt+1, rec.ExecuteAt, want)
		}
		elapsed += backoff
		clock.waitForArmedTimers(t, 1)
		clock.Advance(backoff)
	}

	rec := waitForStatus(t, eng, id, StatusFailed)
	if rec.RetryAttempts != 3 {
		t.Errorf("RetryAttempts = %d, want 3", rec.RetryAttempts)
	}
	if rec.LastError == "" {
		t.Error("終態 FAILED 應保留最後的錯誤細節")
	}
	if exec.callCount() != 4 {
		t.Errorf("執行次數 = %d, want 4 (首次 + 3 次重試)", exec.callCount())
	}
}

func TestBackoffIsCappedByMaxBackoff(t *testing.T) {
	clock := newFakeClock(testEpoch)
	exec := &stubExecutor{fn: func(attempt int, req PaymentRequest) (ExecutionResult, error) {
		return ExecutionResult{}, ErrChannelBusy
	}}
	eng := newTestEngine(t, clock, exec, func(c *Config) {
		c.MaxRetries = 3
		c.MaxBackoff = 1500 * time.Millisecond
	})

	due := testEpoch.Add(time.Minute)
	id, _ := eng.SchedulePayment(validRequest(due))
	clock.waitForArmedTimers(t, 1)
	clock.Advance(time.Minute)

	// 第一次退避為 BaseBackoff=1s，推進後才會進入第二次重試。
	if !eventually(t, func() bool {
		rec, _ := eng.GetPaymentStatus(id)
		return rec.RetryAttempts == 1
	}) {
		t.Fatal("未進行到第一次重試")
	}
	clock.waitForArmedTimers(t, 1)
	clock.Advance(time.Second)

	if !eventually(t, func() bool {
		rec, _ := eng.GetPaymentStatus(id)
		return rec.RetryAttempts == 2
	}) {
		t.Fatal("未進行到第二次重試")
	}
	rec, _ := eng.GetPaymentStatus(id)
	if want := due.Add(time.Second + 1500*time.Millisecond); !rec.ExecuteAt.Equal(want) {
		t.Fatalf("第二次退避未被 MaxBackoff 夾住: ExecuteAt = %v, want %v", rec.ExecuteAt, want)
	}
}

func TestNonRetryableErrorFailsImmediately(t *testing.T) {
	clock := newFakeClock(testEpoch)
	fatal := errors.New("account frozen")
	exec := &stubExecutor{fn: func(attempt int, req PaymentRequest) (ExecutionResult, error) {
		return ExecutionResult{}, fatal
	}}
	eng := newTestEngine(t, clock, exec, nil)

	id, _ := eng.SchedulePayment(validRequest(testEpoch.Add(time.Minute)))
	clock.waitForArmedTimers(t, 1)
	clock.Advance(time.Minute)

	rec := waitForStatus(t, eng, id, StatusFailed)
	if rec.RetryAttempts != 0 {
		t.Errorf("不可重試的錯誤不應消耗重試額度, got %d", rec.RetryAttempts)
	}
	if exec.callCount() != 1 {
		t.Errorf("執行次數 = %d, want 1", exec.callCount())
	}
}

// 呼叫端自訂錯誤可透過 RetryableError 介面宣告可重試，無須改動引擎。
type customRetryable struct{}

func (customRetryable) Error() string   { return "custom transient" }
func (customRetryable) Retryable() bool { return true }

func TestCustomRetryableError(t *testing.T) {
	clock := newFakeClock(testEpoch)
	exec := &stubExecutor{fn: func(attempt int, req PaymentRequest) (ExecutionResult, error) {
		if attempt == 0 {
			return ExecutionResult{}, fmt.Errorf("wrapped: %w", customRetryable{})
		}
		return ExecutionResult{ExternalRef: "ext-ref"}, nil
	}}
	eng := newTestEngine(t, clock, exec, nil)

	id, _ := eng.SchedulePayment(validRequest(testEpoch.Add(time.Minute)))
	clock.waitForArmedTimers(t, 1)
	clock.Advance(time.Minute)

	if !eventually(t, func() bool {
		rec, _ := eng.GetPaymentStatus(id)
		return rec.RetryAttempts == 1
	}) {
		t.Fatal("自訂可重試錯誤未觸發重試")
	}
	clock.waitForArmedTimers(t, 1)
	clock.Advance(time.Second)
	waitForStatus(t, eng, id, StatusCompleted)
}

// 退避等待期間狀態為 SCHEDULED，因此仍可取消；取消後不得再送外部通道。
func TestCancelDuringBackoff(t *testing.T) {
	clock := newFakeClock(testEpoch)
	exec := &stubExecutor{fn: func(attempt int, req PaymentRequest) (ExecutionResult, error) {
		return ExecutionResult{}, ErrChannelBusy
	}}
	eng := newTestEngine(t, clock, exec, nil)

	id, _ := eng.SchedulePayment(validRequest(testEpoch.Add(time.Minute)))
	clock.waitForArmedTimers(t, 1)
	clock.Advance(time.Minute)

	if !eventually(t, func() bool {
		rec, _ := eng.GetPaymentStatus(id)
		return rec.Status == StatusScheduled && rec.RetryAttempts == 1
	}) {
		t.Fatal("未進入退避等待")
	}

	if err := eng.CancelPayment(id); err != nil {
		t.Fatalf("退避期間應可取消: %v", err)
	}

	clock.waitForArmedTimers(t, 1)
	clock.Advance(time.Minute)
	if eventually(t, func() bool { return exec.callCount() > 1 }) {
		t.Fatal("取消後不應再呼叫外部通道")
	}
	if rec, _ := eng.GetPaymentStatus(id); rec.Status != StatusCancelled {
		t.Fatalf("Status = %s, want CANCELLED", rec.Status)
	}
}

// 被限流擋下時，付款以退避重排，且不消耗 MaxRetries 額度。
func TestRateLimitedPaymentIsDeferredWithoutConsumingRetries(t *testing.T) {
	clock := newFakeClock(testEpoch)
	exec := &stubExecutor{}
	eng := newTestEngine(t, clock, exec, func(c *Config) {
		c.Channel = LimiterConfig{Default: BucketConfig{Capacity: 1, RefillPerSecond: 1}}
		c.LimiterBackoff = time.Second
	})

	due := testEpoch.Add(time.Minute)
	first, _ := eng.SchedulePayment(validRequest(due))
	second, _ := eng.SchedulePayment(validRequest(due))

	clock.waitForArmedTimers(t, 1)
	clock.Advance(time.Minute)

	waitForStatus(t, eng, first, StatusCompleted)
	if !eventually(t, func() bool {
		rec, _ := eng.GetPaymentStatus(second)
		return rec.Status == StatusScheduled && rec.LimiterDeferrals == 1
	}) {
		rec, _ := eng.GetPaymentStatus(second)
		t.Fatalf("第二筆應被限流延後, got %s/deferrals=%d", rec.Status, rec.LimiterDeferrals)
	}
	rec, _ := eng.GetPaymentStatus(second)
	if rec.RetryAttempts != 0 {
		t.Errorf("限流延後不應消耗重試額度, RetryAttempts = %d", rec.RetryAttempts)
	}

	// 一秒後通道補回 1 顆 token，第二筆得以完成。
	clock.waitForArmedTimers(t, 1)
	clock.Advance(time.Second)
	waitForStatus(t, eng, second, StatusCompleted)
}

func TestLimiterDeferralsExhaustedFails(t *testing.T) {
	clock := newFakeClock(testEpoch)
	exec := &stubExecutor{}
	eng := newTestEngine(t, clock, exec, func(c *Config) {
		// 容量 1 且補充極慢，讓第二筆付款持續被擋。
		c.Channel = LimiterConfig{Default: BucketConfig{Capacity: 1, RefillPerSecond: 0.0001}}
		c.LimiterBackoff = time.Second
		c.MaxLimiterDeferrals = 2
	})

	due := testEpoch.Add(time.Minute)
	first, _ := eng.SchedulePayment(validRequest(due))
	second, _ := eng.SchedulePayment(validRequest(due))

	clock.waitForArmedTimers(t, 1)
	clock.Advance(time.Minute)
	waitForStatus(t, eng, first, StatusCompleted)

	for i := 0; i < 2; i++ {
		clock.waitForArmedTimers(t, 1)
		clock.Advance(time.Second)
	}

	rec := waitForStatus(t, eng, second, StatusFailed)
	if rec.LastError == "" {
		t.Error("應記錄限流導致的失敗原因")
	}
	if rec.RetryAttempts != 0 {
		t.Errorf("RetryAttempts = %d, want 0", rec.RetryAttempts)
	}
}
