package paysched

import (
	"errors"
	"testing"
)

// 狀態機的唯一真實來源是 transition table，所有狀態流轉都必須經過 canTransition，
// 避免各處散落 if-else 導致行為漂移。
func TestCanTransition(t *testing.T) {
	cases := []struct {
		name    string
		from    Status
		to      Status
		wantErr error
	}{
		{"排程中可派送", StatusScheduled, StatusDispatched, nil},
		{"排程中可取消", StatusScheduled, StatusCancelled, nil},
		{"派送中可完成", StatusDispatched, StatusCompleted, nil},
		{"派送中可失敗", StatusDispatched, StatusFailed, nil},
		{"派送中可退回排程等重試", StatusDispatched, StatusScheduled, nil},

		{"派送中不可取消", StatusDispatched, StatusCancelled, ErrInProgress},
		{"已取消不可重複取消", StatusCancelled, StatusCancelled, ErrAlreadyCancelled},
		{"已取消不可再派送", StatusCancelled, StatusDispatched, ErrAlreadyCancelled},
		{"已完成不可取消", StatusCompleted, StatusCancelled, ErrAlreadyExecuted},
		{"已完成不可再派送", StatusCompleted, StatusDispatched, ErrAlreadyExecuted},
		{"已失敗不可取消", StatusFailed, StatusCancelled, ErrAlreadyExecuted},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := canTransition(tc.from, tc.to)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("canTransition(%s, %s) = %v, want %v", tc.from, tc.to, err, tc.wantErr)
			}
		})
	}
}

func TestStatusIsTerminal(t *testing.T) {
	terminal := []Status{StatusCompleted, StatusCancelled, StatusFailed}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("%s 應為終態", s)
		}
	}
	for _, s := range []Status{StatusScheduled, StatusDispatched} {
		if s.IsTerminal() {
			t.Errorf("%s 不應為終態", s)
		}
	}
}
