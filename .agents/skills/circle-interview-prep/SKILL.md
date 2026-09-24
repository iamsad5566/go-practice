---
name: circle-interview-prep
description: >-
  Playbook and mental models for AI-assisted technical interviews (e.g. Circle, Fintech, CodeSignal ICF).
  Use this skill when preparing for or executing Senior/Staff SWE coding interviews involving concurrent in-memory ledgers,
  locking design, deadlock prevention, state machines, and architecture-first AI prompt engineering.
---

# Circle & Fintech AI Implementation Interview Playbook

This skill equips an engineer or AI assistant with the architectural models, concurrency patterns, and prompt engineering frameworks needed to pass Senior/Staff Software Engineer technical interviews (especially at Circle, Fintech companies, or CodeSignal progressive multi-level assessments).

---

## 1. Core Mindset: The "Driver vs. Copilot" Model

In an **AI Implementation Interview**:
- **You are the Tech Lead / System Architect (Driver)**: You make all architectural decisions, identify concurrency risks, define state machines, and set interface boundaries before asking the AI to write code.
- **AI is the Junior Coder (Copilot)**: Responsible for rapid boilerplate generation, syntax details, standard algorithms, and unit test generation.
- **You are the Gatekeeper**: You review every generated line for concurrency leaks, deadlocks, and missed edge cases.

### The 3-Minute Rule Before Every Prompt
When a new Level or requirement is given, **never copy-paste the problem directly to AI**. Spend 2–3 minutes writing down:
1. **Affected Data Models**: What structs and fields need to change?
2. **Concurrency & Locking Strategy**: What locks are acquired? In what order? Is there an ABBA deadlock risk?
3. **State Transitions & Edge Cases**: What is the happy path? What can fail? What needs to be atomic?

---

## 2. Senior Prompt Template (The "Architecture-First" Prompt)

Structure your prompt to Claude / Copilot using this 5-part blueprint:

```markdown
[Context & Role]
We are building a concurrent in-memory settlement ledger in Go.

[Architectural Decisions]
- Locking Strategy: [e.g., Use two-level locking; lock accounts in lexicographical order via lockPair to avoid ABBA deadlock].
- Transaction Atomicity: [e.g., Use Two-Phase execution: validate checks first (checkDebit/checkCredit), then apply state updates].
- Data Flow: [e.g., Read snapshots for rankings without holding the collection write lock].

[Interface & Method Contract]
[Provide exact method signature, parameters, and return types]

[Guard Clauses & Explicit Errors]
- [Condition 1] -> Err...
- [Condition 2] -> Err...

[Implementation Guidelines]
- Idiomatic Go (errors.Is, pointer receivers, no global locks).
- Thread-safe and zero data races under `go test -race`.
```

For full copy-ready prompt templates, see [prompt_templates.md](./references/prompt_templates.md).

---

## 3. The 4 Golden Concurrency Patterns in Fintech Ledgers

When dealing with financial transactions in memory, always apply these four patterns:

1. **Two-Level Locking (Hierarchical Locking)**:
   - Level 1: `sync.RWMutex` on the registry/map (read lock for lookups, write lock only when creating/deleting accounts).
   - Level 2: `sync.Mutex` inside each entity (`Account`) to protect balance and history.
2. **Lock Ordering (Deadlock Elimination)**:
   - When acquiring locks on two entities (e.g. Account A and Account B in a transfer), **always sort by ID first**:
     `first, second := x, y; if second.id < first.id { first, second = second, first }`.
3. **Two-Phase Commit (Check-then-Act)**:
   - Hold both locks -> Check all conditions (balance, overflows, limits) -> Apply all changes -> Release locks.
4. **Snapshot Read for Heavy Aggregations**:
   - For analytics/rankings (`GetTopSpenders`), read references under collection RLock, read entity snapshots under individual Mutex, and sort outside of critical sections to maximize write throughput.

Detailed code references and diagrams are in [locking_and_concurrency.md](./references/locking_and_concurrency.md).

---

## 4. State Machines & Payment Lifecycles

In Level 3 & Level 4, questions transition to delayed payments, batch processing, and rollbacks:
- **Statuses**: `SCHEDULED` -> `PROCESSING` -> `SUCCESS` | `FAILED` | `CANCELLED`.
- **Sorting Rules**: Sort by execution timestamp ascending, break ties by ID.
- **Idempotency & Replay Protection**: Track transaction IDs to reject duplicates.
- **Rollback / Compensation**: Implement an undo stack or command log.

See [fintech_state_machines.md](./references/fintech_state_machines.md) for full implementation patterns.

---

## 5. Verification Checklist

Always run these commands in the terminal before declaring a level complete:
```bash
# 1. Run tests with race detector enabled
go test -count=1 -v -race ./...

# 2. Verify compilation and lint
go vet ./...
```
