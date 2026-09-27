package paysched

import (
	"sync"
	"testing"
	"time"
)

// fakeClock 是測試用的可手動推進時鐘。
//
// 與生產時鐘一致，計時器以絕對到期時刻掛上：
// 若掛上時該時刻已不晚於當前假時間，就立刻觸發。
// 這消除了「測試已推進時間，但被測 goroutine 才剛掛上計時器」的競態，
// 否則該計時器永遠不會響。
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

func newFakeClock(now time.Time) *fakeClock {
	return &fakeClock{now: now}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) NewTimer(deadline time.Time) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()

	t := &fakeTimer{clock: c, ch: make(chan time.Time, 1)}
	c.timers = append(c.timers, t)
	t.armLocked(deadline)
	return t
}

// Advance 推進假時間並觸發所有到期的計時器。
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
	for _, t := range c.timers {
		t.fireIfDueLocked()
	}
}

// waitForArmedTimers 等到至少 n 個計時器處於待觸發狀態，
// 讓測試能確定被測程式已進入等待後再推進時間。
func (c *fakeClock) waitForArmedTimers(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		armed := 0
		for _, ft := range c.timers {
			if ft.armed {
				armed++
			}
		}
		c.mu.Unlock()
		if armed >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等待 %d 個已掛上的計時器逾時", n)
}

type fakeTimer struct {
	clock    *fakeClock
	ch       chan time.Time
	deadline time.Time
	armed    bool
}

func (t *fakeTimer) C() <-chan time.Time { return t.ch }

func (t *fakeTimer) Reset(deadline time.Time) bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()

	wasArmed := t.armed
	t.armLocked(deadline)
	return wasArmed
}

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()

	wasArmed := t.armed
	t.armed = false
	return wasArmed
}

func (t *fakeTimer) armLocked(deadline time.Time) {
	t.deadline = deadline
	t.armed = true
	// 清掉上一輪殘留的觸發訊號，避免舊事件汙染新一輪等待。
	select {
	case <-t.ch:
	default:
	}
	t.fireIfDueLocked()
}

func (t *fakeTimer) fireIfDueLocked() {
	if !t.armed || t.clock.now.Before(t.deadline) {
		return
	}
	t.armed = false
	select {
	case t.ch <- t.clock.now:
	default:
	}
}
