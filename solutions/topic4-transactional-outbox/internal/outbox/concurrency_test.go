package outbox

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 高併發灌入下，每一筆事件都必須恰好被投遞一次（在全部成功的情境下）。
func TestConcurrentPublishDeliversEachEventOnce(t *testing.T) {
	const publishers, perPublisher = 8, 25
	const total = publishers * perPublisher

	sender := newFakeSender()
	eng := newTestEngine(t, NewRealClock(), sender, func(cfg *Config) {
		cfg.Workers = 8
		cfg.MaxInflightPerMerchant = 8
	})

	var wg sync.WaitGroup
	ids := make([]EventID, total)
	for p := 0; p < publishers; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			for i := 0; i < perPublisher; i++ {
				ev := validEvent()
				ev.EventID = EventID(fmt.Sprintf("evt-%d-%d", p, i))
				id, err := eng.PublishEvent(ev)
				if err != nil {
					t.Errorf("PublishEvent() 失敗: %v", err)
					return
				}
				ids[p*perPublisher+i] = id
			}
		}(p)
	}
	wg.Wait()

	for _, id := range ids {
		waitForStatus(t, eng, id, StatusDelivered)
		if n := sender.attemptsFor(id); n != 1 {
			t.Errorf("事件 %s 被投遞 %d 次, 預期恰好 1 次", id, n)
		}
	}
	if n := sender.totalAttempts(); n != total {
		t.Errorf("總投遞次數 = %d, 預期 %d", n, total)
	}
}

// concurrencyTracker 是一個會偵測「同一事件同時被兩個 worker 投遞」的 sender。
type concurrencyTracker struct {
	mu       sync.Mutex
	inFlight map[EventID]int
	// violations 記錄偵測到的重複派送次數。
	violations atomic.Int64
	// maxConcurrent 是同時進行中的投遞數峰值，用於確認測試真的有併發。
	maxConcurrent int
	current       int
}

func (c *concurrencyTracker) Send(_ context.Context, _ string, headers map[string]string, _ []byte) (HTTPResponse, error) {
	id := EventID(headers[headerEventID])

	c.mu.Lock()
	c.inFlight[id]++
	c.current++
	if c.inFlight[id] > 1 {
		c.violations.Add(1)
	}
	if c.current > c.maxConcurrent {
		c.maxConcurrent = c.current
	}
	c.mu.Unlock()

	// 刻意讓投遞停留一小段時間，拉大競爭窗口。
	time.Sleep(time.Millisecond)

	c.mu.Lock()
	c.inFlight[id]--
	c.current--
	c.mu.Unlock()

	return HTTPResponse{StatusCode: 200}, nil
}

// PRD 非功能需求 2：多個 worker 競爭時，絕不可有兩個同時投遞同一筆事件。
func TestNoDoubleDispatchUnderCompetingWorkers(t *testing.T) {
	tracker := &concurrencyTracker{inFlight: make(map[EventID]int)}
	eng := newTestEngine(t, NewRealClock(), tracker, func(cfg *Config) {
		cfg.Workers = 10
		cfg.MaxInflightPerMerchant = 10
	})

	const total = 200
	ids := make([]EventID, total)
	for i := 0; i < total; i++ {
		ev := validEvent()
		ev.EventID = EventID(fmt.Sprintf("evt-%d", i))
		id, err := eng.PublishEvent(ev)
		if err != nil {
			t.Fatalf("PublishEvent() 失敗: %v", err)
		}
		ids[i] = id
	}

	for _, id := range ids {
		waitForStatus(t, eng, id, StatusDelivered)
	}

	if v := tracker.violations.Load(); v != 0 {
		t.Errorf("偵測到 %d 次重複派送", v)
	}
	tracker.mu.Lock()
	peak := tracker.maxConcurrent
	tracker.mu.Unlock()
	if peak < 2 {
		t.Errorf("併發峰值僅 %d，測試未構成有效的競爭條件", peak)
	}
}

// 同一個 EventID 被並發發布時，只有一個能成功——這是 outbox 的去重保證。
func TestConcurrentDuplicatePublish(t *testing.T) {
	sender := newFakeSender()
	eng := newTestEngine(t, NewRealClock(), sender, nil)

	const racers = 16
	var succeeded, duplicates atomic.Int64

	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ev := validEvent()
			ev.EventID = "same-id"
			switch _, err := eng.PublishEvent(ev); {
			case err == nil:
				succeeded.Add(1)
			case errors.Is(err, ErrDuplicateEvent):
				duplicates.Add(1)
			default:
				t.Errorf("非預期的錯誤: %v", err)
			}
		}()
	}
	wg.Wait()

	if succeeded.Load() != 1 {
		t.Errorf("成功發布 %d 次, 預期恰好 1 次", succeeded.Load())
	}
	if duplicates.Load() != racers-1 {
		t.Errorf("被判重複 %d 次, 預期 %d 次", duplicates.Load(), racers-1)
	}

	waitForStatus(t, eng, "same-id", StatusDelivered)
	if n := sender.attemptsFor("same-id"); n != 1 {
		t.Errorf("投遞次數 = %d, 預期 1", n)
	}
}

// 並發 replay 同一筆死信時只有一個能成功，
// 否則同一事件會在佇列中出現多份、被投遞多次。
func TestConcurrentReplayOnlyOneWins(t *testing.T) {
	sender := newFakeSender()
	sender.respond = func(_ EventID, attempt int) (HTTPResponse, error) {
		if attempt == 1 {
			return HTTPResponse{StatusCode: 400}, nil
		}
		return HTTPResponse{StatusCode: 200}, nil
	}
	eng := newTestEngine(t, NewRealClock(), sender, nil)

	ev := validEvent()
	ev.EventID = "to-replay"
	id, _ := eng.PublishEvent(ev)
	waitForStatus(t, eng, id, StatusDeadLetter)

	const racers = 16
	var succeeded atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch err := eng.ReplayEvent(id); {
			case err == nil:
				succeeded.Add(1)
			case errors.Is(err, ErrNotDeadLettered), errors.Is(err, ErrAlreadyDelivered), errors.Is(err, ErrInFlight):
				// 皆為預期：事件已被別人救回並繼續推進。
			default:
				t.Errorf("非預期的錯誤: %v", err)
			}
		}()
	}
	wg.Wait()

	if succeeded.Load() != 1 {
		t.Errorf("replay 成功 %d 次, 預期恰好 1 次", succeeded.Load())
	}
	waitForStatus(t, eng, id, StatusDelivered)
	if n := sender.attemptsFor(id); n != 2 {
		t.Errorf("投遞次數 = %d, 預期 2（首次失敗 + replay 一次）", n)
	}
}

// PRD 非功能需求 3：事件發布不得被緩慢的投遞阻塞。
// 這裡把所有 worker 都卡死，發布仍必須立即返回。
func TestPublishNotBlockedByHangingDeliveries(t *testing.T) {
	sender := newFakeSender()
	sender.block = make(chan struct{})
	defer close(sender.block)

	eng := newTestEngine(t, NewRealClock(), sender, func(cfg *Config) {
		cfg.Workers = 2
		cfg.QueueSize = 2
		cfg.MaxInflightPerMerchant = 2
	})

	// 先讓 worker 與交接佇列全部塞滿，dispatcher 因此卡在交棒上。
	for i := 0; i < 8; i++ {
		ev := validEvent()
		ev.EventID = EventID(fmt.Sprintf("filler-%d", i))
		if _, err := eng.PublishEvent(ev); err != nil {
			t.Fatalf("PublishEvent() 失敗: %v", err)
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 500; i++ {
			ev := validEvent()
			ev.EventID = EventID(fmt.Sprintf("evt-%d", i))
			if _, err := eng.PublishEvent(ev); err != nil {
				t.Errorf("PublishEvent() 失敗: %v", err)
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("投遞全部卡死時，事件發布也被一起阻塞了")
	}
}

// 讀寫混合：Go 對並發讀寫 map 會直接 fatal，因此查詢路徑也必須正確上鎖。
func TestConcurrentReadsAndWrites(t *testing.T) {
	sender := newFakeSender()
	sender.respond = func(id EventID, _ int) (HTTPResponse, error) {
		// 讓一部分事件進 DLQ，好讓 ListDLQ 真的有東西可走訪。
		if len(id)%2 == 0 {
			return HTTPResponse{StatusCode: 400}, nil
		}
		return HTTPResponse{StatusCode: 200}, nil
	}
	eng := newTestEngine(t, NewRealClock(), sender, func(cfg *Config) {
		cfg.Workers = 4
		cfg.MaxInflightPerMerchant = 4
	})

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// 持續發布。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			ev := validEvent()
			ev.EventID = EventID(fmt.Sprintf("evt-%d", i))
			if _, err := eng.PublishEvent(ev); err != nil {
				t.Errorf("PublishEvent() 失敗: %v", err)
				return
			}
		}
	}()

	// 同時持續走訪 DLQ 與查詢狀態。
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func(r int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := eng.ListDLQ(10); err != nil {
					t.Errorf("ListDLQ() 失敗: %v", err)
					return
				}
				if _, err := eng.GetEventStatus(EventID(fmt.Sprintf("evt-%d", r))); err != nil && !errors.Is(err, ErrNotFound) {
					t.Errorf("GetEventStatus() 失敗: %v", err)
					return
				}
			}
		}(r)
	}

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}
