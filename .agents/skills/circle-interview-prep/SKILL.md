---
name: circle-interview-prep
description: >-
  Comprehensive guide and evaluation rubric for Circle Senior/Staff SWE Technical Interviews (e.g. AI Implementation Interview, CodeSignal ICF).
  Use this skill to understand what Circle interviewers evaluate, how to drive AI as a Tech Lead, master Go concurrency and data integrity patterns,
  and execute high-stakes live coding interviews across any problem domain.
---

# Circle Senior/Staff SWE Interview Playbook

This skill is a complete guide to succeeding in Circle's **AI Implementation Interview** and other technical rounds. It details the exact rubric Circle interviewers use, how to maintain architectural control when pairing with AI, and the universal engineering standards expected of a Senior or Staff Software Engineer.

---

## 1. What Circle Interviewers Are Actually Evaluating

In an AI-enabled interview, the interviewer is **not** testing whether you can memorize Go syntax or write basic algorithms from scratch. They are assessing whether you can function as an **autonomous, high-judgment Tech Lead**.

### The 5 Evaluation Dimensions

| Dimension | What Interviewers Look For | Red Flags (Down-level / Reject) |
| :--- | :--- | :--- |
| **1. Architectural Sovereignty** | You define data structures, boundaries, and invariants *before* writing code. You tell the AI what to build, not ask it what to do. | Copy-pasting the raw problem description directly into the AI and letting it dictate the architecture. |
| **2. Concurrency & Data Integrity** | Deep instinct for race conditions, deadlock elimination, atomicity, and lock granularity. Knowing financial states cannot tolerate data races. | Using a single global lock, ignoring ABBA deadlocks on multi-resource operations, or mutating state without locks. |
| **3. AI Collaboration & Prompt Quality** | High-signal, constraint-driven prompts. Treating AI as a junior pair-programmer. | Vague prompts, repetitive trial-and-error prompting, or accepting code without reading it. |
| **4. Defensive Engineering** | Guard clauses, typed errors (`errors.Is`), numeric overflow checks, idempotent operations, and zero data leakage. | Missing validation, silent failures, panic risks, or ignoring edge cases like negative numbers and zero values. |
| **5. Communication & Think Aloud** | Narrating decisions: *"I am setting a strict lock ordering here to prevent circular wait, then I'll have AI implement the helper."* | Silent coding, staring at AI outputs without explaining what you are reviewing or why a test failed. |

---

## 2. The 4 Progressive Levels: What the Interviewer Tests at Each Stage

Circle's technical tasks (CodeSignal Industry Coding Framework) evolve through 4 distinct levels. Regardless of the domain (Ledger, Key-Value Store, File System, or Token Router), each level has a specific grading purpose:

```text
Level 1: Foundation & Data Modeling (15 min)
   └─ Goal: Clean abstractions, proper encapsulation, basic CRUD, thread-safe scaffolding.

Level 2: Aggregation & Multi-Resource Interaction (20 min)
   └─ Goal: Cross-entity operations, deadlock prevention, non-blocking analytical reads.

Level 3: Complex State & Requirement Shift (30 min)
   └─ Goal: Introducing state machines, delayed/scheduled operations, refactoring without breaking tests.

Level 4: Resilience, Backward Compatibility & Edge Cases (20 min)
   └─ Goal: Compensation/Rollback, account merges, idempotency, seamless backward compatibility.
```

---

## 3. The "Driver-Copilot" Operating Model

When interacting with the AI during the interview, follow the **3-Minute Architecture Rule**:

```text
[New Requirement Arrives]
        │
        ▼
[2-3 Min: Architect Alone] ──> Define structs, lock boundaries, error types, state transitions
        │
        ▼
[Prompt: Architecture-First] ──> Send constraints + decisions + interfaces to AI
        │
        ▼
[Gatekeeper Review] ──> Audit AI code for concurrency bugs, value copying, and edge cases
        │
        ▼
[Verification] ──> Run tests with `go test -count=1 -v -race ./...`
```

---

## 4. Universal Concurrency & Design Principles (Fintech Standards)

No matter what problem Circle presents, the following four rules always apply:

1. **Hierarchical Locking**: Never protect an entire registry and individual records with one big lock. Use read-write locks for lookup registries and granular locks for entities.
2. **Deterministic Lock Ordering**: When any operation requires locking two or more entities simultaneously, **always sort resources by a consistent key (e.g. ID)** before acquiring locks to guarantee zero deadlocks.
3. **Two-Phase Mutation (Check-then-Act)**: When coordinating state changes across multiple resources, check all preconditions across all involved entities before committing mutations to any single entity.
4. **Isolated Snapshot Reads**: Analytics and reporting queries must never hold write locks on active transaction paths. Take snapshots under short read locks and perform sorting or transformations outside the lock.

Detailed implementation patterns and reference code are documented in [concurrency_and_data_integrity.md](./references/concurrency_and_data_integrity.md).

---

## 5. Reference Documentation in this Skill

- [concurrency_and_data_integrity.md](./references/concurrency_and_data_integrity.md): Universal concurrency patterns, deadlock elimination, and Go runtime pitfalls.
- [ai_interaction_framework.md](./references/ai_interaction_framework.md): The Architecture-First Prompting framework, prompt checklists, and common AI blindspots to watch for.
- [interview_execution_playbook.md](./references/interview_execution_playbook.md): Time management, Think Aloud scripts, and handling live test failures under pressure.
