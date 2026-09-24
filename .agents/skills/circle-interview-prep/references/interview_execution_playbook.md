# 90-Minute Interview Execution Playbook

This playbook outlines time management, psychological pacing, and recovery tactics for CodeSignal progressive multi-level assessments.

---

## 1. Tactical Time Management (The 90-Minute Budget)

| Phase | Time Allocated | Objective |
| :--- | :--- | :--- |
| **Level 1** | **12 - 15 min** | Scaffolding, core domain structs, initial CRUD, thread-safe basics. |
| **Level 2** | **18 - 20 min** | Cross-resource interactions, analytics/rankings, deadlock prevention. |
| **Level 3** | **25 - 30 min** | The "pivot" level: state machines, time-based operations, refactoring. |
| **Level 4** | **15 - 20 min** | Resilience, rollback/compensation, backward-compatible integration. |
| **Buffer** | **5 - 10 min** | Full test suite verification with `-race`, clean-up, Q&A with interviewer. |

> **Golden Rule**: Submit and run tests early and often. CodeSignal awards partial credit. Never accumulate uncommitted code across multiple levels.

---

## 2. Pacing by Level

### Level 1: Laying the Foundations
- **Do not overcomplicate, but do not cut corners.**
- Define clean interfaces and domain structs immediately.
- Use granular locks from the start so you don't have to perform a massive rewrite in Level 2.
- Define custom sentinel errors (`var Err... = errors.New(...)`).

### Level 2: Inter-Resource Operations
- Look out for multi-entity locks. Always ask yourself: *"Can operation X on entity A and operation Y on entity B run concurrently?"*
- If yes, implement deterministic lock ordering immediately.
- For analytics/aggregations, ensure they do not block core transaction paths.

### Level 3: The Architecture Pivot
- Level 3 almost always introduces a new lifecycle, temporal dimension (timestamps/scheduling), or complex status changes.
- **Do not panic if you need to refactor.** State to the interviewer:
  > *"To support this new lifecycle without breaking Level 1 and 2 tests, I am going to extract a dedicated state model while preserving the existing public interface."*
- Ensure all Level 1 and Level 2 tests still pass before moving on.

### Level 4: Edge Cases & Compensation
- Usually asks for undo/rollback, entity merging, or batch reconciliation.
- Focus on backward compatibility: old methods must still function seamlessly.
- Check edge cases: zero amounts, duplicate IDs, idempotency keys, non-existent entities.

---

## 3. Recovery Tactics: What to Do When a Test Fails

1. **Don't blindly ask AI to "fix the error"**:
   - Passing a stack trace to AI without context often causes AI to patch symptoms rather than root causes, introducing spaghetti code.
2. **Isolate the Failure**:
   - Read the exact assertion failure and input arguments.
   - Trace the lock acquisition path or state transition in your mind.
3. **Give AI a Surgical Directive**:
   - Instead of *"Fix this failing test"*, prompt:
     > *"TestLevel2_Concurrency failed because `lockPair` does not release the locks in reverse order under early return. Please refactor `lockPair` to use a `defer` cleanup pattern while preserving the same signature."*
