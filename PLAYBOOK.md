# 📖 Circle 90 分鐘實戰戰術手冊：PRD 拆解、架構思維與 AI 指揮指南 (Playbook)

本手冊是專為 **Circle Senior SWE 90 分鐘 AI 協同實作輪 (PRD & AI Pair Implementation)** 設計的**純方法論戰術指南**。
無論面試抽到任何題目，只要依循本手冊的**三步拆解法**與**三階梯 AI 指揮法**，就能展現強大的 Tech Lead 主導姿態，穩拿 Senior Pass！

---

## ⏱️ 90 分鐘節奏控制全景圖 (The 90-Minute Rhythm)

```text
[00:00 - 10:00]  Phase 1: PRD 快速拆解、邊界提問與主動假設 (Clarification & Invariants)
[10:00 - 20:00]  Phase 2: 狀態機構思、資料結構與鎖分級設計 (Mental Model & Architecture)
[20:00 - 65:00]  Phase 3: 三階梯 AI 指揮實作、Gatekeeper 審查與 -race 驗證 (AI Pairing)
[65:00 - 90:00]  Phase 4: 面試官分散式追問、KISS 兩段式回答 (Distributed Follow-ups)
```

---

## 第一章：看到 PRD 怎麼拆解？(PRD Deconstruction in 5 Mins)

當面試官把全英文 PRD 丟給你時，**切忌從頭到尾死讀每一行字**。你的大腦只需執行「三維掃描」與「四象限分類」：

### 1. 眼睛看哪裡？三維度快速掃描 (The 3-Dimensional Scan)

```text
PRD 快速掃描
 ├── 1. 數值與型態維度 (Types)     ──► 找 Amount, Time, ID (是否涉及金額？時間是否在過去？)
 ├── 2. 狀態與生命週期維度 (State)  ──► 找 Status 變化 (有無中間態？取消/失敗在何時允許？)
 └── 3. 並行與資源維度 (Concurrency) ──► 找 Worker, Timer, Rate (有沒有定時器？多 Worker 爭搶？)
```

### 2. 四象限分類法 (將模糊點快速歸類)
在 PRD 中抓出 2~3 個點，快速歸入以下兩個籃子：
* **籃子 A：必須向面試官對齊的業務問題 (Business Clarification)**
  * *例如*：「取消操作是在任務被 Worker 撈出前都有效嗎？」
  * *例如*：「手續費是收款方少收（內扣），還是扣款方多扣（外加）？」
* **籃子 B：自己主動宣布的工程技術假設 (Engineering Assumptions)**
  * *例如*：「在金融場景中，為防止浮點數精度損失，我一律假設金額以微單位 (`int64`) 儲存，1 USDC = 1,000,000 micro-units。」
  * *例如*：「為避免建立數萬個定時器造成 Timer 洩漏，我假設內部採用 Min-Heap 配合單一動態重設的 `time.Timer`。」

### 3. 開場 5 分鐘「外顯思考 (Think Out Loud)」對白範本
看完 PRD 後，直接看著面試官說出這段話，立威 Tech Lead 姿態：
> *「這份規格很清楚。在動工前，我想先花兩分鐘對齊兩個核心邊界：*
> 1. *【業務對齊】：關於取消（Cancel），我假設在款項真正送出外部通道前都可以取消；一旦進到派發中（Dispatched），取消則回傳失敗。這個行為符合業務預期嗎？*
> 2. *【工程假設】：在精度與資源方面，我主動假設金額一律以 `int64` 微單位計價防範精度損失；另外為了避免百萬排程下的 Timer 洩漏，系統內部採用單一計時器配合 Min-Heap 來管理。*
> *如果這兩個方向沒問題，我接下來為你展示我的資料模型與狀態機設計。」*

---

## 第二章：動筆前怎麼思考？(Senior Architecture Mental Model)

在讓 AI 寫任何代碼前，你必須在紙上或口頭定義好三個核心骨架：**狀態機**、**Struct 欄位**、**鎖邊界**。

### 1. 狀態機優先 (State Machine First)
任何非同步或金流系統，最危險的 Bug 都在狀態流轉。先寫出狀態枚舉，**永遠記得加「中間態」**：
```text
【排程系統】: SCHEDULED ──► DISPATCHED (中間態，防止取消競爭) ──► COMPLETED / FAILED
【發件系統】: PENDING   ──► IN_FLIGHT  (中間態，防止重複派發) ──► COMPLETED / RETRYING / DEAD_LETTER
```

### 2. 鎖分級與並行四不變量 (Lock Hierarchy & Invariants)
牢記這四大鐵律，面試時直接講出來：
1. **雙層鎖隔離**：
   - 外層 `sync.RWMutex`：只保護全域 Registry/Map 查找（讀多寫少，極快釋放）。
   - 內層 `sync.Mutex`：掛在單筆 Record 上，只保護該筆記錄的狀態流轉。
2. **字典序排序鎖 (根除 ABBA 死鎖)**：
   - 只要操作跨兩個資源（如 A 轉帳給 B），**一律先比對 ID 大小 (`idA < idB`) 依序加鎖**。
3. **兩階段變更 (Check-then-Act)**：
   - 獲取所有相關鎖 $\rightarrow$ 驗證所有前置條件（餘額、狀態合法性） $\rightarrow$ 原子修改 $\rightarrow$ 釋放鎖。
4. **🔴 最嚴格的紅線：絕不在持有鎖時做網路 I/O 或 Sleep**：
   - 鎖內只做記憶體狀態變更（微秒級），變更為中間態後立刻釋放鎖，外部 HTTP / API 呼叫全程由背景 Worker 在無鎖環境下執行！

---

## 第三章：如何像 Tech Lead 一樣指揮 AI？(Directing AI in 3 Steps)

切忌直接把 800 行 Prompt 丟給 AI 一次生成。採用 **三階梯實作法 (Incremental 3-Step Ladder)**：

```text
[Step 1：定義契約] ──► [Step 2：核心並發邏輯] ──► [Step 3：測試與 Race 驗證]
  (約 3~5 分鐘)           (約 15~20 分鐘)              (約 10~15 分鐘)
```

---

### 階梯一：契約與資料模型先行 (Stage 1: Contracts & Models)

#### 🎙️ 你對面試官說：
> *「為了保證代碼結構模組化且好維護，我們採用階梯式實作。第一步我們先請 AI 定義 `model.go`，包含核心 Struct、狀態枚舉與外部調用 Interface。我們先肉眼確認資料契約無誤。」*

#### 📋 給 AI 的 Prompt 模板：
```markdown
Please implement `internal/<pkg>/model.go` in Go 1.22+.
- Define Status constants: (e.g. Scheduled, Dispatched, Completed, Cancelled, Failed).
- Define the Request struct and Record struct.
- IMPORTANT: Put a `sync.Mutex` inside the Record struct to protect its individual state transitions. Provide a `.Snapshot()` method returning a copy.
- Define the external Executor/Sender interface: `Execute(ctx, req) (Result, error)` or `Send(ctx, ...)`.
- Define standard sentinel errors (ErrNotFound, ErrAlreadyDispatched, etc.).
DO NOT implement the engine loop or background workers yet. Just the models and interfaces.
```

#### 🔍 你的 30 秒審查清單 (Gatekeeper 30s Check)：
- [ ] Record 上有沒有帶 `sync.Mutex`？
- [ ] 狀態枚舉有沒有漏掉中間態（`DISPATCHED` / `IN_FLIGHT`）？
- [ ] 外部 Interface 有沒有傳入 `context.Context`？

---

### 階梯二：核心引擎與鎖控制實作 (Stage 2: Core Engine & Locking)

#### 🎙️ 你對面試官說：
> *「資料契約確認很扎實。第二步我們讓 AI 實作 Engine 核心。我會特別要求 AI 嚴格遵守 I/O 與鎖隔離，以及防止定時器洩漏。」*

#### 📋 給 AI 的 Prompt 模板：
```markdown
Now, implement `internal/<pkg>/engine.go` (and queue/worker if needed) using the models defined in `model.go`.
Strict Architecture Constraints:
1. Lock Scope: Engine has `sync.RWMutex` protecting the registry map and priority queue.
2. Invariant: NEVER hold any lock while calling the external interface (`Execute` or `Send`)! Pop task, transition status to InFlight/Dispatched, unlock, then call external I/O in worker goroutine.
3. Timer Hygiene: Use a single `time.Timer` tracking the heap top with a wake channel. NEVER leak uncollected `time.After`.
4. Cancellation: Acquire read lock to find record, unlock engine, lock record's mutex, check status == Scheduled, set Cancelled. (Lazy discard in queue).
5. Graceful Shutdown: `Close()` stops dispatcher, drains worker channel, and cleans up timers without leaking goroutines.
```

#### 🦅 你的「獵鷹三點審查法」(Falcon Eye Check)：
代碼生成後，按 `Cmd+F` 光速搜尋 3 個詞：
1. 搜尋 `Execute` / `Send`：**往上看 5 行，確認沒有任何 `mu.Lock()` 罩在它頭上**。
2. 搜尋 `time.`：**確認沒有出現裸調用的 `time.After(...)`**。
3. 搜尋 `Cancel`：**確認是「先檢查狀態再修改」（Check-then-Act）**。

---

### 階梯三：測試與並發驗證 (Stage 3: Tests & Race Verification)

#### 🎙️ 你對面試官說：
> *「核心邏輯已完成，I/O 鎖邊界很乾淨。最後一步我們請 AI 生成全套並發測試，並在終端機跑 `-race` 驗證零數據競爭。」*

#### 📋 給 AI 的 Prompt 模板：
```markdown
Please implement comprehensive unit and concurrency tests in `internal/<pkg>/engine_test.go`.
Requirements:
1. High Concurrency Test: Simulate 100+ goroutines concurrently submitting, cancelling, and querying tasks.
2. Race Condition Test: Test the exact millisecond race between Cancel and Worker Dispatch.
3. Retry & Backoff Test: Verify exponential backoff and transition to DLQ on max retries.
4. Graceful Shutdown Test: Verify Close() terminates without leaking any goroutines.
Code must pass `go test -v -race ./...`.
```

#### 💻 終端機執行命令：
```bash
go test -v -race ./...
```
- **全綠燈 (PASS)** $\rightarrow$ 露出微笑向面試官展示：*「全套 30+ 測試通過，0 Data Race，0 Goroutine 洩漏！」*
- **若出現 Data Race 或報錯** $\rightarrow$ 不要慌！看清報錯行號，直接對 AI 下引導命令：
  > *"Test failed at line X: Data race detected on `recordsMap` in `ListDLQ`. Please protect the read loop with `e.mu.RLock()` and `defer e.mu.RUnlock()`."*

---

## 第四章：面對 Phase 4 追問的直覺應對公式 (Answering Follow-ups)

在面試最後 20 分鐘，面試官一定會問分散式擴展問題（故意探測你的天花板）。記住這個**「KISS 兩段式回答公式」**，既踏實又展現 Senior 風采：

### 🎯 KISS 兩段式回答公式
> **「第一段（成熟簡潔現成解）：在目前 Circle 一般的微服務規模下，我主張 KISS 原則，優先採用【方案 A】，因為它最簡單、有 ACID 保證且無多餘維護成本。」**
> 
> **「第二段（高階擴展儲備）：如果未來業務規模擴大 50 倍、方案 A 遇到瓶頸時，我們再循序漸進演進至【方案 B】。」**

### 🧠 四大經典追問的兩段式標準套路

| 追問情境 | 第一段：成熟現成解（方案 A - Senior 必講） | 第二段：高階擴展儲備（方案 B - Staff 加分） |
| :--- | :--- | :--- |
| **分散式排程防重複派發** | 任務存 PostgreSQL，多 Pod 用 `SELECT ... FOR UPDATE SKIP LOCKED` 並行撈取，行級鎖天然防重且互不阻塞。 | 資料庫成為瓶頸時，改用 **一致性雜湊 (Consistent Hashing)** 依商戶 ID 分區分片。 |
| **外部 API 超時防雙扣款** | **超時不等於失敗，而是未知 (In-Doubt)**！絕不盲目重試，標記未知，帶 `Idempotency-Key` 由非同步對帳 Worker 向銀行反查。 | 引入分散式 TCC / Saga 狀態協調器維護補償鏈。 |
| **業務狀態與事件發送防雙寫** | 不要跨系統做分散式事務！將業務變更與發件表記錄放在 **同一個 PostgreSQL 本地事務 (BEGIN ... COMMIT)** 內，天然 ACID。 | 高吞吐下引入 **CDC (Debezium)** 監聽 Postgres WAL 邏輯日誌解碼發至 Kafka。 |
| **分散式限流防網路延遲** | 同機房 Redis 延遲只有 0.5ms，相比外部 API (200ms) 微不足道，標準 Redis Lua 腳本足夠支撐。 | 極致低延遲採 **Token Batching（批次預領 / Quota Leasing）**，每台 Pod 每次預領 20 個在本地消耗。 |

---

## 第五章：現場突發狀況急救包與除錯心法 (Live Emergency & Debugging Protocol)

面試現場最怕遇到「測試突然卡住 (Hang)」、「噴出 Data Race」或「編譯失敗」。記住這套急救 SOP，1 分鐘內冷靜化解危機：

### 1. 終端機測試黃金命令 (Golden Test Command)
在面試中跑測試，**絕對不要只打 `go test ./...`**，永遠使用這條完整命令：
```bash
go test -v -race -count=1 -timeout 10s ./...
```
* **`-count=1`**：**強制禁用測試快取**！確保你看到的是當前代碼的真實結果，而不是舊的 `(cached)` 假象。
* **`-timeout 10s`**：**死鎖救命旗標**！Go 預設測試超時是 10 分鐘，如果代碼發生死鎖，終端機會乾等 10 分鐘毫無反應；加上 `-timeout 10s`，10 秒內會強制中斷並**自動印出所有 Goroutine 的堆疊追蹤 (Stack Trace)**，直接指出死鎖卡在哪一行！
* **`-race`**：開啟並發競爭偵測器。

---

### 2. 遇到測試卡住 (Deadlock / Hang) 的 10 秒定位法
如果 10 秒超時噴出 `panic: test timed out after 10s`：
1. 眼睛往上看堆疊日誌，搜尋：
   - `sync.(*Mutex).Lock` 或 `sync.(*RWMutex).Lock`：**代表某個協程在等一把永遠沒釋放的鎖**（常見於在持有鎖時又呼叫了另一個需要同把鎖的內部方法，造成自我死鎖）。
   - `chan receive` 或 `chan send`：**代表無緩衝 Channel 兩端沒有配對好，互相等待**。
2. **對面試官說**：
   > *「測試在 10 秒超時了，從堆疊上看，Goroutine A 卡在 `engine.go:85` 的 `mu.Lock()`，這代表前面某個分支 return 時忘了 `defer mu.Unlock()`，或發生了巢狀鎖競爭。我立刻排查這幾行。」*

---

### 3. 遇到 Data Race 報錯的 10 秒定位法
當 `go test -race` 噴出紅字 `WARNING: DATA RACE` 時，它永遠會成對印出兩個堆疊：
* `Read at 0x... by goroutine A:` $\rightarrow$ **誰在讀**（檔案與行號）
* `Previous write at 0x... by goroutine B:` $\rightarrow$ **誰在寫**（檔案與行號）

**急救心法**：99% 的原因都是「讀取者在讀 Map 或 Struct 欄位時，忘了加 `RLock()` 或該欄位沒有被鎖保護」。

---

### 4. 指揮 AI 修復錯誤的「精準手術 Prompt」
當測試報錯時，**切忌把整篇終端日誌直接丟給 AI 說「壞了幫我修」**（AI 容易把整份架構推翻重寫，越修越爛）。
採用**「精準手術式提示 (Surgical Prompt)」**：
> *"The test failed with a data race on `records` map. Goroutine 7 is reading in `ListDLQ` at line 175, while Goroutine 12 is writing in `PublishEvent` at line 120. Please ONLY protect the read in `ListDLQ` with `e.mu.RLock()` and `defer e.mu.RUnlock()`. DO NOT change any other logic or structs."*

---

### 5. 當面試官中途質疑時的「降阻溝通話術」
如果面試官在你講話或審查代碼時突然打斷：*「你這樣設計會不會有效能問題 / 鎖競爭？」*
**絕對不要直接反駁或驚慌認錯**，使用 **「認同 $\rightarrow$ 說明權衡 $\rightarrow$ 彈性調整」** 的三步話術：
1. **認同 (Acknowledge)**：*「這確實是一個很好的觀察，在極高流量下這個鎖確實可能成為潛在熱點。」*
2. **說明權衡 (Trade-off)**：*「我當初優先選擇這個方案，是為了保證臨界區足夠簡單、代碼不易出錯 (KISS)。」*
3. **彈性調整 (Bridge & Adapt)**：*「如果要追求極致吞吐，我們可以將這裡改為分段鎖 (Sharding) 或無鎖原子操作。你希望我們現在調整，還是先保留這個結構把端到端流程跑通？」*
這會讓面試官覺得你極為成熟、擅長溝通且充滿彈性！

---

## 🏆 總結：面試當天的三句心法

1. **「我是 Tech Lead，AI 是我的初級打手」**：你定義架構與鎖，AI 負責寫樣板，你驗證測試。
2. **「大步拆小步，階梯式交付」**：先定 Model 契約，再寫 Engine 核心，最後跑測試，永遠掌控節奏。
3. **「先求簡單可靠，再談高大上擴展」**：用 KISS 原則回答追問，展現最受主管青睞的務實 Senior 特質！
