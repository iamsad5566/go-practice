# PRD: High-Performance In-Memory Key-Value Store with TTL, MVCC Snapshots & Prefix Scan

## 1. 系統背景與目標 (Background & Objective)
Circle 內部微服務架構中，多個核心金流與合規服務需要一個超低延遲、高吞吐的**記憶體鍵值儲存核心 (In-Memory KV Store)**。該服務負責快取鏈上即時狀態、商戶臨時權限 Token、以及交易路由規則。

除了常規的 CRUD 外，系統需要具備精準的生存時間管理 (TTL)、時間旅行快照查詢 (MVCC / Point-in-Time Query)、前綴批量檢索 (Prefix Scan)，並且在高並發讀寫下維持極致的低延遲，避免記憶體洩漏與背景線程失控。

---

## 2. 核心功能規格 (Functional Requirements)

### 2.1 基礎鍵值存取 (Basic KV Operations)
- **`Set(key string, value string) error`**
  - 寫入或更新鍵值對，永不過期（除非被覆蓋或顯式刪除）。
- **`Get(key string) (string, error)`**
  - 檢索鍵對應的最新值。若鍵不存在或已過期，回傳 `ErrKeyNotFound`。
- **`Delete(key string) error`**
  - 顯式刪除鍵值對。若鍵不存在可回傳 `ErrKeyNotFound` 或視為冪等成功。

### 2.2 生存時間與雙重淘汰機制 (TTL & Active/Passive Eviction)
- **`SetWithTTL(key string, value string, ttl time.Duration) error`**
  - 寫入鍵值對並設定存活時間（TTL）。
  - 若 `ttl <= 0`，需有明確語意（如拒絕或視同立即過期）。
- **惰性淘汰 (Passive/Lazy Eviction)**：
  - 在呼叫 `Get(key)` 或 `ScanPrefix` 時，若偵測到該 Key 已過期，需即時清理並回傳 `ErrKeyNotFound`。
- **主動背景淘汰 (Active Eviction & Worker Lifecycle)**：
  - 系統需運行背景清潔工 (Background Cleaner)，定期（或基於事件/取樣）掃描並清理已過期的鍵，防止記憶體無上限膨脹。
- **優雅終止 (Graceful Shutdown)**：
  - 提供 `Close()` 或傳入 `context.Context`，確保背景清潔 Worker 能在關閉時乾淨退出，**絕對不可洩漏 Goroutine**。

### 2.3 版本控制與時間旅行快照 (MVCC & Point-in-Time Snapshot Query)
- **歷史版本追蹤**：
  - 每次寫入操作均需記錄其生效時間戳（Unix Timestamp / Nano）或遞增版本號。
- **`GetAt(key string, timestamp int64) (string, error)`**
  - 查詢該 Key 在指定歷史時間戳 `timestamp` 當下的有效值（Point-in-Time Read）。
  - 若在該時間點該 Key 尚未建立、或當時已過期/被刪除，需回傳 `ErrKeyNotFound`。
- **讀寫非阻塞要求**：
  - 歷史快照查詢與讀取操作**不得長時間佔用全域寫入鎖**，前台並發的 `Set` 操作不能被快照查詢卡死。

### 2.4 前綴批量檢索 (Prefix Scan)
- **`ScanPrefix(prefix string) (map[string]string, error)`**
  - 檢索所有符合指定前綴 `prefix` 的有效（非過期、非刪除）鍵值對。
  - 回傳當前時間點的最新快照數據。

---

## 3. 非功能性要求 (Non-Functional Requirements)

1. **極致線程安全與並發驗證 (Thread Safety & Concurrency)**
   - 必須 100% 通過 `go test -v -race ./...` 驗證，高並發讀寫無 Data Race。
   - 嚴格防禦死鎖 (Deadlock-Free)。
2. **低鎖延遲與鎖分級 (Low Contention & Lock Hierarchy)**
   - 避免粗暴的全域大鎖（Single Global Lock），避免讀寫完全串行化。
   - 需設計細粒度鎖、分段鎖 (Sharded Locking) 或讀寫分離模式 (`sync.RWMutex`)。
3. **無 Goroutine 與記憶體洩漏 (Zero Resource Leak)**
   - 背景清理 Worker 必須能夠被優雅通知退出。
   - 避免使用 `time.After` 產生累積未釋放的 Timer。
4. **時鐘單調性與邊界校驗 (Clock Monotonicity & Boundary Invariants)**
   - 處理 TTL 與過期時需考慮時鐘回撥防禦，推薦使用 `time.Now()` 的單調時鐘讀數。

---

## 4. 候選人澄清與情境假設記錄區 (Clarifications & Assumptions Log)

### 4.1 MVCC 歷史保留與刪除語意 (已於 Phase 1 對齊)
1. **刪除與 TTL 機制（邏輯標記 + Retention Window）**：
   - `Delete` 與過期的 Key 不會立即從記憶體物理清除，而是寫入 Tombstone（標記為已刪除/過期）記錄，以利 `GetAt` 能正確追溯歷史狀態。
   - **Retention Window（10 分鐘）**：為避免記憶體無限膨脹，歷史版本只保留最近 10 分鐘內的 `ChangeRecord`，超過 10 分鐘且已被覆蓋/刪除的歷史版本由背景 Cleaner 清理。
   - **重新激活 (Re-activation)**：若 Key 先前被刪除或過期，再次呼叫 `Set` 或 `SetWithTTL` 將視為寫入全新版本並重新激活。

### 4.2 儲存模型與檢索複雜度 (Data Model & Complexity)
1. **變更歷史鏈 (`[]*ChangeRecord`)**：
   - 每個 Key 維護一個按時間嚴格遞增排序的變更歷史切片。
   - **最新值檢索 (`Get`)**：直接取切片末尾元素，時間複雜度 $O(1)$。
   - **歷史值檢索 (`GetAt`)**：對切片的時間戳進行二分搜尋 (Binary Search)，時間複雜度 $O(\log N)$。
2. **單調時間戳防護 (Monotonic Clock Defense)**：
   - 寫入時若偵測到當前系統時鐘 `time.Now()` 小於等於最新一筆記錄的 `createdAt`，強制調整為 `createdAt + 1 nanosecond`，確保時間戳嚴格單調遞增，杜絕二分搜尋死角與時鐘回撥問題。

### 4.3 並行架構與前綴檢索權衡 (Locking & Scan Trade-off)
1. **分段讀寫鎖 (Sharded RWMutex)**：
   - 採用 32 或 64 個獨立分片 (Shards)，透過 `FNV-1a` 或 `Murmur3` 雜湊將 Key 映射至特定 Shard。
   - 每個 Shard 擁有獨立的 `sync.RWMutex`，大幅降低鎖競爭 (Lock Contention)。
2. **前綴掃描 (`ScanPrefix`) 弱一致性**：
   - 權衡取捨：為避免全域大鎖長時間凍結整個資料庫的寫入，`ScanPrefix` 採用逐個 Shard 分段獲取 `RLock` 的方式進行快照掃描。
   - 具備 Shard 內的一致性，全局具備弱一致性 (Weakly Consistent / Segment Snapshot)，以極大化前台寫入吞吐量。

