# Circle Technical Interview: Follow-Up Question Bank

在面試最後約 20 分鐘（70:00 – 90:00），面試官會以中文進行高維度的分散式系統架構追問，評估候選人能否將剛才實作的單機記憶體原型，演進為支撐百萬級 TPS、具備高可用與金融級強一致性的分散式生產平台。

---

## Category 1: Distributed Architecture & Scaling

### Q1: "Currently, our ledger runs in a single process memory. How do you scale this to handle 100,000 TPS across multiple nodes?"
- **Staff-Level Answer**:
  - **Account-Based Sharding (Partitioning)**: Partition accounts across nodes using consistent hashing on `AccountID` (or virtual shards). All operations for Account A route to Node A.
  - **Intra-Shard vs. Cross-Shard Transfers**:
    - *Intra-shard* (Account A and B on same node): Executes in-memory with local lock ordering (sub-millisecond latency).
    - *Cross-shard* (Account A on Node 1, Account B on Node 2): Use a **Two-Phase Commit (2PC)** or a **Saga pattern with compensating transactions**.
    - Alternative: Introduce a reservation step (`PENDING_DEBIT` on Node 1) followed by asynchronous credit on Node 2 via a reliable distributed log (Kafka/Pulsar).

### Q2: "How do you prevent distributed deadlocks when two cross-shard transfers occur concurrently (A->B and B->A)?"
- **Staff-Level Answer**:
  - Apply **global deterministic lock ordering** or **distributed lease acquisition** based on sorted Account IDs, OR
  - Decouple with an **Outbox + Event-Driven Orchestration**: Debit A and emit `TransferInitiatedEvent`. Node 2 consumes the event and credits B. If B fails permanently, emit `CompensationEvent` to refund A. No distributed lock required.

---

## Category 2: Persistence, Durability & Recovery

### Q3: "If the process crashes or the server loses power, all in-memory balances are lost. How do you ensure durability without killing write latency?"
- **Staff-Level Answer**:
  - **Write-Ahead Logging (WAL) + Periodic Snapshots** (similar to SQLite / Redis AOF / LMAX Disruptor):
    1. Append incoming intent to an append-only WAL on SSD using `O_DIRECT` or batched `fsync` (group commit).
    2. Apply mutation to in-memory state.
    3. Return success to client once WAL is synced.
    4. Periodically snapshot full ledger state to S3/cold storage and truncate old WAL segments.
  - **Recovery**: On reboot, restore from the latest snapshot, then replay uncompacted WAL entries in sequence.

### Q4: "How would you design the storage schema if we backed this with a relational database (e.g., PostgreSQL)?"
- **Staff-Level Answer**:
  - **Double-Entry Bookkeeping Model**: Never just store a mutable `balance` column. Store immutable journal entries:
    - `accounts` table: `id`, `created_at`, `status`.
    - `journal_entries` table: `id`, `transaction_id`, `account_id`, `entry_type` (DEBIT/CREDIT), `amount`, `created_at`.
  - Balance is derived as `SUM(credits) - SUM(debits)`.
  - For performance: Maintain a cached `account_balances` table updated via database transactions with `SELECT ... FOR UPDATE` (sorted by account ID to prevent DB deadlocks), or event-driven aggregation.

---

## Category 3: Idempotency & Reliability

### Q5: "Clients may experience network timeouts and retry requests. How do you guarantee idempotency?"
- **Staff-Level Answer**:
  - **Idempotency Key Store**:
    - Client sends an `Idempotency-Key` header with UUID.
    - Check an in-memory/Redis TTL cache or DB unique constraint on `(client_id, idempotency_key)`.
    - States: `IN_PROGRESS`, `COMPLETED`, `FAILED`.
    - If `IN_PROGRESS`: Reject with 409 Conflict or wait on a condition variable.
    - If `COMPLETED`: Return cached response immediately without re-executing state mutation.

---

## Category 4: Observability, Fraud & Auditability

### Q6: "How do we audit that the total USDC in the system never exceeds the minted amount, or detect internal anomalies?"
- **Staff-Level Answer**:
  - **Conservation of Value Invariant**: For every internal transfer, `sum(debits) == sum(credits)`.
  - **Merkle Tree Proofs / Cryptographic Hashing**: Hash transactions in sequential blocks.
  - **Continuous Reconciliation Worker**: Background goroutine takes periodic read-only snapshots and verifies:
    `TotalSystemBalance = InitialDepositTotal + Sum(Deposits) - Sum(Withdrawals)`. Any deviation raises an immediate P0 alert and freezes affected settlement pipelines.
