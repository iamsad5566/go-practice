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

---

## 題型 5：多幣別即時換匯與跨國出金引導引擎 (Multi-Currency FX & Routing Engine)

### 題型背景
Circle 處理跨國出金與結算，支援多種主權貨幣與數位貨幣（如 USD、EUR、GBP、USDC）。商戶發起跨幣別支付時，系統必須向多個外部流動性提供商（Liquidity Providers / LPs）詢價，鎖定報價（Quote with TTL），凍結發起方資金，並在時效內清算入帳。此題目純屬傳統金融後端高並發架構，不涉任何區塊鏈知識。

### 核心功能需求
1. **即時報價鎖定 (Quote Request & TTL Lock)**：
   - `RequestQuote(fromCurrency, toCurrency, amount)` 向流動性渠道取得最優匯率，生成具備 15 秒生存期 (`ExpiresAt`) 的 `QuoteID`。
2. **資金兩階段預扣與清算 (Two-Phase Hold & Settle)**：
   - `ExecuteTransfer(quoteID, fromAccountID, toAccountID)`：
     - 驗證 `Quote` 是否有效未過期。
     - 原子凍結 `fromAccountID` 源幣別資金。
     - 呼叫外部結算渠道 `ClearingChannel.Settle()`。
     - 結算成功後扣減源幣別凍結款，並將目標幣別記入 `toAccountID`。
3. **過期與失敗釋放 (Rollback / Release)**：
   - 若外部通道結算失敗或報價過期，原子釋放凍結資金，回傳確切原因。

### 面試官預埋的模糊陷阱 (考驗澄清與假設)
- **報價過期毫秒級競爭 (Expiry Race)**：在呼叫 `ExecuteTransfer` 的當下瞬間報價剛好過期，或執行中過期，系統如何定義仲裁邊界？
- **跨帳戶轉帳死鎖**：若涉及到同時扣款與入帳，多協程跨幣別轉帳時如何依 ID 字典序有序加鎖避免 ABBA 死鎖？
- **微單位精度與四捨五入 (Rounding Precision)**：匯率乘以金額時產生的微小零頭如何處理？是截斷（Truncate）還是進位？各幣別的微單位精度不同（如 JPY 為 1，USD 為 1,000,000）時如何換算？

---

## 題型 6：實時交易風控與滑動窗口頻率檢核引擎 (Transaction Velocity & Risk Engine)

### 題型背景
所有經過 Circle 的金流在送往銀行通道前，必須在毫秒級（<5ms）內通過動態風控限額檢核。風控規則包含單筆金額上限、單一商戶在滾動時間窗口（如過去 1 分鐘、1 小時）內的累計金額與次數限制。

### 核心功能需求
1. **風控規則評估 (EvaluateTransaction)**：
   - 接收交易 `(TransactionID, MerchantID, Amount, Timestamp)`。
   - 評估單筆限額與滑動窗口累計限額（Sliding Window Velocity Limit）。
   - 回傳三態決策：`APPROVED`、`REJECTED`、`REVIEW_HOLD`。
2. **滑動窗口統計 (Sliding Window Aggregator)**：
   - 支援基於記憶體的高效滾動時間統計（如 Bucket ring buffer 或有序時間戳鏈表），拒絕粗暴的全量記憶體掃描。
3. **規則熱加載 (Dynamic Rule Hot-Reload)**：
   - 支援 `UpdateRules(rules)` 在不停機、無 Race Condition 下動態覆蓋現有規則，且熱更新不能長時間阻塞高頻評估。
4. **人工審核暫態隊列 (Review Hold Queue)**：
   - 標記為 `REVIEW_HOLD` 的交易進入暫態隊列，支援管理者呼叫 `ApproveHold(txID)` 或 `RejectHold(txID)`，超時未審核則依預設政策自動拒絕。

### 面試官預埋的模糊陷阱
- **滑動窗口鎖競爭與記憶體洩漏**：高並發頻繁寫入時，如果使用大鎖保護時間窗口，會成為全系統瓶頸；過期窗口記錄未清理會導致記憶體爆炸。
- **時鐘回撥與亂序交易**：若伺服器時間微幅漂移，或網絡延遲導致較早時間戳的交易晚到達，滑動窗口如何處理？
- **三態審查超時競爭**：當後台管理者正在按 Approve 的同時，超時自動退回的 Timer 也剛好觸發，如何保證決策的原子性？

---

## 題型 7：批次出金匯總與銀行異步對帳引擎 (Batch Payout & Reconciliation Engine)

### 題型背景
為降低每筆銀行電匯與清算手續費，Circle 需將多筆小額出金即時聚合成批次（Batching），定期送交銀行。同時，銀行每日會非同步傳回對帳文件（Reconciliation Statement），系統需自動比對帳本與銀行流水，揪出未平帳、漏帳或微差交易。

### 核心功能需求
1. **雙觸發動態批次匯總 (Dual-Trigger Batch Accumulator)**：
   - 商戶提交出金 `SubmitPayout(payoutID, merchantID, amount)`。
   - 批次觸發條件：**數量達到上限（如 100 筆）OR 距離上次送出已達時間上限（如 3 秒）**，兩者先到者觸發打包，送至外部 `BankFileSender.SendBatch(batch)`。
2. **批次鎖定與取消競爭 (Batch Lock & Cancel)**：
   - 出金在打包前可隨時呼叫 `CancelPayout(payoutID)` 取消；一旦批次已觸發送交銀行，取消操作必須回傳 `ErrBatchInFlight`。
3. **銀行流水自動對帳 (Reconciliation Processor)**：
   - 接收銀行非同步對帳清單 `[]BankTransactionRecord`。
   - 自動將內部記錄標記為 `RECONCILED`、`MISMATCH_AMOUNT`、或 `MISSING_IN_LEDGER`。
   - 支援手續費公差容忍（Penny Tolerance，如差異 $\le$ 1 美分時自動平衡入手續費帳目）。

### 面試官預埋的模糊陷阱
- **定時器與協程洩漏**：每次有新交易加入或定時器到期時，若未使用正確的 `time.Timer` 重設（Reset）模式，容易產生數萬個死掉的 Goroutine 與 Timer 殘留。
- **外部 I/O 阻塞臨界區**：在打包好批次呼叫 `BankFileSender.SendBatch` 進行網絡 I/O 時，如果依然持有批次鎖，將導致所有後續出金請求被活活卡死。
- **對帳單重放攻擊與冪等性**：銀行網路可能重複推送同一份對帳文件，對帳引擎如何防止手續費重複抵扣與狀態二次竄改？
