# Circle Senior/Staff SWE: 核心題型庫與高仿真 PRD (Problem Archetypes)

本文件收錄 Circle 在 90 分鐘 AI 協同實作 / Pair Programming 輪次中最高頻出現的 **4 大實戰題型** 與規格書設計。

---

## 題型 1：高並發金融帳本與轉帳引擎 (High-Concurrency Double-Entry Ledger & Transfer Engine)

### 題型背景
Circle 核心業務為 USDC 發行與跨國結算。此題型評估候選人對**帳戶餘額、資金流轉、防死鎖、資料一致性與防透支**的實戰能力。

### 核心功能需求
1. **帳戶管理 (Account Registry)**：
   - 建立帳戶（設定唯一 `AccountID`、幣種 `Currency`，初始餘額可為 0 或給定正數）。
   - 查詢帳戶即時餘額。
2. **轉帳操作 (Transfer)**：
   - 支援從 `FromAccountID` 轉帳至 `ToAccountID`。
   - 扣除與入帳必須為原子操作（Atomic Check-then-Act）。
   - 禁止負餘額（除非有特殊透支授權），禁止自我轉帳。
3. **資金預扣與結算 (Hold / Escrow & Settle) [進階擴充]**：
   - 支援先凍結款項 `HoldBalance`，待外部條件滿足後呼叫 `SettleHold` 結算或 `ReleaseHold` 退回。
4. **審計與明細 (Audit History)**：
   - 記錄每筆交易的流水號、時間戳、變動金額、前後餘額。

### 面試官預埋的模糊陷阱 (考驗澄清與假設)
- **手續費**：手續費由誰吸收？是內扣（收款方少收）還是外加（轉出方多扣）？如果餘額剛好等於轉帳金額，外加手續費是否導致餘額不足？
- **精度問題**：是否允許浮點數？如何定義最小計量單位（微單位 micro-units）？
- **死鎖問題**：若 Goroutine 1 執行 `A -> B`，同時 Goroutine 2 執行 `B -> A`，如何防止 ABBA 互鎖？
- **溢位問題**：若帳戶餘額接近 `math.MaxInt64`，入帳是否會發生正負反轉溢位？

---

## 題型 2：帶 TTL、版本號與快照的高性能記憶體數據庫 (In-Memory Key-Value Store with TTL & Snapshots)

### 題型背景
類似 CodeSignal 漸進式實作或 Circle 內部快取/結算狀態維護。考核並發讀寫分離、定時淘汰與資源洩漏防護。

### 核心功能需求
1. **基礎 CRUD**：`Set(key, val)`, `Get(key)`, `Delete(key)`。
2. **生存時間 (TTL & Expiration)**：
   - `SetWithTTL(key, val, ttl)`。
   - `Get` 時若過期需回傳 Not Found，並進行惰性清理 (Lazy Eviction)。
   - 背景主動清理過期 Key (Active Eviction)，不得造成內存洩漏。
3. **版本控制與時間旅行快照 (MVCC / Snapshot Query)**：
   - 每次寫入遞增版本號或記錄時間戳。
   - 支援 `GetAt(key, timestamp)` 讀取歷史狀態，且快照讀取不得長時間阻塞前台寫入。
4. **前綴批量檢索 (Prefix Scan)**：
   - `ScanPrefix(prefix)` 回傳符合的所有 Key-Value。

### 面試官預埋的模糊陷阱
- **背景 Worker 洩漏**：背景清潔工 goroutine 是否能在系統關閉或重置時透過 `context.Context` 優雅退出？
- **鎖競爭 (Contention)**：如果只有一把全域 `sync.RWMutex`，頻繁的 TTL 清理與前綴掃描會不會徹底卡死高頻 `Set`？（是否需要分段鎖 Sharded Lock 或讀寫分離？）
- **時間單調性**：依賴 `time.Now()` 是否會受伺服器 NTP 時鐘回撥影響？

---

## 題型 3：支付排程與代幣桶限流網關 (Payment Scheduler & Rate-Limiting Gateway)

### 題型背景
Circle 平台必須對接多個外部銀行網路（Fedwire, ACH, SEPA）與區塊鏈節點，各管道有嚴格的每秒請求數 (QPS) 與每分鐘交易額 (Volume Limit) 限制。

### 核心功能需求
1. **交易排程 (Scheduling)**：
   - 接收交易請求，指定未來的執行時間 `ExecuteAt` 或依優先級依序處理。
   - 到期時由背景 Worker Pool 非同步派發。
2. **細粒度限流 (Rate Limiter)**：
   - 針對個別商戶 (`MerchantID`) 或渠道 (`ChannelID`) 實作 Token Bucket 或 Sliding Window Counter。
   - 超出速率限制時，回傳 `RateLimitExceeded` 錯誤或進入排隊緩衝區。
3. **重試與退避 (Retry with Backoff)**：
   - 外部通道暫時性失敗時，支援指數退避 (Exponential Backoff with Jitter) 重試，最多重試 N 次。

### 面試官預埋的模糊陷阱
- **Timer 洩漏**：若大量排程請求進來，使用 `time.After` 是否會導致數萬個未觸發的 Timer 在記憶體堆積無法回收？
- **取消語意 (Cancellation)**：在排程未觸發前，商戶如果取消交易，排程器如何及時取消？
- **精準度 vs 性能**：滑動窗口演算法在極高並發下使用 Mutex 是否會成為瓶頸？是否應考慮原子操作 (`atomic`)？

---

## 題型 4：事務性發件箱與 Webhook 交付引擎 (Transactional Outbox & Reliable Webhook Dispatcher)

### 題型背景
當 Circle 完成鏈上結算或入帳時，需要保證 100% 可靠地通知商戶系統（Webhook），不允許漏通知，且商戶系統可能偶發性當機。

### 核心功能需求
1. **可靠事件持久化 (Outbox Event Enqueue)**：
   - 本地業務狀態變更與發件箱事件入列必須具有原子性。
2. **非同步派發 Worker (Concurrent Dispatcher)**：
   - 多個背景 Worker 平行消費發件箱，向商戶 Webhook URL 發送 HTTP POST。
3. **冪等性保障 (Idempotency Key)**：
   - 請求攜帶 `X-Delivery-ID` 與 `X-Signature`。
4. **死信隊列與報警 (DLQ & Alerting)**：
   - 重試耗盡後轉入 Dead Letter Queue (DLQ)，標記失敗原因並觸發警報。

### 面試官預埋的模糊陷阱
- **併發重複消費**：多個 Worker 同時撈取待發送事件時，如何防止同一事件被兩個 Worker 同時發送？
- **At-least-once 語意**：商戶端已處理但 HTTP Response 逾時，Worker 重試時商戶端如何保證冪等？
- **順序保證 (Ordering)**：同一個商戶的多個狀態變更事件是否需要嚴格保持先後順序？
