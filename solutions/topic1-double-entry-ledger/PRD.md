# PRD: High-Concurrency Multi-Account Transfer & Escrow Engine

## 1. 系統背景與目標 (Background & Objective)
Circle 正在擴建新一代微結算錢包引擎。我們需要一個高效能、執行於記憶體中 (In-Memory) 的帳本核心服務，供內部多個支付網關高頻呼叫，處理大量並發的帳戶餘額查詢、點對點資金轉帳，以及第三方商戶交易常用的保證金預扣 (Escrow / Hold) 機制。

---

## 2. 核心功能規格 (Functional Requirements)

### 2.1 帳戶建立與餘額管理 (Account Management)
- **`CreateAccount(accountID string, initialBalance int64) error`**
  - 帳戶 ID 具備唯一性。
  - 初始餘額不得為負數。
- **`GetBalance(accountID string) (int64, error)`**
  - 支援高頻查詢帳戶的即時可用餘額。
  - 若帳戶不存在需回傳明確錯誤。

### 2.2 點對點資金轉帳 (P2P Transfer)
- **`Transfer(fromAccountID, toAccountID string, amount int64, fee int64) (*TransferReceipt, error)`**
  - 扣除轉出方資金，並增加接收方資金。
  - 系統每筆轉帳可指定 `fee`（手續費）。
  - 需確保高並發轉帳下的資料一致性與原子性（Check-then-Act），餘額不足時拒絕轉帳。
  - 回傳轉帳憑證 `TransferReceipt`，包含交易流水號、時間戳、雙方 ID、轉帳額度與手續費。

### 2.3 資金預扣與結算 (Hold / Escrow & Settle Workflow)
- **`Hold(holdID string, accountID string, amount int64) error`**
  - 暫時凍結特定帳戶的可用餘額（凍結後可用餘額減少，但總資產尚未移轉）。
  - 同一 `holdID` 必須具備唯一性。
- **`SettleHold(holdID string, toAccountID string) error`**
  - 將先前凍結的款項正式結算並轉移至目標帳戶 `toAccountID`。
  - 結算後該筆 Hold 狀態標記為已結算 (`SETTLED`)。
- **`ReleaseHold(holdID string) error`**
  - 若交易取消或逾期，解凍款項並全額歸還至原帳戶可用餘額。
  - 解凍後該筆 Hold 狀態標記為已釋放 (`RELEASED`)。

### 2.4 交易審計日誌 (Audit History)
- **`GetTransactionHistory(accountID string) ([]TransactionRecord, error)`**
  - 查詢該帳戶所有歷史操作紀錄（包含轉帳、被轉入、凍結、解凍與結算明細）。

---

## 3. 非功能性要求 (Non-Functional Requirements)

1. **線程安全與並發防禦 (Thread Safety & Concurrency)**
   - 必須通過 `go test -v -race ./...` 驗證。
   - 在數百個並發 Goroutine 同時進行跨帳戶轉帳、凍結與查詢時，絕對不可出現 Data Race 或 Deadlock。
2. **資金守恆與數值完整性 (Conservation of Money & Integrity)**
   - 絕對禁止資金憑空產生或消失。
   - 防禦整數溢位與無效參數。
3. **低鎖延遲 (Low Contention)**
   - 讀取餘額或審計歷史時，不可長時間阻塞其他帳戶的正常轉帳與寫入操作。

---

## 4. 候選人澄清與情境假設記錄區 (Clarifications & Assumptions Log)

### 4.1 手續費機制與資金守恆 (已於 Phase 1 對齊)
1. **扣款語意 (外加制)**：
   - 手續費由**轉出方 (fromAccountID)** 負擔。
   - 轉出方扣款總額為 `amount + fee`；接收方實收 `amount`。
   - 轉出方可用餘額必須滿足 `balance >= amount + fee`，若不足則回傳 `ErrInsufficientBalance`。
2. **手續費入帳帳戶 (System Fee Collector)**：
   - 遵循金融資金守恆定律（Conservation of Money），手續費不可憑空消失。
   - 系統內部設有專門的系統保留帳戶（預設 ID：`system:fee_collector`）。
   - 每筆交易成功時，`fee` 會自動劃撥至該系統帳戶，維持整體系統資金恆等式：`Sum(User Balances) + System Fee Balance = Initial Total Supply`。
   - 手續費必須 `>= 0`，若傳入負數手續費應回傳 `ErrInvalidAmount`。

### 4.2 帳戶安全性與邊界防禦 (已於 Phase 1 對齊)
1. **禁止自己轉給自己 (Self-Transfer Guard)**：
   - 若 `fromAccountID == toAccountID`，直接透過 Guard Clause 攔截並回傳 `ErrSelfTransfer` (Status 400)。
   - *技術價值*：除商業邏輯不合例外，同時從根本杜絕 Go `sync.Mutex` 不可重入所造成的自我死鎖 (Self-Deadlock)。
2. **系統保留帳戶隔離性與安全性 (System Account Security Isolation)**：
   - 系統保留帳戶（如 `system:fee_collector`）不對外公開暴露。
   - `CreateAccount`：若外部使用者嘗試建立系統保留 ID，直接回傳帳戶已存在/已被佔用錯誤 (Status 400)，避免洩露內部架構細節。
   - `Transfer`：禁止外部發起包含系統保留 ID 的點對點轉帳（`from` 或 `to` 皆不可為系統帳戶），若傳入則回傳帳戶不存在錯誤 (Status 400)，杜絕外部盜領或偽造系統手續費金流。

### 4.3 資金預扣 (Hold) 與保證金狀態機 (已於 Phase 1 對齊)
1. **雙重餘額模型 (Two-Balance Model)**：
   - 帳戶維護 `balance`（總餘額）與 `locked`（凍結金額）。
   - `available = balance - locked`（即時可用餘額）。
   - 一般轉帳只能動用 `available` 餘額。
2. **Hold 狀態機與冪等性 (Idempotency)**：
   - 以 `holdID` 作為天然的冪等去重鍵 (Idempotency Key)，系統全域維護 `map[string]*HoldRecord`。
   - 狀態流轉：`PENDING` -> `SETTLED`（不可逆）或 `RELEASED`（不可逆）。
   - `Hold(holdID, accountID, amount)`：
     - 若 `holdID` 已存在且狀態為 `PENDING`，視為重試冪等；若參數不合或已結算則回傳錯誤。
     - 驗證 `account.available >= amount` 後，`account.locked += amount`。
   - `SettleHold(holdID, toAccountID)`：
     - 需同時取得原帳戶與目標帳戶之鎖。
     - 原帳戶：`balance -= amount`, `locked -= amount`。
     - 目標帳戶：`balance += amount`。
     - 標記狀態為 `SETTLED`。
   - `ReleaseHold(holdID)`：
     - 原帳戶：`locked -= amount`，全額退回至可用餘額。
     - 標記狀態為 `RELEASED`。

### 4.4 資金預扣 (Hold & Settle) 之手續費規則 (已於 Phase 1/Phase 3 對齊)
1. **零手續費機制 (Zero-Fee Escrow)**：
   - 目前 MVP 規格中，`Hold`、`SettleHold` 與 `ReleaseHold` 函式簽名皆無 `fee` 參數。
   - 預扣與結算屬於單純保證金授權與撥付（Authorization & Capture）流程，手續費預設為 0。
   - `Hold` 凍結多少金額，`SettleHold` 時目標帳戶就實收多少金額，`ReleaseHold` 時全額退回，不扣取手續費。
   - 僅點對點 `Transfer` 支援並扣除額外手續費。






