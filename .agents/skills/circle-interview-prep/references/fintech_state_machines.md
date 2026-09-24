# Fintech State Machines & Lifecycle Management

Financial systems rarely execute everything synchronously. Handling scheduled transfers, settlement delays, and compensations requires robust state machine design.

---

## 1. Typical Payment Lifecycle State Machine

```text
                  +-------------------+
                  |     SCHEDULED     |
                  +---------+---------+
                            |
           +----------------+----------------+
           |                                 |
 [Time reached & Process]             [User Cancel]
           |                                 |
           v                                 v
+--------------------+             +-------------------+
|  Check Balance     |             |     CANCELLED     |
+----+----------+----+             +-------------------+
     |          |
 (Sufficient) (Insufficient)
     |          |
     v          v
+---------+ +--------+
| SUCCESS | | FAILED |
+---------+ +--------+
```

---

## 2. In-Memory State Model Implementation

```go
type PaymentStatus string

const (
    StatusScheduled PaymentStatus = "SCHEDULED"
    StatusSuccess   PaymentStatus = "SUCCESS"
    StatusFailed    PaymentStatus = "FAILED"
    StatusCancelled PaymentStatus = "CANCELLED"
)

type Payment struct {
    ID        string
    FromID    string
    ToID      string
    Amount    int64
    ExecuteAt int64
    Status    PaymentStatus
    ErrorMsg  string
}
```

---

## 3. Deterministic Batch Processing

In distributed ledger simulations, tie-breaking must be **100% deterministic**:

```go
func (b *BankLedger) ProcessScheduledPayments(currentTime int64) []PaymentResult {
    // 1. Gather eligible payments under read lock
    b.paymentsMu.RLock()
    var eligible []*Payment
    for _, p := range b.payments {
        if p.Status == StatusScheduled && p.ExecuteAt <= currentTime {
            eligible = append(eligible, p)
        }
    }
    b.paymentsMu.RUnlock()

    // 2. Sort deterministically: ExecuteAt ASC, then PaymentID ASC
    slices.SortFunc(eligible, func(a, b *Payment) int {
        if c := cmp.Compare(a.ExecuteAt, b.ExecuteAt); c != 0 {
            return c
        }
        return cmp.Compare(a.ID, b.ID)
    })

    // 3. Process sequentially
    results := make([]PaymentResult, 0, len(eligible))
    for _, p := range eligible {
        res := b.executeSinglePayment(p)
        results = append(results, res)
    }
    return results
}
```

---

## 4. Idempotency & Rollback Patterns

### Idempotency Key:
Store processed keys in a `map[string]struct{}`. If a request arrives with an existing key, reject or return cached result.

### Compensating Transactions (Rollback):
Do not mutate original transaction rows. Record a new `RollbackTransaction` and invert the credit/debit balances, verifying that the target account has enough balance to refund.
