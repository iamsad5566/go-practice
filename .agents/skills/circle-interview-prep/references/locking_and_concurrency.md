# Locking & Concurrency Design for In-Memory Ledgers

Financial ledgers require strict guarantees: **Atomicity, Consistency, High Throughput, and Zero Deadlocks**.

---

## 1. The Global Lock Anti-Pattern
```go
// ❌ WRONG: Single big lock kills throughput
type BankLedger struct {
    mu       sync.Mutex
    accounts map[string]*Account
}
```
*Why it fails Senior interviews*: Any operation on Alice blocks Bob depositing into Charlie's account.

---

## 2. Two-Level Locking (Production Standard)

```go
// Registry Level: Only guards map topology
type BankLedger struct {
    mu       sync.RWMutex
    accounts map[string]*Account
}

// Entity Level: Guards individual account state
type Account struct {
    mu           sync.Mutex
    id           string
    balance      int64
    totalOutflow int64
}
```

### Access Flow:
1. **Find account**: Acquire `b.mu.RLock()`, retrieve `*Account`, immediately call `b.mu.RUnlock()`.
2. **Mutate balance**: Acquire `acc.mu.Lock()`, perform operation, call `acc.mu.Unlock()`.

---

## 3. The ABBA Deadlock Problem & Lock Ordering Solution

### The Scenario:
- Goroutine 1: Transfer from Alice to Bob (Locks Alice, then waits for Bob).
- Goroutine 2: Transfer from Bob to Alice (Locks Bob, then waits for Alice).
- **Result**: Permanent deadlock, test hangs or crashes.

### The Solution: Strict Lexicographical Lock Ordering
Always acquire locks in a globally deterministic order (e.g. by sorting IDs).

```go
func lockPair(x, y *Account) (unlock func()) {
    first, second := x, y
    // Always lock the smaller ID first
    if second.id < first.id {
        first, second = second, first
    }
    first.mu.Lock()
    second.mu.Lock()
    return func() {
        second.mu.Unlock()
        first.mu.Unlock()
    }
}
```

---

## 4. Two-Phase Execution Pattern (Atomic Transfers)

Never debit Account A before verifying Account B can be credited.

```go
func (b *BankLedger) Transfer(fromID, toID string, amount int64) (int64, int64, error) {
    from, to, err := b.getAccountPair(fromID, toID)
    if err != nil {
        return 0, 0, err
    }

    unlock := lockPair(from, to)
    defer unlock()

    // Phase 1: Validate BOTH accounts under lock (Check)
    if err := from.checkDebit(amount); err != nil {
        return 0, 0, err
    }
    if err := to.checkCredit(amount); err != nil {
        return 0, 0, err
    }

    // Phase 2: Apply state mutations (Act)
    from.applyDebit(amount)
    to.applyCredit(amount)

    return from.balance, to.balance, nil
}
```

---

## 5. Non-Blocking Read Snapshots for Analytics

When computing rankings or reports (e.g., `GetTopSpenders`), do not hold a global lock while sorting:

```go
func (b *BankLedger) GetTopSpenders(n int) []AccountSpending {
    if n <= 0 {
        return []AccountSpending{}
    }

    // 1. Shallow copy account pointers under registry read lock
    b.mu.RLock()
    accounts := make([]*Account, 0, len(b.accounts))
    for _, acc := range b.accounts {
        accounts = append(accounts, acc)
    }
    b.mu.RUnlock()

    // 2. Read each account's value snapshot under its individual lock
    spenders := make([]AccountSpending, 0, len(accounts))
    for _, acc := range accounts {
        spenders = append(spenders, acc.spendingSnapshot())
    }

    // 3. Sort outside of any locks!
    slices.SortFunc(spenders, func(x, y AccountSpending) int {
        if c := cmp.Compare(y.TotalOutflow, x.TotalOutflow); c != 0 {
            return c
        }
        return cmp.Compare(x.AccountID, y.AccountID)
    })

    return spenders[:min(n, len(spenders))]
}
```
