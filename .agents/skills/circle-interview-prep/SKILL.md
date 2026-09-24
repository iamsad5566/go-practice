---
name: circle-interview-prep
description: >-
  Comprehensive guide and evaluation rubric for Circle Senior/Staff SWE Technical Interviews (PRD-driven AI Implementation Round).
  Use this skill to master the PRD clarification & classification workflow, drive AI as a Tech Lead, excel at Go concurrency and data integrity,
  and ace deep architectural follow-ups.
---

# Circle Senior/Staff SWE Interview Playbook (PRD & AI Pair Implementation)

This skill guides preparation and execution for Circle's **AI Implementation Technical Interview**. 

In this round, candidates receive a realistic **PRD / Technical Specification**, clarify ambiguities, classify assumptions, collaborate with an AI assistant (Claude/Copilot) as a Tech Lead, and defend their architecture during an interactive **Follow-up** discussion.

---

## 1. Core Evaluation Rubric (What Circle Evaluates)

Circle evaluates Senior and Staff candidates across five core dimensions:

| Dimension | What Interviewers Look For | Strong Senior/Staff Signal | Red Flags (Down-level / Reject) |
| :--- | :--- | :--- | :--- |
| **1. Clarification & Classification** | Proactively identifying ambiguities in the PRD, classifying which require product alignment vs. reasonable engineering assumptions. | Surfaces hidden edge cases, defines explicit assumptions out loud, clarifies consistency and precision invariants. | Silently guessing requirements; passing the raw PRD straight into AI without clarification. |
| **2. Architectural Sovereignty (Driver)** | Deciding data models, lock hierarchy, state machines, and error contracts before touching code. | Defines struct fields, locking order (ABBA prevention), and two-phase mutations *before* prompting AI. | Letting AI design the architecture; accepting AI hallucinations or anti-patterns blindly. |
| **3. Concurrency & Data Integrity** | Deep instinct for race conditions, deadlock elimination, lock contention, numeric precision, and overflow checks. | Two-level locking, deterministic ID-ordered lock acquisition, atomic check-then-act, zero races under `go test -race`. | Global locks on all methods, locking out-of-order, floating-point money, ignoring integer overflow. |
| **4. High-Signal AI Collaboration** | Constraint-driven, architecture-first prompts. Treating AI as an implementation copilot for boilerplate, algorithms, and tests. | Writes precise specs, struct definitions, and error constraints; directs AI surgically to fix failures. | Vague prompts ("implement this PRD"), repetitive trial-and-error prompting, lack of gatekeeping. |
| **5. Communication & Follow-up Depth** | Continuous "Think Out Loud"; crisp justification of trade-offs (KISS vs. extensibility); deep answers to distributed systems follow-ups. | Articulates trade-offs clearly; demonstrates deep knowledge when evolving in-memory state to distributed infra (Saga, 2PC, WAL). | Coding in silence; panicking when tests fail; unable to explain how in-memory design maps to distributed infra. |

---

## 2. The 4-Phase Interview Workflow (60 Minutes)

```text
[00:00 - 08:00] Phase 1: PRD Analysis, Clarification & Classification
    └─ Read PRD, spot ambiguities.
    └─ Classify: (A) Business rules needing interviewer confirmation vs. (B) Technical assumptions stated out loud.

[08:00 - 15:00] Phase 2: Architecture & Think Out Loud
    └─ Define structs, state transitions, locking hierarchy, guard clauses, and error types out loud.

[15:00 - 45:00] Phase 3: AI-Assisted Implementation & Verification
    └─ Prompt AI with strict architectural boundaries (Architecture-First Template).
    └─ Review code line-by-line (Gatekeeper pattern: race conditions, deadlock, leak checks).
    └─ Execute tests with `go test -v -race ./...`.

[45:00 - 60:00] Phase 4: Interviewer Follow-up & System Evolution
    └─ Deep dive into distributed scaling, sharding, persistence, idempotency, or disaster recovery.
```

---

## 3. The Clarification & Classification Framework

Before writing any code or prompts, systematically classify ambiguities using these 4 buckets:

1. **Business Semantics & State Lifecycle**:
   - Fees & Deductions: Is the fee deducted from the transfer amount or billed separately?
   - Failure Modes: If a transfer fails mid-flight, is it marked FAILED immediately or queued for retry?
   - Negative Balances: Are credit lines / overdrafts ever permitted, or strictly non-negative?
2. **Numeric Precision & Range Invariants**:
   - Currency Units: Are amounts strictly `int64` micro-units (e.g. 1 USDC = 1,000,000 micro-USDC)? No floating points (`float64`).
   - Bounds & Overflows: Must we guard against `amount + balance > math.MaxInt64`?
3. **Concurrency & Consistency Guarantees**:
   - Ordering: Are concurrent requests for the same account strictly FIFO, or prioritized by timestamp?
   - Idempotency: Does the PRD require deduplication via `Idempotency-Key` or transaction ID?
4. **Scope & Architectural Assumptions (State Clearly to Interviewer)**:
   - *"For today's live session, I will assume an in-memory storage model without external persistence, but I will design clean boundaries so a storage adapter can be plugged in later."*
   - *"I will assume accounts cannot be deleted once created to preserve ledger auditability."*

---

## 4. The 4 Universal Concurrency & Design Principles

All Circle backend systems deal with financial or mission-critical state. The following four invariants must be respected:

1. **Two-Level Locking (Hierarchical Locking)**:
   - Registry Level: `sync.RWMutex` (Read-lock for lookups; Write-lock strictly for creation/deletion).
   - Entity Level: `sync.Mutex` (Protects entity balances and internal state).
2. **Deterministic Lock Ordering (ABBA Deadlock Elimination)**:
   - When acquiring locks across multiple entities (e.g. transfers, merges), **always sort entities by a deterministic key (e.g. AccountID)** before acquiring locks.
3. **Two-Phase Commit (Check-then-Act)**:
   - Acquire all required locks -> Check all invariants (balance, overflow, limits) -> Apply state mutations -> Release locks.
4. **Snapshot Reads for Analytics/Aggregations**:
   - For queries like `GetTopSpenders` or ledger audits, read snapshots under short read-locks and perform sorting/filtering outside critical sections to minimize write-lock contention.

---

## 5. Reference Documentation

- [interview_execution_playbook.md](./references/interview_execution_playbook.md): Time management, Think Aloud scripts, and handling live test failures.
- [ai_interaction_framework.md](./references/ai_interaction_framework.md): The Architecture-First Prompting blueprint and prompt templates.
- [concurrency_and_data_integrity.md](./references/concurrency_and_data_integrity.md): Universal Go concurrency blueprints, memory models, and race prevention.
- [followup_question_bank.md](./references/followup_question_bank.md): High-probability follow-up questions (distributed transactions, persistence, sharding, consensus) and Staff-level answers.
