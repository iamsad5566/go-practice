# Concurrency & Data Integrity Guide (Go & Fintech Standards)

Financial engineering requires absolute guarantees against data corruption, lost updates, race conditions, and deadlocks. This guide outlines the universal patterns expected in Circle Senior/Staff interviews.

---

## 1. Locking Hierarchies: Collection vs. Entity

### Anti-Pattern: Single Global Lock
A single `sync.Mutex` on the root engine creates a bottleneck where every read and write blocks every other user.

### Production Pattern: Two-Level Locking
- **Level 1 (Registry / Map Level)**: Protected by `sync.RWMutex`.
  - Hold `RLock` when looking up entity pointers. Release as soon as the pointer is retrieved.
  - Hold `Lock` ONLY when adding, removing, or re-indexing entities in the collection.
- **Level 2 (Entity / Item Level)**: Protected by individual `sync.Mutex` (or `sync.RWMutex`).
  - Protects the entity's mutable fields (balances, state, timestamps, counters).
  - Operations on Entity A run 100% in parallel with operations on Entity B.

---

## 2. Deadlock Elimination: Strict Lock Ordering

### The Root Cause (Circular Wait / ABBA Deadlock)
Whenever an operation requires locking two entities simultaneously (e.g. Transfer, Resource Swap, Merging):
- Worker 1: Locks Resource A, attempts to lock Resource B.
- Worker 2: Locks Resource B, attempts to lock Resource A.
- Both goroutines sleep forever.

### The Universal Defense: Total Order Locking
Always enforce a deterministic, global ordering on resources before acquiring their locks.

```go
// Generic lockPair pattern for any two resources
func lockPair[T interface{ ID() string }](a, b *T, lockFunc func(*T)) (unlock func()) {
    first, second := a, b
    // Enforce strict order using a unique, immutable property (e.g. ID)
    if (*second).ID() < (*first).ID() {
        first, second = second, first
    }
    
    // Acquire in deterministic order
    lock(first)
    lock(second)

    // Unlock in reverse order
    return func() {
        unlock(second)
        unlock(first)
    }
}
```

---

## 3. Two-Phase State Mutation (Check-then-Act)

When mutating multiple related states:
1. **Phase 1: Verification (Under Locks)**
   - Check all preconditions across all entities (e.g. balances, limits, state validity, overflow checks).
   - If ANY check fails, abort immediately without altering any state.
2. **Phase 2: Commit (Under Locks)**
   - Apply mutations to all entities.
   - Emit audit/history records if required.
   - Release locks.

---

## 4. Non-Blocking Analytics & Snapshot Isolation

### The Problem
Computing aggregations (e.g., top rankings, total system reserves, filtered scans) takes $O(N \log N)$ or $O(N)$ time. Holding locks during this computation halts write traffic.

### The Solution: Copy-on-Read / Snapshot Pattern
1. Under a brief registry read lock (`RLock`), collect shallow references to all active entities.
2. For each entity, acquire its individual lock briefly to extract a read-only snapshot struct, then immediately release the lock.
3. Perform all sorting, filtering, and aggregation on the snapshots **completely outside of any critical section**.

---

## 5. Critical Go Concurrency Gotchas

Circle interviewers watch closely for these common Go-specific concurrency errors:

| Gotcha | Why It Fails | Correct Approach |
| :--- | :--- | :--- |
| **Copying a Mutex by Value** | Passing a struct containing `sync.Mutex` by value creates a new lock copy, rendering synchronization useless and triggering `go vet` warnings. | Always pass structs with locks by pointer (`*Entity`), and use pointer receivers (`func (e *Entity) Method()`). |
| **Slice / Map Aliasing** | Returning an internal slice directly from a thread-safe method allows external callers to read or modify it without locks, causing data races. | Return a copy of the slice (`copy(out, original)`). |
| **Goroutine Leaks** | Launching background workers or timers without a termination signal (`context.Context` or `chan struct{}`). | Always pair goroutines with a cancellation mechanism or clean shutdown lifecycle. |
| **Map Concurrent Read/Write Panic** | Go maps panic natively on simultaneous read/write. | Ensure all map access is guarded by an `RWMutex` or `sync.Map`. |
