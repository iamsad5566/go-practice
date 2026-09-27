# 💡 Circle Senior/Staff SWE 面試實戰核心精髓與覆盤總結 (TAKEAWAY.md)

本文件整理自「題型 5：多幣別即時換匯與跨國出金引導引擎」的高強度擬真對話演練，提煉出**通過 Circle Senior SWE 面試的關鍵勝負手、AI 指揮協議、守門員防禦陷阱與分散式高階追問標準答案**。

---

## 🧭 一、 核心心法突破：為什麼「最小單元代碼」是面試的唯一生路？

### 1. 致命陷阱：叫 AI 一次產生全部代碼 (The 300-Line Code Dump)
在前幾題的演練中，若 Prompt 下得太廣或直接把整篇 PRD 丟給 AI，AI 會一口氣吐出 200~300 行代碼：
- **認知超載 (Cognitive Overload)**：在 90 分鐘緊張高壓的面試下，候選人根本無法在短時間內逐行 Review 300 行程式碼。
- **面試官的大忌 (Hiring Committee Red Flag)**：候選人看著一坨龐大代碼只能默默直接跑 `go test`。當面試官問起：「為什麼這裡要加讀鎖？」候選人答不出來。評審會直接判定：**「這只是個被 AI 牽著走的 Prompt 操作員，不是能獨當一面的 Tech Lead」**。

### 2. 破局之道：最小單元代碼階梯式產出 (Minimal Unit Protocol)
面試的核心是**展示你對架構的 100% 主導權（Architectural Sovereignty）**。你必須把實作拆成 3 個階梯，每次只讓 AI 產出 30~50 行代碼：

```text
[Step 1: 定義契約與模型 (約 30 行)] ──► [Step 2: 核心並發與狀態機 (分段 40 行)] ──► [Step 3: 測試與驗證]
  • Account / Quote Struct                • 階段一：前置驗證與凍結 (Hold)            • 正常轉帳測試
  • Interface & 錯誤枚舉                   • 階段二：無鎖呼叫外部 Settle               • 併發競爭測試
  • 15 秒肉眼掃描指標接收者               • 階段三：排序鎖收尾與回滾                  • go test -race 綠燈
```

> **核心效益**：
> 1. **骨架全由你預先規劃**：AI 產出的每一行代碼本來就在你的預期之中，你絕對不可能看不懂。
> 2. **Review 速度極快**：一段只有 40 行，花 15 秒就能精準挑出並發錯誤，當場面試官展現 Senior 的 Code Review 實力！

---

## 🛡️ 二、 守門員審查（Gatekeeper）四大經典致命陷阱

在今天的演練中，我們精準抓出了 4 個金融並行系統的「死刑級」缺陷：

### 陷阱 1：結構體值接收者複製 Mutex（Copylock Violation）
- **現象**：AI 寫出 `func (a Account) Deduct(...)` 而非 `func (a *Account)`。
- **後果**：每次呼叫都複製了一份 Struct（包含內部的 `sync.Mutex`）。各協程鎖在各自的副本上，互斥保護完全失效；且數值修改不會反映到原始物件上。
- **防禦**：只要 Struct 內含有 `sync.Mutex` 或 `sync.RWMutex`，所有方法一律強制使用**指標接收者（Pointer Receiver）**。

### 陷阱 2：過早提交與缺少狀態回滾（Premature Commit & Missing Rollback）
- **現象**：在 `prepareHold` 中，還沒檢查帳戶餘額是否足夠，就先將 `quote.IsConsumed` 設為 `true`。
- **後果**：若用戶餘額不足或幣別錯誤，該筆合法報價單已被永久廢棄，商戶儲值後無法重試下單。
- **防禦**：貫徹 **Check-then-Act** 原則——所有前置驗證通過且成功凍結資金後，才原子更新狀態；若中途出錯，必須有對應的狀態還原（Rollback）。

### 陷阱 3：跨帳戶轉帳的 ABBA 死鎖（Cyclic Deadlock）
- **現象**：轉帳收尾時，同時鎖住 `fromAcc.mu.Lock()` 與 `toAcc.mu.Lock()`。
- **後果**：當協程 1 執行 `A -> B` 拿著 A 鎖等 B，協程 2 執行 `B -> A` 拿著 B 鎖等 A，引發循環等待死鎖，整個服務掛死。
- **防禦**：**確定性鎖排序（Deterministic Lock Ordering）**。跨帳戶加鎖前，比對 Account ID 字典序（`if idA < idB`），永遠按固定順序拿鎖，依相反順序釋放。

### 陷阱 4：外部 I/O 阻塞臨界區（Holding Lock Across Network I/O）
- **現象**：在持有 `Account.mu` 或 `Engine.mu` 的情況下呼叫 `ClearingChannel.Settle(ctx, ...)`。
- **後果**：外部銀行網路延遲 500ms，全系統所有該帳戶的後續操作全部排隊阻塞，引發連鎖雪崩。
- **防禦**：**凍結完成後立即釋放鎖**，外部 I/O 全程在無鎖環境下執行，呼叫完成後再重新獲取鎖進行狀態結算。

---

## 🌊 三、 Phase 4 分散式架構追問：標準滿分應對模板

### Q1: 呼叫外部銀行 Settle 遇到 Network Timeout / HTTP 504 時，為什麼不能直接 Rollback 退款？

* **❌ 菜鳥回答**：「超時就是失敗，直接回滾釋放凍結資金，退回給用戶。」
* **💥 災難後果**：
  跨國銀行伺服器其實**已經成功扣款打給收款人**，只是回傳 HTTP 200 時網路斷線。如果系統認定超時為失敗並將錢退給付款人，將導致**「收款人收到錢，付款人也拿回錢」，公司面臨雙倍資金損失（Double Loss 倒貼）**！
* **🟢 Senior / Staff 滿分回答（三步架構法）**：
  1. **定性本質**：在金融跨境清算中，網路超時是**三態邏輯中的「未知狀態 (In-Doubt)」**，絕非失敗。
  2. **維持凍結**：遇到超時時，交易標記為 `SETTLEMENT_IN_DOUBT`，資金**繼續維持在 `LockedBalance`**，絕不擅自釋放，防止用戶提現落跑。
  3. **非同步反查對帳 (Reconciliation Polling)**：由獨立的對帳 Worker 帶著唯一的 `QuoteID`（作為 Idempotency-Key），主動向銀行的 Status API 輪詢；或比對次日的銀行對帳單（Reconciliation Statement），確定銀行確實沒入帳才解凍，確定銀行已入帳則正式結算扣款。

---

### Q2: 系統擴展到多個 Pod 時，如何防止同一帳戶被並發超額扣款？

* **❌ 扣分回答**：「一上來就導入 Redis 分散式鎖，或者手寫 Raft 共識演算法。」（過度設計，徒增運維故障點）
* **🟢 Senior / Staff 滿分回答（KISS 階梯式架構推演）**：
  1. **第一階段（KISS 原則首選）——關聯式資料庫 ACID 本地事務**：
     - 在一般生產規模下，將帳戶存於 PostgreSQL/MySQL。
     - 利用行級排他鎖（`SELECT ... FOR UPDATE`）或樂觀條件更新（`UPDATE accounts SET available = available - $amt WHERE id = $id AND available >= $amt`）。
     - 單庫事務天然保證線性一致性，無任何分散式鎖的腦裂或逾時維護成本。
  2. **第二階段（超大規模擴展）——依 Account ID 一致性雜湊分片 (Sharding)**：
     - 當寫入 QPS 超出單一資料庫極限時，採用一致性雜湊（Consistent Hashing）依 `AccountID` 分片。
     - 同一帳戶的所有交易始終由固定的資料庫分片或 Partition Worker 處理，將跨節點的並發競爭降維成單節點內部的安全有序處理。

---

## 📋 四、 實戰口訣卡 (臨場反射)

```text
【架構主權】
不讓 AI 做 Planning，契約先行三十行。分段交付鎖邊界，每段審查無遁形。

【並發防線】
鎖內不做網路網，指標接收防 Copylock。雙帳轉帳排 ID，ABBA 死鎖立馬平。

【金融三態】
成功失敗與未知，超時退款公司死。鎖定資金待反查，對帳確認才結清。

【分散擴展】
首選 DB 行級鎖，KISS 原則面試過。流量爆棚再分片，一致雜湊定乾坤。
```
