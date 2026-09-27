# Circle Senior SWE 技術面試實戰與通關寶典 (PRD & AI Pair Implementation)

本專案收錄了 Circle 90 分鐘技術面試（AI 協同實作 / Technical Pair Programming Round）的 **4 大高頻實戰題型完整實作**、**PRD 規格書**、**全套 Race-Free 測試套件**，以及 **Senior vs. Staff 評審維度與臨場應對寶典**。

---

## 🧭 導讀：揭密 Senior vs. Staff 的真實面試考核邊界

在面試過程中，許多候選人面對 Phase 4 的深水分散式追問常會產生難度焦慮。
以下是矽谷一線大廠（Circle、Stripe、Coinbase）評審委員會的底層邏輯：

### 1. 什麼是「自適應壓力摸頂測試 (Adaptive Stress-Testing)」？
* **前 65 分鐘（架構與代碼）**：面試官只看你是否達到 **Senior 及格線**（代碼可運行、無 Data Race、鎖邊界乾淨、懂得鎖內不做 I/O）。
* **後 25 分鐘（高階追問）**：一旦你通過及格線，面試官一定會**故意往 Staff/Principal 的深度探測（Find the Ceiling）**，一路問到你答不出來為止。
* **打分真相**：
  - 摸頂題答不出來，或只給出標準成熟解法 $\rightarrow$ **100% 依然是 Senior Hire（完全不扣分）**！
  - 摸頂題剛好答出高階架構 $\rightarrow$ 評語額外標記「具備 Staff 潛力（加分）」。

### 2. Senior 及格線 vs. Staff 加分項對照表

| 面試維度 | 🟢 Senior 必拿基準線 (Pass Bar - 權重 80%) | 🌟 Staff 摸頂加分項 (Bonus - 不會不影響錄取) |
| :--- | :--- | :--- |
| **Phase 1: PRD 需求澄清** | • 主動點出 1~2 個邊界（精度、取消競爭、過期策略）。<br>• 給出合理的工程假設並說明理由。 | 洞察跨業務線法規合規、多區域資料主權架構。 |
| **Phase 2: 架構與資料模型** | • 碰代碼前定義好 Struct、狀態機。<br>• 具備基本鎖分級意識，**絕對不在持有鎖時做網路 I/O**。 | 設計動態時間輪 (Timing Wheel)、無鎖環形隊列 (Lock-free RingBuffer)。 |
| **Phase 3: AI 實作與防禦** | • **漸進式階梯實作**：先 Model/契約 $\rightarrow$ 再 Engine $\rightarrow$ 最後 Test。<br>• 擔任 Gatekeeper 抓出並發漏洞，`go test -race` 綠燈通過。 | 極限壓榨 CPU Cache Line、達成 0 記憶體配置 (0 allocs)。 |
| **Phase 4: 分散式追問** | • **KISS 原則**：善用成熟組件（DB 事務、`SKIP LOCKED`）。<br>• 金融思維：超時不等於失敗，需標記未知並反查對帳。 | 深入推演多區域 Paxos/Raft 底層共識、Formal Verification 形式化驗證。 |

### 3. 最受面試官欣賞的 Senior 答題心法：KISS 哲學 (Keep It Simple, Stupid)
面試官最忌諱「面試 Senior 卻開口閉口都是過度複雜的 Staff 大架構」。最得體、最高分的 Senior 回答架構永遠是：
> **「在目前 Circle 的一般生產規模下，我主張 KISS 原則，優先採用成熟、維護成本最低的【方案 A（如 DB 事務 / SKIP LOCKED）】解決問題，避免引入多餘的中間件負擔；未來如果業務成長 50 倍、資料庫出現瓶頸時，我們再循序漸進演進至【方案 B（分片 / CDC）】。」**

---

## 🛠️ Phase 3 AI 協同實作：資深工程師的「三階梯實作法」

切忌直接將整篇 800 行 Prompt 丟給 AI 一次生成全部代碼（容易大腦超載、喪失主導權、難以 Debug）。採用分階段交付：

```text
[Step 1：定義契約] ──► [Step 2：核心並發邏輯] ──► [Step 3：測試與 Race 驗證]
  (約 3~5 分鐘)           (約 15~20 分鐘)              (約 10~15 分鐘)
```

1. **Step 1（先給 Model 與 Interface）**：
   - Prompt：*"先幫我實作 `model.go`，定義資料結構、錯誤定義、狀態枚舉以及核心 Interface。注意：需包含保護單筆狀態的 `sync.Mutex`。先不要實作 Engine 內部邏輯。"*
   - 花 30 秒確認欄位與鎖定義，確認無誤再繼續。
2. **Step 2（實作 Engine 核心）**：
   - 快速 Review 3 大紅線：
     - ① 外部 I/O（`Execute` / `Send`）前**必須先釋放鎖**。
     - ② 絕無未回收的 `time.After`。
     - ③ 取消邏輯為先查再改（Check-then-Act）。
3. **Step 3（產出測試套件）**：
   - 終端執行 `go test -v -race ./...` 驗證 100% 綠燈通過。

---

## 📚 四大核心實戰題型全解析

### 題型一：高並發金融帳本與轉帳引擎 (Double-Entry Ledger & Transfer Engine)
* **專案路徑**：[`solutions/topic1-double-entry-ledger`](file:///Users/twyk/Projects/go-practice/solutions/topic1-double-entry-ledger)

#### 1. Phase 1：必說澄清與工程假設
- **數值精度 (Precision Invariant)**：
  - *"在金融場景中嚴禁使用 `float64`。我一律假設金額以最小微單位（Micro-units, `int64`）儲存，1 USDC = 1,000,000 micro-units。"*
- **數值安全 (Overflow & Negative)**：
  - 入帳時必須主動加入 `math.MaxInt64 - amount < balance` 溢位防禦，且禁止 `<= 0` 的非正數金額。
- **手續費語意 (Fee Deductibility)**：
  - 主動向面試官對齊：*手續費是「內扣」（收款人實得少）還是「外加」（付款人多扣）？*

#### 2. Phase 2 & 3：架構邊界與並行控制
- **雙層鎖架構 (Two-Level Locking)**：
  - 註冊表層：`sync.RWMutex`（查找帳戶用 `RLock`；僅在註冊新帳戶時用 `Lock`）。
  - 帳戶實體層：各帳戶自帶 `sync.Mutex` 保護 `Balance`。
- **確定性鎖排序 (ABBA 死鎖根除)**：
  - 跨帳戶轉帳（A 轉 B）時，**必須先比對 ID 字典序（`idA < idB`）依序獲取鎖**，離開時依相反順序釋放。
- **兩階段驗證與變更 (Check-then-Act)**：
  - 鎖住雙邊帳戶 $\rightarrow$ 驗證餘額充足且無溢位 $\rightarrow$ 雙邊扣款與入帳 $\rightarrow$ 釋放鎖。絕不可「先扣 A，釋放鎖，再去鎖 B 加錢」。

#### 3. Phase 4：追問應對指南
- **Q1: 擴展到多節點分散式環境，如何避免死鎖與保證一致性？**
  - **🟢 Senior 穩拿回答**：採用 **依帳戶分區 (Account Partitioning / Sharding)**：利用一致性雜湊將特定帳戶的所有轉帳請求固定路由到同一個節點或 Worker 循序處理，把跨節點並發退化為單節點有序處理。
  - **🌟 Staff 深入摸頂**：跨分區轉帳改採 **TCC (Try-Confirm-Cancel) / Saga 模式**（Try 扣可用餘額轉凍結資金，Confirm 在目標入帳並解凍，失敗則 Cancel 原路退回）。
- **Q2: 什麼是雙錄記帳 (Double-Entry Bookkeeping)？為什麼不能直接 `balance += x`？**
  - **🟢 Senior 穩拿回答**：金融機構要求審計不可篡改。每筆交易必須同時記錄借方 (Debit) 與貸方 (Credit)，且借貸必相等。餘額只是所有歷史流水事件的聚合計算。重放日誌即可校驗總帳平衡，杜絕憑空生錢或漏帳。

---

### 題型二：帶 TTL、版本號與快照的高性能 KV 資料庫 (In-Memory KV Store)
* **專案路徑**：[`solutions/topic2-in-memory-kv`](file:///Users/twyk/Projects/go-practice/solutions/topic2-in-memory-kv)

#### 1. Phase 1：必說澄清與工程假設
- **背景 Worker 洩漏防護 (Zero Leak)**：
  - *"主動過期清理的背景 Goroutine 必須接收 `context.Context` 或透過 `Close()` channel 進行優雅關機，防止 Goroutine 洩漏。"*
- **時間單調性 (Monotonic Clock)**：
  - 使用 `time.Now()` 的單調時鐘讀數，防止伺服器 NTP 時鐘回撥導致原本未過期的 Key 被提前誤刪。

#### 2. Phase 2 & 3：架構邊界與並行控制
- **分段鎖 (Sharded Locking)**：
  - 嚴禁用單一全域 `sync.RWMutex`。將 Key 依雜湊值分配到 32 個獨立的 Shard，每個 Shard 各自擁有獨立的 `sync.RWMutex`。
- **快照查詢 (Point-in-Time Query / MVCC)**：
  - 歷史版本維護為依時間排序的 Slice：`[]VersionItem`，使用二分搜尋 (`sort.Search`) 在 $O(\log V)$ 找到歷史點的值。
- **縮減臨界區與防切片洩漏**：
  - 在鎖內只做指針快照讀取，不可在持有寫鎖時遍歷大量數據；淘汰舊版本時防範底層 Array 引用洩漏。

#### 3. Phase 4：追問應對指南
- **Q1: 重開機如何保證數據不丟失？WAL 與快照如何協同？**
  - **🟢 Senior 穩拿回答**：採用 **WAL + Snapshot**：寫入時先 append 寫入 WAL 落盤，再寫記憶體。定期生成全量 Snapshot 鏡像，寫入後截斷 (Truncate) 該時間點前的 WAL。
  - **🌟 Staff 深入摸頂**：高吞吐下引入 **Group Commit**（多協程寫入合併為一次批次 `fsync` 刷盤）。
- **Q2: 什麼是 Safe-TS 快照隔離？為什麼不能只看本地 `time.Now()`？**
  - **🟢 Senior 穩拿回答**：節點間有時鐘漂移與複製延遲。**Safe-TS 是一條安全水位線**，只有確定所有時間戳 $\le T_{\text{safe}}$ 的交易都已同步完成，才允許執行快照讀取，確保線性一致性。

---

### 題型三：支付排程與代幣桶限流網關 (Payment Scheduler & Rate-Limiter Gateway)
* **專案路徑**：[`solutions/topic3-payment-scheduler`](file:///Users/twyk/Projects/go-practice/solutions/topic3-payment-scheduler)

#### 1. Phase 1：必說澄清與工程假設
- **取消與派送的毫秒級競爭 (Cancellation Race)**：
  - *"在任務被 Worker 撈出並呼叫銀行的瞬間，狀態流轉到 `DISPATCHED`。在此之前（包含退避期），商戶都可以取消；一旦流轉到 `DISPATCHED`，Cancel 立即失敗並回傳 `ErrInProgress`。"*
- **代幣桶計算頻率 (Lazy Refill)**：
  - *"拒絕為每個租戶開背景 Ticker，採用 Lazy Refill——每次請求進來時，依 $\Delta t \times \text{RefillRate}$ 動態補足代幣，節省 CPU 與協程。"*

#### 2. Phase 2 & 3：架構邊界與並行控制
- **單一計時器與動態喚醒 (Single Timer Pattern)**：
  - 核心佇列採用 Min-Heap（按 `ExecuteAt` 排序），**嚴禁使用 `time.After`**。
  - 整個 Dispatcher 只有一個 `time.Timer` 對齊堆頂；當有更早的任務插入時，透過 `wake` channel 喚醒並重設 `timer.Reset()`。
- **I/O 與臨界區完全隔離 (不可違背之紅線)**：
  - Engine 寫鎖只在 Heap 的 `Push`/`Pop`（微秒級）時短暫持有。
  - **呼叫外部金融通道 (`PaymentExecutor.Execute`) 全程不持有任何鎖**。
- **多租戶零鎖爭用**：
  - 外層 RWMutex 只保護 `Tenant -> Bucket` 映射；每個 Bucket 自帶 `sync.Mutex`，不同商戶之間的限流完全平行。

#### 3. Phase 4：追問應對指南
- **Q1: 單機 Min-Heap 在水平擴展到 10 個 Pod 時如何避免重複派發？**
  - **🟢 Senior 穩拿回答（KISS 原則典範）**：直接將任務存於 PostgreSQL，多個 Pod Worker 透過 `SELECT ... FOR UPDATE SKIP LOCKED` 撈取任務。資料庫行級鎖自動保證單一任務只會被一個 Pod 領走，且互不阻塞。
- **Q2: 全域限制 200 QPS，但有 20 台伺服器，如何避免每次打 Redis 造成延遲？**
  - **🟢 Senior 穩拿回答**：同機房 Redis 的網路延遲（約 0.5ms）相比於外部銀行 API（100~500ms）微不足道，標準 Redis Lua 腳本完全堪用。
  - **🌟 Staff 深入摸頂**：極限低延遲可採 **Token Batching（批次預領 / Quota Leasing）**，每台機器每次向 Redis 預領一批額度放在本地消耗。
- **Q3: 呼叫外部銀行遭遇 HTTP Timeout 逾時，如何防止重試造成重複扣款？**
  - **🟢 Senior 穩拿回答（金融級核心原則）**：**網路超時不等於失敗，而是三態邏輯中的未知 (In-Doubt)**！絕不盲目重試，標記為 `IN_DOUBT`，帶上唯一 `Idempotency-Key`，由獨立對帳 Worker (Reconciliation Worker) 向銀行 Status API 反查確認。

---

### 題型四：事務性發件箱與 Webhook 交付引擎 (Transactional Outbox & Webhook Dispatcher)
* **專案路徑**：[`solutions/topic4-transactional-outbox`](file:///Users/twyk/Projects/go-practice/solutions/topic4-transactional-outbox)

#### 1. Phase 1：必說澄清與工程假設
- **隊頭阻塞與非同步重試 (Head-of-Line Blocking Defense)**：
  - *"如果某商戶當機，系統絕對不能同步卡在當前任務一直重試！失敗任務應計算好退避時間後，移入非同步延遲佇列，當前 Worker 立即釋放去處理其他事件。"*
- **順序性與交付保證**：
  - *"採用 At-Least-Once Delivery。在 Header 與 Payload 帶上遞增 Sequence Number 與 Timestamp，由商戶端做最終狀態冪等與去重。"*

#### 2. Phase 2 & 3：架構邊界與並行控制
- **競爭消費防重複派發 (Competing Consumers)**：
  - 採用 Go Buffered Channel (`eventCh chan *EventRecord`) 作為派發媒介，Go Channel 保證單一事件絕對只會被一個 Worker 領取。
- **HTTP 呼叫與鎖隔離**：
  - 密鑰簽名與 HTTP POST 全程在無鎖環境下由 Worker 執行。
- **讀取保護與深拷貝**：
  - `ListDLQ` 走訪 map 時嚴格加上 `e.mu.RLock()`，防範 Go fatal concurrent map read/write 崩潰；回傳時使用 `clone()` 深拷貝防指針逃逸。
- **舊數據回收器 (Reaper)**：
  - 定期清理已終結 (`COMPLETED` 或留存超時的 `DEAD_LETTER`) 事件，防止記憶體無上限膨脹。

#### 3. Phase 4：追問應對指南
- **Q1: 如何保證業務數據庫更新與 Webhook 事件發送的原子性（解決雙寫問題）？**
  - **🟢 Senior 穩拿回答（Transactional Outbox 核心）**：不要跨兩個系統做分散式事務！將業務變更（如餘額扣款）與 Outbox 事件寫入，放在 **同一個 PostgreSQL 本地事務 (BEGIN ... COMMIT)** 內。同一個 DB 事務天然保證 ACID。後續再由非同步 Worker 撈取 Outbox 表並派發。
  - **🌟 Staff 深入摸頂**：可採用 **CDC (Change Data Capture，如 Debezium)** 直接讀取 PostgreSQL 的 WAL 邏輯解碼日誌，零侵入將事件串流至 Kafka。
- **Q2: 如果某家壞商戶有 10 萬筆事件且每次 HTTP 都卡 30 秒，如何避免其他正常商戶被餓死？**
  - **🟢 Senior 穩拿回答**：重試上限耗盡後轉入 DLQ 隔離。運行時在隊列層設置 **單一商戶並發上限 (Per-Tenant Concurrency Cap)**（例如總共 20 個 Worker，但規定任何單一商戶最多只能同時佔用 2 個 Worker）。
- **Q3: 商戶輪換 Webhook Secret 時，如何做到零停機？如何防範重放攻擊？**
  - **🟢 Senior 穩拿回答**：
    1. **雙密鑰寬限期 (Dual-Secret Grace Period)**：輪換期間保留一段相容時間，系統同時接受舊 Key 與新 Key 驗證，等商戶端全量更新完畢後再正式廢棄舊 Key。
    2. **防重放攻擊**：商戶端接收 Webhook 時，校驗 `|now - X-Timestamp| <= 5 分鐘`。因為 Timestamp 本身被包裹在 HMAC 簽名內，攻擊者無法竄改時間戳，超過 5 分鐘的重放請求將被商戶直接拒收。

---

## 🎯 終極面試臨場記憶口訣 (4 大題型直覺反射)

```text
【金流帳本】
一防精度損失 (int64 micro-units)，二防 ABBA 排序鎖，三防溢位 MaxInt64，分散式用分區或 TCC。

【KV 儲存】
一防全域鎖 (分段 Sharding)，二防協程洩漏 (Context/Close)，三讀快照二分搜，持久化用 WAL + Snapshot。

【排程限流】
一防 Timer 堆積 (單一 Timer + Wake 堆頂)，二防 I/O 鎖死 (出隊釋鎖再做 HTTP)，三防逾時雙扣 (In-Doubt 反查對帳)。

【事務發件箱】
一防分散式雙寫 (同庫 DB 本機事務)，二防壞商戶霸佔 (租戶並發上限)，三防重放攻擊 (Timestamp 容忍窗口)。
```
