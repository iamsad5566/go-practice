# Architecture-First Prompt Templates for AI Interviews

Use these copy-and-adapt prompt templates when instructing Claude/Copilot during technical interviews.

---

## Template 1: Level 1 - Foundational Domain Model & CRUD

```markdown
Claude, we are building a high-concurrency, in-memory settlement engine in Go.

### Architectural Decisions:
1. Concurrency: Use a two-level locking model:
   - A registry-level `sync.RWMutex` to guard the account map topology.
   - An entity-level `sync.Mutex` inside each `Account` struct to protect balance modifications.
2. Encapsulation: Keep struct fields unexported. All modifications must go through receiver methods.
3. Overflow Protection: In deposit operations, explicitly guard against `math.MaxInt64` integer overflow.

### Interfaces & Types:
[Paste Interface and Custom Errors here]

### Requirements:
- Implement `CreateAccount`, `Deposit`, `Withdraw`, and `GetBalance`.
- Provide guard clauses for empty IDs, non-positive amounts, non-existent accounts, and insufficient funds.
- Write clean, idiomatic Go with pointer receivers and custom error wrapping (%w).
```

---

## Template 2: Level 2 - Inter-Entity Transactions & Deadlock Prevention

```markdown
Claude, we need to extend the ledger with cross-account atomic transfers and analytics.

### Architectural Decisions:
1. Deadlock Prevention: Since each Account holds its own Mutex, concurrent cross-transfers (A->B and B->A) can trigger ABBA deadlocks.
   - Implement a `lockPair(from, to)` helper that sorts accounts by `AccountID` lexicographically and locks them in deterministic order.
2. Two-Phase Execution: Separate account balance mutations into two phases:
   - Phase 1 (Check): `checkDebit` and `checkCredit` to validate limits without mutating state.
   - Phase 2 (Apply): `applyDebit` and `applyCredit` only after both accounts pass Phase 1.
3. Analytics Read Isolation:
   - In `GetTopSpenders(n int)`, capture snapshots of spending per account under individual locks so that global transfers are not blocked during sorting.
   - Sort using `slices.SortFunc` with primary (TotalOutflow desc) and tie-breaking (AccountID asc) criteria.

### Interfaces & Specifications:
[Paste method signatures and rules]
```

---

## Template 3: Level 3 - Scheduled Payments & State Machine

```markdown
Claude, we are introducing delayed/scheduled payments with a lifecycle state machine.

### Architectural Decisions:
1. Separation of Concerns:
   - Define a `Payment` entity with fields: `id`, `fromID`, `toID`, `amount`, `executeAt`, `status` (`SCHEDULED`, `SUCCESS`, `FAILED`, `CANCELLED`).
   - Store payments in a thread-safe registry with `sync.RWMutex`.
2. Deferred Debit:
   - Scheduling does NOT reserve or lock funds immediately.
   - Execution occurs during `ProcessScheduledPayments(currentTime)`.
3. Deterministic Batch Processing:
   - Filter all payments where `status == SCHEDULED` and `executeAt <= currentTime`.
   - Sort by `executeAt` ASC, then `paymentID` ASC.
   - Execute each payment using the atomic transfer mechanism. If balance is insufficient, transition status to `FAILED`. If successful, transition to `SUCCESS` and update outflow metrics.
4. Idempotent Cancellation:
   - `CancelPayment` is only allowed when status is `SCHEDULED`. Already completed/cancelled payments return `ErrPaymentAlreadyProcessed`.

### Interfaces & Specifications:
[Paste method signatures and rules]
```

---

## Template 4: Level 4 - Rollback, Compensation & Audit Trail

```markdown
Claude, we are adding transaction rollback / undo capabilities to maintain ledger consistency.

### Architectural Decisions:
1. Audit / Event Log:
   - Maintain an append-only transaction history log (`Transaction` record with `txID`, `timestamp`, `type`, `fromID`, `toID`, `amount`, `reversed`).
2. Compensating Transaction:
   - Rolling back a transaction does not erase historical records; it applies an inverse compensating action (e.g., credit `from`, debit `to`) under lock.
   - Validate that the beneficiary (`toID`) still has enough balance to refund the amount before reversing.
3. Idempotency:
   - A transaction cannot be rolled back more than once. Flag as `reversed = true` atomically.
```
