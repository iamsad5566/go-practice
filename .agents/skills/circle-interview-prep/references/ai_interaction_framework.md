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

## 3. Think Aloud Scripts During AI Pairing (中文思考外顯話術)

在指揮 AI 協作的過程中，向面試官用中文口述你的工程判斷與把關思考，展現 Tech Lead 的獨立決策力：

- **下 Prompt 前 (說明架構約束，展現防禦意識)**：
  > 「在請 AI 生成轉帳方法前，我們先識別並發關鍵風險：因為涉及多帳戶餘額更新，存在經典的 ABBA 死鎖風險。我不會直接讓 AI 自由發揮，而是給出嚴格約束，要求它必須先調用我們定義的 `lockPair`（按 AccountID 排序加鎖），並遵循 Check-then-Act 兩階段變更。」

- **審查 AI 代碼時 (Gatekeeper 逐行審查，抓出隱蔽 Bug)**：
  > 「檢視 AI 剛產生的代碼，核心邏輯看起來很流暢，但注意看第 38 行：它直接把內部的 transaction slice 回傳了。在 Go 語言中，這會把內部可變狀態暴露給外部調用方，造成潛在的 Data Race。我現在手動（或請 AI）加上防禦性複製（Defensive Copy）。」

- **測試報錯或 `-race` 警告時 (冷靜診斷，拒絕盲試)**：
  > 「測試在高並發場景下報了 Data Race。這不是隨機問題，是因為 AI 在查詢狀態時偷懶沒拿讀鎖。我現在直接指定修復位置與加鎖邊界，而不是讓 AI 瞎猜亂改。」

