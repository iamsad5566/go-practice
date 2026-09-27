package paysched

import (
	"context"
	"fmt"
	"time"
)

// runWorker 消化交接佇列。Close 時佇列被關閉，worker 把手上與剩餘的付款做完才退出。
func (e *Engine) runWorker() {
	defer e.workerWG.Done()
	for p := range e.dispatch {
		e.process(p)
	}
}

// process 執行單筆付款。
//
// 這裡是「取消 vs 派送」的裁決點：SCHEDULED → DISPATCHED 的流轉刻意壓到
// 真正呼叫外部通道的前一刻才做，讓使用者在款項實際送出前都還能取消。
// 一旦流轉成功，後續的 CancelPayment 會得到 ErrInProgress。
// 外部 I/O 全程不持有任何鎖。
func (e *Engine) process(p *payment) {
	now := e.clock.Now()

	var req PaymentRequest
	err := p.transition(StatusDispatched, now, func(rec *PaymentRecord) {
		rec.DispatchedAt = now
		req = rec.Request
	})
	if err != nil {
		// 對手先取得鎖（通常是取消），或付款已終結：直接丟棄，不碰外部通道。
		return
	}

	// 雙維度限流檢查在狀態流轉之後、外部呼叫之前，
	// 確保被限流的付款不會佔用外部通道的配額。
	allowed, limitErr := e.gate.allow(req.MerchantID, req.ChannelID, req.Cost)
	if limitErr != nil {
		// 設定錯誤（例如 cost 大於桶容量）永遠不可能通過，直接終結。
		e.markFailed(p, limitErr)
		return
	}
	if !allowed {
		e.deferForRateLimit(p)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), e.cfg.ExecuteTimeout)
	defer cancel()

	result, execErr := e.cfg.Executor.Execute(ctx, req)
	if execErr == nil {
		e.markCompleted(p, result)
		return
	}

	if e.cfg.IsRetryable(execErr) && p.snapshot().RetryAttempts < e.cfg.MaxRetries {
		e.scheduleRetry(p, execErr)
		return
	}
	e.markFailed(p, execErr)
}

func (e *Engine) markCompleted(p *payment, result ExecutionResult) {
	now := e.clock.Now()
	_ = p.transition(StatusCompleted, now, func(rec *PaymentRecord) {
		rec.Result = result
		rec.SettledAt = now
		rec.LastError = ""
	})
}

func (e *Engine) markFailed(p *payment, cause error) {
	now := e.clock.Now()
	_ = p.transition(StatusFailed, now, func(rec *PaymentRecord) {
		rec.LastError = cause.Error()
		rec.SettledAt = now
	})
}

// scheduleRetry 讓付款退回 SCHEDULED 並以指數退避 + full jitter 重排。
//
// 退避期間狀態是 SCHEDULED 而非某個特殊的 BACKOFF 態，這是刻意的：
// 款項尚未送出，使用者本就應該能取消，沿用 SCHEDULED 讓取消邏輯不必多一條分支。
func (e *Engine) scheduleRetry(p *payment, cause error) {
	now := e.clock.Now()

	var dueAt time.Time
	_ = p.transition(StatusScheduled, now, func(rec *PaymentRecord) {
		rec.RetryAttempts++
		rec.LastError = cause.Error()
		rec.ExecuteAt = now.Add(e.cfg.backoffFor(rec.RetryAttempts))
		dueAt = rec.ExecuteAt
	})
	e.reschedule(p, dueAt)
}

// deferForRateLimit 處理被限流擋下的付款。
//
// 限流是系統自我節流，不是付款本身失敗，因此刻意不消耗 RetryAttempts 額度，
// 否則一次流量尖峰就會把一批本來會成功的付款推入 FAILED。
// 但延後次數仍需設上限，避免持續壅塞的付款無限重排。
func (e *Engine) deferForRateLimit(p *payment) {
	now := e.clock.Now()

	if p.snapshot().LimiterDeferrals >= e.cfg.MaxLimiterDeferrals {
		e.markFailed(p, fmt.Errorf("%w: deferred %d times", ErrRateLimitExceeded, e.cfg.MaxLimiterDeferrals))
		return
	}

	var dueAt time.Time
	_ = p.transition(StatusScheduled, now, func(rec *PaymentRecord) {
		rec.LimiterDeferrals++
		rec.LastError = ErrRateLimitExceeded.Error()
		rec.ExecuteAt = now.Add(e.cfg.limiterBackoff())
		dueAt = rec.ExecuteAt
	})
	e.reschedule(p, dueAt)
}
