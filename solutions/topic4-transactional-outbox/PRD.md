# PRD: Transactional Outbox & Reliable Webhook Dispatcher Engine

## 1. System Background & Objective
When Circle executes blockchain settlements, cross-border payouts, or mint/burn operations, merchant applications must be notified in real time via HTTP Webhooks. Because merchant servers may experience network outages, service downtime, or deploy delays, Circle's notification infrastructure must guarantee **At-Least-Once Delivery** and **Zero Event Loss**, without blocking primary settlement transactions.

The objective of this project is to implement an in-memory prototype of the **Transactional Outbox & Reliable Webhook Dispatcher Engine** in Go. The engine decouples business event publication from asynchronous delivery, supports concurrent worker dispatching, enforces cryptographic payload signing, handles retry backoffs, and isolates persistent failures into a Dead Letter Queue (DLQ).

---

## 2. Functional Requirements

### 2.1 Outbox Event Enqueue (Transactional Emission)
- **`PublishEvent(event OutboxEvent) (EventID, error)`**
  - Accepts an event payload targeting a specific `MerchantID` and `DestinationURL`.
  - Event metadata includes: `EventID` (unique string/UUID), `EventType` (e.g. `payment.settled`, `payout.failed`), `Payload` (JSON bytes or structured data), `CreatedAt time.Time`.
  - Must atomically register the event in the outbox in `PENDING` state.
  - Generates a unique monotonic sequence number or timestamp to support per-merchant ordering.

### 2.2 Concurrent Dispatcher & Worker Pool
- **Asynchronous Dispatching**:
  - Background dispatcher continuously fetches `PENDING` events and distributes them across a bounded pool of concurrent workers.
  - Dispatches Webhook payloads via HTTP POST to the merchant's `DestinationURL`.
- **Security & Cryptographic Signing**:
  - Outgoing HTTP requests must include:
    - `X-Delivery-ID`: Unique identifier for each delivery attempt (to aid merchant deduplication).
    - `X-Event-ID`: Canonical event ID.
    - `X-Timestamp`: Unix timestamp (to guard against replay attacks).
    - `X-Signature-SHA256`: HMAC-SHA256 signature generated using a pre-configured merchant secret key:
      $$\text{Signature} = \text{HMAC-SHA256}(\text{Secret}, \text{timestamp} + "." + \text{payload})$$
- **Dispatcher Abstraction**:
  ```go
  type WebhookSender interface {
      Send(ctx context.Context, destURL string, headers map[string]string, payload []byte) (HTTPResponse, error)
  }
  ```

### 2.3 Failure Handling, Exponential Backoff & DLQ
- If a delivery attempt fails (network timeout, connection refused, or HTTP 5xx / 429 status):
  - Transitions event to `RETRYING`.
  - Applies Exponential Backoff with Jitter for next attempt time (`NextAttemptAt`).
- If a delivery returns a non-retryable 4xx error (e.g., 400 Bad Request, 401 Unauthorized), or if `MaxRetries` is exhausted:
  - Transitions event to `DEAD_LETTER` (DLQ).
  - Records failure reason, attempt count, and last HTTP status code.

### 2.4 DLQ Inspection & Replay
- **`GetEventStatus(eventID string) (EventRecord, error)`**: Returns current delivery lifecycle state and attempt history.
- **`ListDLQ(limit int) ([]EventRecord, error)`**: Lists events currently in dead letter state.
- **`ReplayEvent(eventID string) error`**: Manually or programmatically resets a dead-lettered event back to `PENDING` with reset retry counters.

### 2.5 Graceful Shutdown
- Exposes `Close()` or consumes `context.Context` to:
  - Stop accepting new `PublishEvent` calls.
  - Cease polling new pending events.
  - Allow in-flight worker HTTP requests to complete within a configurable shutdown timeout.
  - Zero goroutine leaks.

---

## 3. Non-Functional Requirements

1. **Thread Safety & Race-Free Concurrency**
   - 100% clean under `go test -v -race ./...`.
   - Guaranteed deadlock-free under high-concurrency ingestion and competing worker dispatching.
2. **Competing Worker Deduplication (No Double Dispatch)**
   - Multiple concurrent workers must NEVER claim and dispatch the exact same event at the same time.
   - Status transition from `PENDING` to `IN_FLIGHT` must be atomic (Check-then-Act / CAS).
3. **Low Contention & High Throughput**
   - Ingestion (`PublishEvent`) must not be blocked by slow or hanging HTTP worker deliveries.
4. **Clean Abstraction & Testability**
   - Storage, HTTP Sender, and Clock abstractions must be decoupled via Go interfaces for deterministic unit testing.

---

## 4. Intentional Ambiguities & Interview Traps

*These areas are intentionally left open to evaluate candidate's architectural leadership and clarification rigor:*
1. **Per-Merchant FIFO vs. Global Worker Concurrency**:
   - If a merchant receives two events: `payment.created` and `payment.settled`, but the first one fails and enters retry backoff, should the second event proceed or wait? What are the trade-offs of Strict FIFO vs. Out-of-Order delivery?
2. **Double-Dispatch Prevention in Competing Workers**:
   - When 10 workers concurrently pull from the in-memory outbox, how do you prevent two workers from picking up the exact same event?
3. **Poison Pill Endpoints**:
   - If a merchant's server is down or slow (takes 30 seconds to timeout), how do you prevent slow merchant endpoints from exhausting your worker pool and starving all other merchants?
4. **At-Least-Once Delivery & Merchant Idempotency**:
   - If the merchant processes the webhook successfully but their HTTP 200 response drops due to a network glitch, how does the system design help the merchant maintain idempotency?
