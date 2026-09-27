package outbox

import (
	"errors"
	"testing"
)

func TestStatusIsTerminal(t *testing.T) {
	cases := map[Status]bool{
		StatusUnknown:    false,
		StatusPending:    false,
		StatusInFlight:   false,
		StatusRetrying:   false,
		StatusDelivered:  true,
		StatusDeadLetter: true,
	}
	for status, want := range cases {
		if got := status.IsTerminal(); got != want {
			t.Errorf("%s.IsTerminal() = %v, 預期 %v", status, got, want)
		}
	}
}

// 派送迴圈只該碰「等待中」的事件；IN_FLIGHT 已被別的 worker claim，終態則不再派送。
func TestStatusIsDispatchable(t *testing.T) {
	cases := map[Status]bool{
		StatusUnknown:    false,
		StatusPending:    true,
		StatusRetrying:   true,
		StatusInFlight:   false,
		StatusDelivered:  false,
		StatusDeadLetter: false,
	}
	for status, want := range cases {
		if got := status.IsDispatchable(); got != want {
			t.Errorf("%s.IsDispatchable() = %v, 預期 %v", status, got, want)
		}
	}
}

func TestCanTransitionAllowed(t *testing.T) {
	allowed := []struct{ from, to Status }{
		{StatusPending, StatusInFlight},
		{StatusRetrying, StatusInFlight},
		{StatusInFlight, StatusDelivered},
		{StatusInFlight, StatusRetrying},
		{StatusInFlight, StatusDeadLetter},
		// 唯一能回到 PENDING 的路徑是人工 replay。
		{StatusDeadLetter, StatusPending},
	}
	for _, c := range allowed {
		if err := canTransition(c.from, c.to); err != nil {
			t.Errorf("canTransition(%s, %s) = %v, 預期 nil", c.from, c.to, err)
		}
	}
}

func TestCanTransitionRejected(t *testing.T) {
	cases := []struct {
		from, to Status
		wantErr  error
	}{
		// 已送達是真正的終態，replay 也不該讓它重新投遞。
		{StatusDelivered, StatusPending, ErrAlreadyDelivered},
		{StatusDelivered, StatusInFlight, ErrAlreadyDelivered},
		// 正在飛的事件不可被重複 claim——這是防雙重派送的最後一道閘。
		{StatusInFlight, StatusInFlight, ErrInFlight},
		// replay 只對 DLQ 內的事件有意義。
		{StatusPending, StatusPending, ErrNotDeadLettered},
		{StatusRetrying, StatusPending, ErrNotDeadLettered},
		// DLQ 事件除了 replay 之外無路可走。
		{StatusDeadLetter, StatusInFlight, ErrDeadLettered},
		{StatusDeadLetter, StatusDelivered, ErrDeadLettered},
		{StatusUnknown, StatusInFlight, ErrInvalidTransition},
	}
	for _, c := range cases {
		err := canTransition(c.from, c.to)
		if !errors.Is(err, c.wantErr) {
			t.Errorf("canTransition(%s, %s) = %v, 預期 %v", c.from, c.to, err, c.wantErr)
		}
	}
}
