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
*(待 Phase 1 討論後將共識與技術假設記錄於此)*
