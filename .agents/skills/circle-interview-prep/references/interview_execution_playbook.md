# PRD-Driven Interview Execution Playbook

This playbook outlines time management, psychological pacing, and communication scripts for Circle's PRD-driven AI Implementation round.

---

## 1. Tactical Time Management (The 60-Minute Budget)

| Phase | Time Allocated | Objective & Deliverables |
| :--- | :--- | :--- |
| **Phase 1: PRD Clarification & Classification** | **00:00 - 08:00 (8 min)** | Spot ambiguities, classify business questions vs. technical assumptions, state invariants out loud. |
| **Phase 2: Architecture & Think Out Loud** | **08:00 - 15:00 (7 min)** | Define structs, lock hierarchies, two-phase mutations, and sentinel errors before touching code. |
| **Phase 3: AI Pair Implementation & Verification** | **15:00 - 45:00 (30 min)** | Execute Architecture-First Prompts with AI, review code (Gatekeeper), run `go test -v -race ./...`. |
| **Phase 4: Follow-up & System Evolution** | **45:00 - 60:00 (15 min)** | Discuss distributed scaling, 2PC/Saga, WAL, sharding, and resilience with the interviewer. |

---

## 2. Phase 1: Clarification & Classification Script

When you first receive the PRD, DO NOT start writing code or prompts. Follow this script:

```text
"I've read through the requirements. Before diving into implementation, I want to clarify a few ambiguities and explicitly classify our assumptions:

1. Business Semantics:
   - For transfers, if account A transfers to account B, how should fees or zero-amount transfers be handled?
   - In case of failure (e.g., insufficient balance), is the transaction state immediately marked FAILED or queued?

2. Precision & Range:
   - I assume all amounts are represented as int64 micro-units (e.g. 1 USDC = 10^6 micro-USDC) to avoid floating-point inaccuracies.
   - I'll add overflow protection against math.MaxInt64.

3. Concurrency Guarantees:
   - I assume high concurrent operations across accounts, requiring strict data consistency and zero data races.
   - For account lookups vs. updates, I'll use a two-level locking model.

4. Scope Assumptions:
   - For today's session, I will assume pure in-memory storage, but encapsulate the data access cleanly so persistent storage can be plugged in during the follow-up."
```

---

## 3. Phase 2: Architecture & Think Out Loud Script

Narrate your data models and concurrency design:

```text
"Now I'll define the architectural foundation:
1. Data Models:
   - A Ledger struct containing a map[string]*Account protected by a sync.RWMutex.
   - An Account struct with balance int64 protected by a sync.Mutex.
2. Deadlock Prevention:
   - For multi-account transfers, I will implement a lockPair helper that orders account IDs lexicographically before acquiring locks to eliminate ABBA deadlocks.
3. Mutation Strategy:
   - I will use a Two-Phase approach: validate balances and preconditions on both accounts while holding both locks, then apply mutations atomically."
```

---

## 4. Phase 3: AI Pairing & Recovery Tactics

### The Gatekeeper Review Checklist
When the AI generates code, scan for these 5 common AI defects before running tests:
1. **Did AI acquire locks in arbitrary order?** (Deadlock hazard)
2. **Did AI hold a lock across an I/O operation or sleep?** (Contention hazard)
3. **Did AI copy a struct containing a `sync.Mutex` by value?** (`go vet` failure)
4. **Did AI forget overflow checks or negative amount checks?** (Edge case bug)
5. **Did AI mutate state before checking all conditions?** (Atomicity leak)

### When Tests Fail
1. **Never say to AI: "Fix this error"**: That produces band-aid fixes and spaghetti code.
2. **Diagnose first, then direct**:
   > *"The test failed with a race condition on `account.balance` during concurrent transfers. Refactor `transferInternal` to ensure `lockPair` is called before reading either balance, and verify with `go test -race`."*

---

## 5. Phase 4: Follow-up Preparedness

Expect the interviewer to pivot at minute 45:
- *"How would this change if we have 10 instances of this service?"*
- *"How do we persist this to PostgreSQL or DynamoDB without losing throughput?"*
- *"What if one of the accounts is on an external ledger?"*

Refer to [followup_question_bank.md](./followup_question_bank.md) for Staff-level response frameworks.
