---
name: circle-interview-prep
description: >-
  Comprehensive guide and evaluation rubric for Circle Senior/Staff SWE Technical Interviews (PRD-driven AI Implementation Round).
  Use this skill to master the PRD clarification & classification workflow, drive AI as a Tech Lead, excel at Go concurrency and data integrity,
  and ace deep architectural follow-ups.
---

# Circle Senior/Staff SWE Interview Playbook (PRD & AI Pair Implementation)

This skill guides preparation and execution for Circle's **AI Implementation Technical Interview** (90-Minute Round, conducted in Chinese / 中文進行).

在這一輪面試中，面試官關注的是候選人作為 **Tech Lead / 系統負責人** 的綜合能力：能否在面對模糊不清的 **PRD / 規格書** 時展現**獨立思考能力**，主動提出高含金量的**澄清與情境化假設**，在動筆前主導系統架構與鎖分級，指揮 AI 輔助實作，並在最後的 Follow-up 中深入探討分散式架構與資料一致性。

---

## 1. 核心評估維度與信號標準 (Core Evaluation Rubric)

Circle 對 Senior 與 Staff 候選人的評估聚焦在以下五大維度（特別看重**獨立思考**與**主動澄清假設**）：

| 維度 (Dimension) | 面試官看什麼 (Interviewer Expectations) | 強烈正面信號 (Senior / Staff Signal) | 紅旗警訊 (Down-level / Reject) |
| :--- | :--- | :--- | :--- |
| **1. 模糊澄清與合理假設<br>(Clarification & Assumptions)** | 能主動抓出 PRD 盲點，區分「需向業務對齊的問題」與「工程自主的合理假設」。 | **主動主導**：不盲從規格，逐條梳理邊界情境；主動向面試官對齊不確定性，並清晰陳述自身技術假設與取捨。 | 默不作聲自行盲猜；把整份 PRD 直接複製貼給 AI，讓 AI 決定業務邏輯與架構。 |
| **2. 獨立架構主導權<br>(Architectural Sovereignty)** | 具有獨立思考能力，在碰代碼前先定義好數據模型、狀態機、鎖層級（Lock Hierarchy）與錯誤契約。 | **Tech Lead 姿態**：先定義 struct 欄位、排序鎖（防止 ABBA 死鎖）、Two-Phase 檢查與變更，再讓 AI 產出樣板。 | 被 AI 牽著鼻子走；AI 寫什麼就收什麼；架構缺乏一致性邊界；無法說出為什麼這樣設計。 |
| **3. 並行控制與資料完整性<br>(Concurrency & Integrity)** | 對金融級並發的直覺反應：Race condition、死鎖、鎖競爭、浮點數精度損失、整數溢位。 | 雙層鎖（Registry RWMutex + Entity Mutex）、排序鎖避免死鎖、Check-then-Act 原子性、`go test -race` 零競爭。 | 全域大鎖硬幹、隨意無序加鎖、使用 `float64` 處理金額、忽略 `int64` 溢位或負數校驗。 |
| **4. 高訊號 AI 協同合作<br>(High-Signal AI Pairing)** | 將 AI 視為「實作副手」而非「決策者」。給出精確架構約束與邊界條件，對 AI 代碼進行嚴格把關。 | Architecture-First 提示詞模板；逐行 Code Review 挑出 AI 並發盲點（如值複製 Mutex、切片洩漏）；精準引導修復。 | 模糊提示（「請實作這個 PRD」）、反覆試錯型 Prompting、看不出 AI 產出代碼中的並發漏洞。 |
| **5. 溝通表達與深度防禦<br>(Think Out Loud & Follow-up)** | 全程「中文思考外顯 (Think Out Loud)」；主動說明 Trade-offs；深入回答分散式高階追問。 | 溝通有條理、主動說明為何選擇方案 A 而非 B；在 90 分鐘架構演進中游刃有餘（2PC/Saga, WAL, Sharding）。 | 沉默寫代碼；測試報錯時驚慌失措；無法將內存原型延伸映射至分散式生產架構。 |

---

## 2. 90 分鐘節奏控制藍圖 (The 90-Minute Tactical Timeline)

面試整體時間為 **90 分鐘**，以中文互動進行：

```text
[00:00 - 15:00] Phase 1: PRD 需求深度剖析、模糊澄清與情境假設 (Clarification & Assumptions)
    └─ 快速掃描 PRD，抓出矛盾與模糊處。
    └─ 分類對齊：(A) 涉及業務核心語意的模糊點（向面試官提問澄清）
               (B) 範圍與工程邊界的合理假設（主動清晰陳述技術情境假設）。
    └─ 展現獨立思考，確定核心 Invariants（一致性、數值精度、狀態機）。

[15:00 - 25:00] Phase 2: 架構藍圖設計與 Think Out Loud (Architecture & Mental Model)
    └─ 構思核心資料結構（Struct fields, State transitions）。
    └─ 設計鎖層級與並行控制策略（Registry RWMutex vs Entity Mutex, 避免 ABBA 死鎖的排序鎖機制）。
    └─ 與面試官以中文快速對齊架構輪廓，確認思路無偏誤。

[25:00 - 70:00] Phase 3: AI 協同實作、代碼審查 (Gatekeeper) 與 Race 驗證 (AI Implementation)
    └─ 使用「架構優先提示詞 (Architecture-First Prompt)」讓 AI 產出核心邏輯與測試。
    └─ 嚴格把關 (Gatekeeper Review)：檢查鎖範圍、Mutex 複製、切片引用洩漏、溢位校驗。
    └─ 執行 `go test -v -race ./...` 驗證高並發下資料一致性與零 Race condition。

[70:00 - 90:00] Phase 4: 面試官分散式架構與高階演進追問 (Distributed Follow-ups & Evolution)
    └─ 將單機記憶體系統擴展為多節點/高可用分散式架構（Sharding, Consistent Hashing）。
    └─ 討論持久化與高吞吐（WAL, Snapshot, 双錄記帳 Double-Entry）。
    └─ 網路異常與重試（Idempotency Key, 分散式事務 2PC vs Saga, Outbox Pattern）。
```

---

## 3. 獨立思考：澄清與假設分類框架 (Clarification & Assumptions Framework)

面對故意留白的 PRD，**切忌直接動工**。利用以下四大象限主動向面試官澄清，並給出專業工程假設：

### 1. 業務語意與狀態生命週期 (Business Semantics & Lifecycle)
- **模糊點澄清**：
  - *手續費扣除機制*：轉帳 $100 手續費 $2，是從轉出金額扣（實收 $98）還是額外扣款（扣款方帳戶需扣 $102）？
  - *失敗狀態流轉*：中間步驟失敗時，交易是即時標記為 `FAILED` 並 Rollback，還是進入暫態並進入 Retry 隊列？
  - *帳戶透支政策*：是否允許 Credit limit（信用透支 / 負餘額），還是絕對維持 non-negative？

### 2. 數值精度與安全邊界 (Precision & Range Invariants)
- **情境假設**：
  - *"在金融與加密資產場景中，為避免浮點數精度損失（Floating-point inaccuracy），我一律假設金額以最小微單位（Micro-units, 例如 `int64`）儲存，1 USDC = 1,000,000 micro-units。"*
  - *"我會在金額相加時主動加入 `math.MaxInt64` 溢位防禦，並拒絕任何小於等於 0 的不合法數值。"*

### 3. 並行度與一致性保證 (Concurrency & Consistency Guarantees)
- **模糊點澄清**：
  - *處理順序*：同一帳戶的並發請求是否要求嚴格 FIFO，或是依照請求的時間戳/版本號？
  - *冪等機制*：PRD 是否要求透過 `Idempotency-Key` 進行重試去重？

### 4. 工程範疇與架構取捨假設 (Engineering Scope Assumptions)
- **主動陳述**：
  - *"考慮到 90 分鐘現場實作的最佳時間分配，我今天會先以純記憶體（In-Memory）為儲存模型，但我會將 Storage 抽象成乾淨的 Interface，方便後續抽換為 PostgreSQL 或 Redis 分散式持久層。"*
  - *"我假設帳戶在系統中不支援實體刪除（Soft Delete or Read-only），以保留完整的審計日誌（Audit Trail）。"*

---

## 4. 四大不可妥協之並行與金融設計準則

在 Circle 面試中，任何涉及帳本、餘額或交易的操作必須遵守：

1. **雙層鎖架構 (Two-Level Locking)**：
   - 註冊表層級（Registry/Ledger Level）：使用 `sync.RWMutex`（查找帳戶用 `RLock`；僅在建立/註銷帳戶時用 `Lock`）。
   - 實體層級（Entity/Account Level）：使用 `sync.Mutex`（保護該帳戶的 balance 與內部明細）。
2. **確定性鎖排序 (Deterministic Lock Ordering - 根除 ABBA 死鎖)**：
   - 當跨多個資源操作時（如帳戶 A 轉帳至帳戶 B），**一律先比對 ID 大小（如 `idA < idB`）依序獲取鎖**，再執行運算。
3. **兩階段驗證與變更 (Two-Phase Commit: Check-then-Act)**：
   - 獲取所有相關鎖 -> 驗證所有前置條件（餘額是否足夠、是否溢位） -> 執行原子狀態變更 -> 釋放鎖。絕不在只獲取單邊鎖時就修改狀態。
4. **快照讀取與縮減臨界區 (Snapshot Reads for Aggregations)**：
   - 報表或聚合查詢（如 Top Spenders）時，在鎖內只做快速複製（Snapshot），並將排序、過濾移至鎖外進行，避免阻塞即時交易寫入。

---

## 5. Circle 4 大實戰題型庫 (Common Problem Archetypes)

Circle 面試偏好具備狀態流轉、高並發與金流邏輯的實用工程題目，絕非純 LeetCode 刷題：

1. **高並發金融帳本與轉帳引擎 (Double-Entry Ledger & Transfer Engine)**
   - 核心：帳戶管理、原子性轉帳、資金預扣與結算 (Hold / Release / Settle)、防透支、審計明細。
   - 陷阱：ABBA 死鎖、`math.MaxInt64` 溢位、手續費內扣/外加語意模糊、浮點數精度損失。
2. **帶 TTL、版本號與快照的高性能記憶體數據庫 (In-Memory KV Store with TTL & Snapshots)**
   - 核心：CRUD、生存時間淘汰、歷史時間旅行版本查詢 (MVCC)、前綴掃描。
   - 陷阱：全域大鎖造成寫入飢餓、背景淘汰 Worker 的 Goroutine/Timer 洩漏、快照複製開銷。
3. **支付排程與代幣桶限流網關 (Payment Scheduler & Rate-Limiter Gateway)**
   - 核心：定時執行、優先級隊列、針對 Merchant/IP 的 Token Bucket / Sliding Window 限流、指數退避重試。
   - 陷阱：`time.After` 記憶體洩漏、取消任務語意 (Cancellation)、高並發計數競爭。
4. **事務性發件箱與 Webhook 交付引擎 (Transactional Outbox & Webhook Dispatcher)**
   - 核心：業務狀態與事件寫入的原子性、並行 Worker 派發 HTTP POST、指數退避、死信隊列 (DLQ)。
   - 陷阱：多 Worker 重複消費、At-least-once 重試時的冪等性喪失、優雅關機 (Graceful Shutdown)。

👉 完整規格與陷阱剖析詳見 [problem_archetypes.md](./references/problem_archetypes.md)。

---

## 6. 面試官評分標準與量表 (Interviewer Scoring Rubric: 1 - 4 分制)

在模擬演練或真實考核中，面試官將針對以下 5 大維度進行評分（**3 分為 Senior 及格基準，4 分為 Staff+ 卓越信號**）：

| 維度 (Dimension) | 1 分 (Reject) | 2 分 (Borderline / Mid) | 3 分 (Strong Senior) | 4 分 (Staff / Principal) |
| :--- | :--- | :--- | :--- | :--- |
| **D1: 模糊澄清與情境假設** | 盲目直接動工或直接丟 raw PRD 給 AI。 | 僅問表面問題，無明確業務/工程分類，需面試官提醒。 | 主動拆解 PRD，明確分類業務對齊 vs 工程情境假設，提出數值精度與安全不變量。 | 深度洞察邊界與故障情境，主動提出符合 Circle 業務規模的權衡與架構邊界。 |
| **D2: 獨立架構主導權** | 毫無架構概念，任由 AI 生成隨機程式碼。 | 有初步設計但缺乏防死鎖意識，容易被 AI 的錯誤實作帶偏。 | 碰代碼前口述資料模型、鎖層級、字典序防死鎖與 Check-then-Act 兩階段流程。 | 設計出高擴展、模組化且對外部依賴完全解耦的介面，兼顧 KISS 與可演進性。 |
| **D3: 並行控制與資料完整性** | 出現明顯 Race Condition、ABBA 死鎖、使用 `float64` 計價或溢位。 | 使用全域大鎖粗暴解決，或在鎖內執行 I/O 造成嚴重心跳與延遲瓶頸。 | 實現細粒度雙層鎖、排序鎖，零 Race Condition (`go test -race` 通過)，防禦數值溢位。 | 對記憶體模型、無鎖原子優化 (`sync/atomic`)、鎖競爭與讀寫快照隔離有極致掌握。 |
| **D4: AI 提示詞品質與守門員審查** | 模糊 Prompt ("幫我寫轉帳")，反覆瞎試，看不懂 AI 的代碼缺陷。 | 能給出大致步驟，但缺乏精確契約約束，放過 AI 的值複製 Mutex 或切片洩漏。 | **架構優先提示詞**：包含目標、鎖約束、介面型別、前置校驗；擔任嚴格 Gatekeeper 抓出 AI 盲點。 | 指揮 AI 如臂使指；將 AI 產能發揮到極致，同時在安全與並發邊界上 100% 絕對主導。 |
| **D5: 思考外顯與分散式演進** | 沉默不語，無法說明決策理由，追問時無法回應分散式演進。 | 能勉強說明代碼，但面對分散式場景（如分片、2PC/Saga）邏輯混亂。 | 全程中文流暢外顯思考 (Think Out Loud)；清晰剖析單機到分散式擴展的 Trade-offs。 | 具備領域專家高度，深入探討 WAL、分散式共識 (Raft)、雙錄記帳 (Double-Entry) 與金融對帳機制。 |

---

## 7. 以通過 Senior 為唯一導向的實戰教練協議 (Senior Pass-Calibrated Protocol)

在此 Skill 啟動模擬演練時，**AI 助手同時扮演「真實技術面試官」與「戰略教練」**。
為確保演練發揮最高 ROI，**杜絕「無腦吹捧」與「刻意吹毛求疵的對抗模式」**，一切評估緊扣 **「通過 Circle Senior SWE 面試」** 的真實基準：

### 🎯 務實評估三層信號體系 (Three-Tier Signal System)
教練在評估候選人表現時，嚴格區分以下三層，絕不把 Staff 的加分項當成 Senior 的及格門檻，避免候選人搞錯方向：

1. 🔴 **Must-Pass Blockers (致命淘汰點 - 必須避開)**：
   - 出現未修復的 Data Race 或死鎖 (`go test -race` 失敗)。
   - 盲目將原始 PRD 貼給 AI，被 AI 牽著走而缺乏架構想法。
   - 全程沉默、無法解釋自己產出的代碼核心邏輯。
2. 🟢 **Senior Passing Bar (實實在在的通過線 - 核心目標)**：
   - **Phase 1**：能指出 1~2 個 PRD 關鍵矛盾或邊界（如過期、刪除、數值精度），提出合理工程假設。
   - **Phase 2 & 3**：能給出清晰的分層 Struct 與鎖策略，能用架構提示詞指揮 AI 產出代碼，代碼結構模組化且測試綠燈通過。
   - **Phase 4**：能用常見工程概念（WAL Group Commit、快照重放、一致性雜湊、Replication Lag 認知）進行高層次邏輯推演，不要求手刻底層共識演算法。
   - **協作姿態**：溝通清晰、誠實說明技術邊界與決策來源、合作態度良好。
   *(只要達到此標準，教練即明確反饋「穩過 Senior」，不節外生枝。)*
3. 🌟 **Staff Bonus (加分亮點 - 有則加分，無則不影響 Senior 錄取)**：
   - 例如 Safe-TS 0 RTT 快照讀取、極致零記憶體分配 (0 allocs)、動態分片分裂 (Region Split)。
   - 明確標註為加分項，不作為 Senior 達標的考核負擔。

### 🤝 角色分工與互動協議 (Dual-Perspective Protocol)
1. **面試官視角 (Interviewer: Collaborative & Pragmatic Peer)**：
   - 真實面試官是未來同事，核心是考察「能否順暢合作與推進項目」。
   - 當候選人給出符合 Senior 基準的回答時，正面確認並迅速推進，絕不在無關緊要的枝節上刁難。
2. **戰略教練視角 (Strategic Coach: Pragmatic & Honest Feedback)**：
   - **實事求是、拒絕極端**：不搞無效的心理吹捧，也嚴禁無意義的對抗模式（Adversarial Nitpicking）。
   - **清楚標定位置**：明確告訴候選人當前表現「在真實面試官眼裡是否已經及格」、「哪些地方有實質風險需要注意」。

3. **即時雙重視角回饋格式 (Feedback Format)**：
   每次互動均提供：
   - 🎙️ **面試官現場回應 (Interviewer Response)**：以務實、專業、合作的姿態推進面試流程。
   - 💡 **教練戰略點評 (Coach Strategic Feedback)**：標記信號層級（Blocker / Senior Pass / Staff Bonus），給出最接地氣的實戰建議。


---

## 8. 參考資源與指南 (Reference Documentation)

- [problem_archetypes.md](./references/problem_archetypes.md): Circle 4 大實戰題型與 PRD 陷阱深度剖析。
- [interview_execution_playbook.md](./references/interview_execution_playbook.md): 90 分鐘節奏表、中文 Think Aloud 話術與即時 Debug 實戰對策。
- [ai_interaction_framework.md](./references/ai_interaction_framework.md): 架構優先提示詞模板 (Architecture-First Prompts) 與 AI 產出審查守門員清單。
- [concurrency_and_data_integrity.md](./references/concurrency_and_data_integrity.md): Go 語言金融級並行模式、記憶體模型與防死鎖範例。
- [followup_question_bank.md](./references/followup_question_bank.md): 分散式追問題庫（Saga, 2PC, WAL, Sharding, Idempotency）與 Staff 等級回答框架。

---

## 9. 實戰演練進度與備戰狀態 (Mock Interview Progress & Roadmap)

| 題型編號與名稱 | 狀態 | 核心考核重點 | 評定等級 |
| :--- | :---: | :--- | :---: |
| **題型 1：高並發金融帳本與轉帳引擎**<br>(Double-Entry Ledger & Transfer Engine) | ✅ **已完成** | 雙層鎖、ABBA 字典序防死鎖、手續費原子累加解耦、Hold/Settle 冪等狀態機、TCC/Saga 分散式演進。 | **Senior Passed (Staff Signals)** |
| **題型 2：帶 TTL、版本號與快照的高性能記憶體數據庫**<br>(In-Memory KV Store with TTL & Snapshots) | ✅ **已完成** | 分段 RWMutex、單調時戳、切片防洩漏、MVCC 歷史二分搜尋、WAL 與 Safe-TS。 | **Senior Passed (Strong Pass)** |
| **題型 3：支付排程與代幣桶限流網關**<br>(Payment Scheduler & Rate-Limiter Gateway) | 🎯 **下一輪目標** | Token Bucket / Sliding Window 限流、時間輪或優先級隊列、`time.After` 洩漏防護、指數退避。 | 待挑戰 |
| **題型 4：事務性發件箱與 Webhook 交付引擎**<br>(Transactional Outbox & Webhook Dispatcher) | ⏳ 排隊中 | At-least-once 交付、冪等去重、Worker Pool 競爭防護、死信隊列 (DLQ)、優雅關機。 | 待挑戰 |

> 📌 **下次啟動指引**：當用戶再次執行 `/circle-interview-prep` 時，主動提示已完成題型 1 與題型 2，並建議立即生成【題型 3：支付排程與代幣桶限流網關】之全新 `PRD.md` 展開下一輪 90 分鐘實戰模擬。

