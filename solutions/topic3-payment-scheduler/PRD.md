# PRD: Distributed-Ready Payment Scheduler & Multi-Tenant Rate-Limiting Gateway

## 1. System Background & Objective
Circle's payment and settlement infrastructure orchestrates millions of transactions interacting with diverse external financial counterparties—including traditional central bank clearing rails (Fedwire, ACH, SEPA) and public blockchain RPC nodes. These external networks impose strict **QPS rate limits** and volume caps. Concurrently, enterprise merchants require **scheduled payment execution** for automated recurring disbursements, payroll, and deferred settlements.

The objective of this project is to design and implement a high-throughput, thread-safe, and zero-resource-leak **In-Memory Payment Scheduler & Multi-Tenant Rate-Limiter Gateway Engine** in Go.

---

## 2. Functional Requirements

### 2.1 Payment Scheduling Engine
- **`SchedulePayment(payment PaymentRequest) (ScheduleID, error)`**
  - Accepts a payment request with a specified execution timestamp (`ExecuteAt time.Time`) and priority tier (`Priority`: High, Normal, Low).
  - Generates a unique `ScheduleID`.
  - Must define explicit behavior when `ExecuteAt` is in the past or zero-value (e.g., immediate execution vs. rejection).
- **`CancelPayment(scheduleID ScheduleID) error`**
  - Allows merchants to cancel a pending scheduled payment prior to dispatch.
  - If the payment is already in-flight or completed, must return a deterministic error (`ErrAlreadyExecuted`, `ErrInProgress`, or `ErrNotFound`).
- **Background Dispatcher & Worker Pool**:
  - Dynamically triggers tasks as they reach their due timestamps, respecting priority ordering.
  - Dispatches tasks to an external payment executor abstraction:
    ```go
    type PaymentExecutor interface {
        Execute(ctx context.Context, payment PaymentRequest) (ExecutionResult, error)
    }
    ```
- **Graceful Shutdown**:
  - Exposes `Close()` or consumes `context.Context` to stop dispatching new payments and gracefully terminate workers without leaking goroutines or active timers.

### 2.2 Multi-Tenant Token Bucket Rate Limiter
- **`Allow(tenantID string, cost int64) (bool, error)`** or **`Wait(ctx context.Context, tenantID string, cost int64) error`**
  - Supports dual-dimensional rate limiting:
    - **Merchant Level (`MerchantID`)**: Prevents noisy-neighbor starvation.
    - **Channel Level (`ChannelID`)**: Protects bank clearing rails from rate breaches.
  - Implements the **Token Bucket Algorithm**:
    - Parameterized by `Capacity` (maximum burst allowance) and `RefillRate` (tokens replenished per second).
  - Configurable policy when capacity is exceeded: immediate rejection (Fail-Fast with `ErrRateLimitExceeded`) or bounded waiting with context deadline.

### 2.3 Failure Handling & Exponential Backoff
- When `PaymentExecutor` returns transient retryable errors (e.g., `ErrChannelBusy`, `ErrNetworkTimeout`), the scheduler re-enqueues the payment.
- Backoff intervals must apply **Exponential Backoff with Full Jitter** to prevent thundering herd spikes against downstream rails.
- Enforces `MaxRetries`. Once exhausted, transitions the transaction to terminal state `FAILED`.

### 2.4 Status Inspection & Audit Trail
- **`GetPaymentStatus(scheduleID ScheduleID) (PaymentRecord, error)`**
  - Returns current lifecycle state (`SCHEDULED`, `DISPATCHED`, `COMPLETED`, `CANCELLED`, `FAILED`).
  - Includes retry attempt count, last error details, and execution timestamps.

---

## 3. Non-Functional Requirements

1. **Thread Safety & Race-Free Concurrency**
   - 100% compliant with `go test -v -race ./...`. Absolutely zero data races under concurrent scheduling, cancellations, rate queries, and executions.
   - Guaranteed deadlock-free under high contention.
2. **Zero Resource Leaks (Timer & Goroutine Hygiene)**
   - Strict prohibition of uncollected `time.After(...)` allocations that cause unbounded memory accumulation.
   - Scheduler must use a unified coordinator (e.g., Min-Heap Priority Queue or Hashed Wheel) driven by an efficiently reset `time.Timer` or channel notifications.
3. **Low Contention & High Throughput**
   - Rate limiting must utilize lock-free atomic math or granular sharded mutexes with **Lazy Refill** (computing tokens on-demand via elapsed time rather than periodic background ticker polling).
4. **Clean Abstraction & Interface Decoupling**
   - Storage and execution mechanisms must be decoupled behind Go interfaces to facilitate future database or distributed queue integration.

---

## 4. Intentional Ambiguities & Interview Traps

*These areas are deliberately underspecified to evaluate how the candidate clarifies requirements and defines technical assumptions:*
1. **Cancellation Race Condition**: What happens if a merchant issues `CancelPayment` in the exact millisecond a worker pulls the payment from the queue to call the bank? How is atomic adjudication handled?
2. **Timer Explosion vs. Scalability**: If 500,000 future payments are scheduled across varying days, how does the scheduling loop manage wake-up timers without exhausting OS resources?
3. **Token Bucket Computation**: Does the rate limiter spawn a background goroutine ticking every millisecond, or compute token replenishment lazily upon arrival? What are the trade-offs?
4. **Idempotency Guarantees**: When a transient error triggers a retry, how does the scheduler prevent duplicate debits on non-idempotent banking endpoints?
