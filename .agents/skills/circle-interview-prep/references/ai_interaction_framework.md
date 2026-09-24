# The AI Interaction Framework (Driving AI as a Tech Lead)

In an AI Implementation Interview, the chat history between you and the AI is visible to the interviewer and forms a primary evaluation signal. This guide defines how to construct high-signal prompts and how to critically review AI-generated code.

---

## 1. The Architecture-First Prompt Formula

Never send a raw problem statement to the AI. Always wrap requirements in the **5-Part Architecture-First Formula**:

```markdown
### 1. Goal & Context
Brief statement of the component or method being added and how it fits into the existing system.

### 2. Architectural Decisions & Constraints (The "Lead" Section)
- **Concurrency & Locking**: Explicitly state the locking hierarchy, which locks to acquire, and the ordering strategy to eliminate deadlocks.
- **Atomicity**: Specify whether check-then-act / two-phase execution is needed.
- **Data Encapsulation**: Struct unexported fields, pointer receivers, no global mutable state.

### 3. Interface & Type Signatures
Define the exact Go interfaces, struct signatures, and custom errors expected.

### 4. Guard Clauses & Invariants
List the exact validation steps to perform at function entry (invalid inputs, non-positive amounts, missing entities, state conflicts).

### 5. Implementation Rules
- Wrap errors with `%w` for `errors.Is` compatibility.
- Ensure thread-safety verifiable by `go test -race`.
- Do not modify existing working interfaces unless backward compatibility is explicitly preserved.
```

---

## 2. Common AI Blindspots (The Gatekeeper Checklist)

When Claude or any AI produces code, do **not** run it immediately. Spend 30 seconds scanning for these frequent AI hallucinations and omissions:

### Check 1: Missing Lock Acquisition / Inconsistent Locks
- Did AI read a map or struct field directly without grabbing the lock?
- Did AI acquire a lock on `Account A` but forget to lock `Account B`?

### Check 2: Premature State Mutation (Atomicity Breach)
- Did AI decrement a balance or change a status before verifying all error conditions? If step 2 fails, is step 1 left in a corrupted state?

### Check 3: Mutex Passed by Value
- Look at function signatures. Did AI define `func (a Account) Method()` instead of `func (a *Account) Method()`? If so, the mutex is copied and disabled.

### Check 4: Data Race via Shared References
- Did AI return a pointer or slice pointing directly to internal data? (e.g. `return a.history` instead of creating a fresh copy).

### Check 5: Goroutine / Timer Leaks
- If AI used `time.AfterFunc` or `go func()`, is there a way to cancel it if the entity is deleted or updated?

---

## 3. Think Aloud Scripts During AI Pairing

Use these phrases to actively demonstrate your Senior engineering judgment to the interviewer while interacting with the AI:

- **Before Prompting**:
  > *"Before I ask the AI to implement this, let's identify the core concurrency risk. Since we are dealing with multi-resource updates, we have an ABBA deadlock hazard. I am going to instruct the AI to use a deterministic lock ordering based on IDs."*

- **While Reviewing AI Output**:
  > *"Looking at the AI's code, the core logic is clean, but notice it directly returned an internal slice. In Go, that leaks mutable state to external callers. I'm going to fix that by doing a defensive copy."*

- **When a Test Fails**:
  > *"The race detector flagged a data race on line 45. The AI accessed the map under an RLock while a background worker held a Lock. Let me adjust the lock boundary rather than letting the AI guess blindly."*
