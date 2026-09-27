package paysched

import (
	"errors"
	"testing"
	"time"
)

func TestSchedulePaymentRejectsInvalidRequests(t *testing.T) {
	clock := newFakeClock(testEpoch)
	eng := newTestEngine(t, clock, &stubExecutor{}, nil)

	future := testEpoch.Add(time.Minute)
	cases := []struct {
		name    string
		mutate  func(*PaymentRequest)
		wantErr error
	}{
		{"ExecuteAt 為零值", func(r *PaymentRequest) { r.ExecuteAt = time.Time{} }, ErrPastExecuteAt},
		{"ExecuteAt 已過期", func(r *PaymentRequest) { r.ExecuteAt = testEpoch.Add(-time.Second) }, ErrPastExecuteAt},
		{"ExecuteAt 等於當下", func(r *PaymentRequest) { r.ExecuteAt = testEpoch }, ErrPastExecuteAt},
		{"缺 MerchantID", func(r *PaymentRequest) { r.MerchantID = "" }, ErrInvalidRequest},
		{"缺 ChannelID", func(r *PaymentRequest) { r.ChannelID = "" }, ErrInvalidRequest},
		{"金額非正數", func(r *PaymentRequest) { r.Amount = 0 }, ErrInvalidRequest},
		{"缺幣別", func(r *PaymentRequest) { r.Currency = "" }, ErrInvalidRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validRequest(future)
			tc.mutate(&req)
			if _, err := eng.SchedulePayment(req); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestSchedulePaymentInitialRecord(t *testing.T) {
	clock := newFakeClock(testEpoch)
	eng := newTestEngine(t, clock, &stubExecutor{}, nil)

	executeAt := testEpoch.Add(time.Hour)
	id, err := eng.SchedulePayment(validRequest(executeAt))
	if err != nil {
		t.Fatalf("SchedulePayment() 失敗: %v", err)
	}

	rec, err := eng.GetPaymentStatus(id)
	if err != nil {
		t.Fatalf("GetPaymentStatus() 失敗: %v", err)
	}
	if rec.Status != StatusScheduled {
		t.Errorf("Status = %s, want %s", rec.Status, StatusScheduled)
	}
	if !rec.ExecuteAt.Equal(executeAt) {
		t.Errorf("ExecuteAt = %v, want %v", rec.ExecuteAt, executeAt)
	}
	// 冪等鍵由引擎填入且等同 ScheduleID，重試時才能讓下游辨識為同一筆付款。
	if rec.Request.IdempotencyKey != string(id) {
		t.Errorf("IdempotencyKey = %q, want %q", rec.Request.IdempotencyKey, id)
	}
	if rec.Request.Cost != 1 {
		t.Errorf("Cost 零值應正規化為 1, got %d", rec.Request.Cost)
	}
}

func TestScheduleIDsAreUnique(t *testing.T) {
	eng := newTestEngine(t, newFakeClock(testEpoch), &stubExecutor{}, nil)

	seen := make(map[ScheduleID]bool)
	for i := 0; i < 100; i++ {
		id, err := eng.SchedulePayment(validRequest(testEpoch.Add(time.Hour)))
		if err != nil {
			t.Fatalf("SchedulePayment() 失敗: %v", err)
		}
		if seen[id] {
			t.Fatalf("ScheduleID 重複: %s", id)
		}
		seen[id] = true
	}
}

func TestGetPaymentStatusUnknownID(t *testing.T) {
	eng := newTestEngine(t, newFakeClock(testEpoch), &stubExecutor{}, nil)
	if _, err := eng.GetPaymentStatus("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// GetPaymentStatus 回傳的是快照，呼叫端改動它不得影響引擎內部狀態。
func TestGetPaymentStatusReturnsSnapshot(t *testing.T) {
	eng := newTestEngine(t, newFakeClock(testEpoch), &stubExecutor{}, nil)
	id, _ := eng.SchedulePayment(validRequest(testEpoch.Add(time.Hour)))

	rec, _ := eng.GetPaymentStatus(id)
	rec.Status = StatusFailed
	rec.RetryAttempts = 99

	again, _ := eng.GetPaymentStatus(id)
	if again.Status != StatusScheduled || again.RetryAttempts != 0 {
		t.Fatalf("內部狀態被外部快照汙染: %+v", again)
	}
}

func TestCancelPaymentBeforeDue(t *testing.T) {
	clock := newFakeClock(testEpoch)
	exec := &stubExecutor{}
	eng := newTestEngine(t, clock, exec, nil)

	id, _ := eng.SchedulePayment(validRequest(testEpoch.Add(time.Minute)))
	if err := eng.CancelPayment(id); err != nil {
		t.Fatalf("CancelPayment() 失敗: %v", err)
	}

	rec, _ := eng.GetPaymentStatus(id)
	if rec.Status != StatusCancelled {
		t.Fatalf("Status = %s, want CANCELLED", rec.Status)
	}

	// 時間推過原定執行時刻後，已取消的付款仍不得送往外部通道。
	clock.waitForArmedTimers(t, 1)
	clock.Advance(2 * time.Minute)
	if eventually(t, func() bool { return exec.callCount() > 0 }) {
		t.Fatal("已取消的付款不應被執行")
	}
}

func TestCancelPaymentErrorContract(t *testing.T) {
	clock := newFakeClock(testEpoch)
	eng := newTestEngine(t, clock, &stubExecutor{}, nil)

	if err := eng.CancelPayment("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("取消不存在的付款 err = %v, want ErrNotFound", err)
	}

	id, _ := eng.SchedulePayment(validRequest(testEpoch.Add(time.Minute)))
	if err := eng.CancelPayment(id); err != nil {
		t.Fatalf("首次取消應成功: %v", err)
	}
	if err := eng.CancelPayment(id); !errors.Is(err, ErrAlreadyCancelled) {
		t.Fatalf("重複取消 err = %v, want ErrAlreadyCancelled", err)
	}
}

func TestCancelAfterCompletedReturnsAlreadyExecuted(t *testing.T) {
	clock := newFakeClock(testEpoch)
	eng := newTestEngine(t, clock, &stubExecutor{}, nil)

	id, _ := eng.SchedulePayment(validRequest(testEpoch.Add(time.Minute)))
	clock.waitForArmedTimers(t, 1)
	clock.Advance(time.Minute)
	waitForStatus(t, eng, id, StatusCompleted)

	if err := eng.CancelPayment(id); !errors.Is(err, ErrAlreadyExecuted) {
		t.Fatalf("err = %v, want ErrAlreadyExecuted", err)
	}
}

// 取消與派送的毫秒級競爭：worker 已進入結算作業（DISPATCHED）後，取消必須明確失敗。
func TestCancelDuringDispatchReturnsInProgress(t *testing.T) {
	clock := newFakeClock(testEpoch)
	started := make(chan struct{})
	release := make(chan struct{})
	exec := &stubExecutor{fn: func(attempt int, req PaymentRequest) (ExecutionResult, error) {
		close(started)
		<-release
		return ExecutionResult{ExternalRef: "ext-ref"}, nil
	}}
	eng := newTestEngine(t, clock, exec, nil)

	id, _ := eng.SchedulePayment(validRequest(testEpoch.Add(time.Minute)))
	clock.waitForArmedTimers(t, 1)
	clock.Advance(time.Minute)

	<-started
	if err := eng.CancelPayment(id); !errors.Is(err, ErrInProgress) {
		t.Fatalf("err = %v, want ErrInProgress", err)
	}
	if rec, _ := eng.GetPaymentStatus(id); rec.Status != StatusDispatched {
		t.Fatalf("Status = %s, want DISPATCHED", rec.Status)
	}

	close(release)
	waitForStatus(t, eng, id, StatusCompleted)
}

func TestClosedEngineRejectsNewSchedules(t *testing.T) {
	eng := newTestEngine(t, newFakeClock(testEpoch), &stubExecutor{}, nil)
	eng.Close()

	if _, err := eng.SchedulePayment(validRequest(testEpoch.Add(time.Minute))); !errors.Is(err, ErrEngineClosed) {
		t.Fatalf("err = %v, want ErrEngineClosed", err)
	}
	// Close 必須可重入。
	eng.Close()
}

func TestNewValidatesConfig(t *testing.T) {
	if _, err := New(Config{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("缺少 Executor 應回 ErrInvalidRequest, got %v", err)
	}
}
