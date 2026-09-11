package main

// import (
// 	"fmt"
// 	"sort"
// )

// type OpType int

// const (
// 	SET OpType = iota
// 	DELETE
// 	CHECKPOINT
// )

// type SnapShot struct {
// 	Storage map[int64]*Database // int64 for checkpoint timestamp
// }

// type Database struct {
// 	Log       map[int64]*LogEntry
// 	DataSet   map[string]*Data
// 	Buff      *Buffer
// 	SerialNum int64
// }

// type Data struct {
// 	Key   string
// 	Value string
// }

// type LogEntry struct {
// 	LogID     int64
// 	Op        OpType
// 	Key       string
// 	Value     string
// 	Timestamp int64
// }

// type Buffer struct {
// 	SerialNum int64
// 	TempLog   []*LogEntry
// }

// func NewDatabase() *Database {
// 	return &Database{
// 		Log:       make(map[int64]*LogEntry),
// 		DataSet:   make(map[string]*Data),
// 		SerialNum: 0,
// 	}
// }

// func NewLogEntry(op OpType, key, value string, logID, timestamp int64) *LogEntry {
// 	return &LogEntry{
// 		LogID:     logID,
// 		Op:        op,
// 		Key:       key,
// 		Value:     value,
// 		Timestamp: timestamp,
// 	}
// }

// func NewData(key, value string) *Data {
// 	return &Data{
// 		Key:   key,
// 		Value: value,
// 	}
// }

// func NewBuffer(serialNum int64) *Buffer {
// 	return &Buffer{
// 		SerialNum: serialNum,
// 		TempLog:   make([]*LogEntry, 0),
// 	}
// }

// func (b *Buffer) getLogID() int64 {
// 	b.SerialNum++
// 	return b.SerialNum
// }

// func (d *Database) getLogID() int64 {
// 	d.SerialNum++
// 	return d.SerialNum
// }

// func (d *Database) isInTransaction() bool {
// 	return d.Buff != nil
// }

// func (d *Database) doSet(key, value string) {
// 	data := NewData(key, value)
// 	d.DataSet[key] = data
// }

// func (d *Database) doDelete(key string) {
// 	delete(d.DataSet, key)
// }

// func (d *Database) Set(key string, value string, timestamp int64) int64 {
// 	if key == "" {
// 		return 0
// 	}

// 	if d.isInTransaction() {
// 		tempLog := NewLogEntry(SET, key, value, 0, 0)
// 		d.Buff.TempLog = append(d.Buff.TempLog, tempLog)
// 		return -1
// 	} else {
// 		logID := d.getLogID()
// 		logEntry := NewLogEntry(SET, key, value, logID, timestamp)
// 		d.doSet(key, value)
// 		d.Log[logID] = logEntry
// 		return logID
// 	}
// }

// func (d *Database) Get(key string) (string, bool) {
// 	if d.isInTransaction() {
// 		n := len(d.Buff.TempLog)
// 		for i := n - 1; i >= 0; i-- {
// 			if d.Buff.TempLog[i].Key == key && d.Buff.TempLog[i].Op == SET {
// 				return d.Buff.TempLog[i].Value, true
// 			} else if d.Buff.TempLog[i].Key == key && d.Buff.TempLog[i].Op == DELETE {
// 				return "", false
// 			}
// 		}
// 		data := d.DataSet[key]
// 		if data == nil {
// 			return "", false
// 		} else {
// 			return data.Value, true
// 		}
// 	} else {
// 		data, ok := d.DataSet[key]
// 		if !ok {
// 			return "", false
// 		}
// 		return data.Value, true
// 	}
// }

// func (d *Database) Delete(key string, timestamp int64) (int64, bool) {
// 	_, ok := d.Get(key)
// 	if !ok {
// 		return 0, false
// 	}

// 	if d.isInTransaction() {
// 		tempLog := NewLogEntry(DELETE, key, "", 0, 0)
// 		d.Buff.TempLog = append(d.Buff.TempLog, tempLog)
// 		return -1, true
// 	} else {
// 		logID := d.getLogID()
// 		log := NewLogEntry(DELETE, key, "", logID, timestamp)
// 		d.Log[logID] = log
// 		d.doDelete(key)

// 		return logID, true
// 	}

// }

// func (d *Database) GetAtLog(key string, logID int64) (string, bool) {
// 	if logID <= 0 {
// 		return "", false
// 	}

// 	checkPointID := int64(-1)

// 	logList := make([]*LogEntry, 0)
// 	for k, v := range d.Log {
// 		if k <= logID && v.Key == key {
// 			logList = append(logList, v)
// 		}
// 		if v.Op == CHECKPOINT {
// 			checkPointID = max(checkPointID, k)
// 		}
// 	}
// 	sort.Slice(logList, func(i, j int) bool {
// 		return logList[i].LogID > logList[j].LogID
// 	})

// 	if len(logList) == 0 || logList[0].Op == DELETE {
// 		return "", false
// 	}

// 	if logList[0].LogID < checkPointID {
// 		return "", false
// 	}

// 	return logList[0].Value, true
// }

// func (d *Database) GetAtTimestamp(key string, timestamp int64) (string, bool) {
// 	logList := make([]*LogEntry, 0)
// 	for _, v := range d.Log {
// 		if v.Timestamp <= timestamp && v.Key == key {
// 			logList = append(logList, v)
// 		}
// 	}

// 	sort.Slice(logList, func(i, j int) bool {
// 		return logList[i].LogID > logList[j].LogID
// 	})

// 	if len(logList) == 0 || logList[0].Op == DELETE {
// 		return "", false
// 	}

// 	return logList[0].Value, true
// }

// func (d *Database) GetKeysModifiedBetween(startTime int64, endTime int64) []string {
// 	resList := make([]string, 0)
// 	if startTime > endTime {
// 		return resList
// 	}

// 	checkpointID := int64(-1)
// 	for _, v := range d.Log {
// 		if v.Op == CHECKPOINT {
// 			checkpointID = max(checkpointID, v.LogID)
// 		}
// 	}

// 	mp := make(map[string]bool)
// 	for _, v := range d.Log {
// 		if v.Timestamp >= startTime && v.Timestamp <= endTime && !mp[v.Key] && v.LogID > checkpointID {
// 			mp[v.Key] = true
// 			resList = append(resList, v.Key)
// 		}
// 	}

// 	sort.Strings(resList)

// 	return resList
// }

// func (d *Database) BeginTransaction() bool {
// 	if d.isInTransaction() {
// 		return false
// 	}

// 	buff := NewBuffer(d.SerialNum)
// 	d.Buff = buff
// 	return true
// }

// func (d *Database) Commit(timestamp int64) (int, bool) {
// 	if !d.isInTransaction() {
// 		return 0, false
// 	}

// 	done := 0
// 	for _, l := range d.Buff.TempLog {
// 		logId := d.Buff.getLogID()
// 		log := NewLogEntry(l.Op, l.Key, l.Value, logId, timestamp)
// 		d.Log[logId] = log
// 		switch log.Op {
// 		case SET:
// 			d.doSet(log.Key, log.Value)
// 		case DELETE:
// 			d.doDelete(log.Key)
// 		}
// 		d.SerialNum = logId
// 		done++
// 	}

// 	d.Buff = nil
// 	return done, true
// }

// func (d *Database) Rollback() bool {
// 	if !d.isInTransaction() {
// 		return false
// 	}
// 	d.Buff = nil
// 	return true
// }

// func (d *Database) Checkpoint(timestamp int64) int64 {
// 	if d.isInTransaction() {
// 		return 0
// 	}

// 	logID := d.getLogID()
// 	checkpointLog := NewLogEntry(CHECKPOINT, "", "", logID, timestamp)
// 	d.Log[logID] = checkpointLog
// 	return logID
// }

// func (d *Database) CompactLogs(checkpointLogID int64) (int, bool) {
// 	checkPointLog, ok := d.Log[checkpointLogID]
// 	if !ok || checkPointLog.Op != CHECKPOINT {
// 		return 0, false
// 	}

// 	logList := make([]*LogEntry, 0)
// 	for _, v := range d.Log {
// 		logList = append(logList, v)
// 	}

// 	sort.Slice(logList, func(i, j int) bool {
// 		return logList[i].LogID > logList[j].LogID
// 	})

// 	res := 0
// 	for i := 0; i < len(logList); i++ {
// 		if logList[i].LogID == checkpointLogID {
// 			res = len(logList) - i - 1
// 			logList = logList[:i+1]
// 			break
// 		}
// 	}

// 	newLog := make(map[int64]*LogEntry)
// 	for _, p := range logList {
// 		newLog[p.LogID] = p
// 	}
// 	d.Log = newLog

// 	return res, true
// }

// func max(i, j int64) int64 {
// 	if i > j {
// 		return i
// 	}
// 	return j
// }

// func runLevel1Tests() {
// 	db := NewDatabase()

// 	// Test 1: Empty Get
// 	val, ok := db.Get("user_1")
// 	assert(!ok && val == "", "Test 1.1 Failed: Initial Get should return false")

// 	// Test 2: Set with valid key
// 	log1 := db.Set("user_1", "Alice", 1000)
// 	assert(log1 == 1, "Test 2.1 Failed: First logID should be 1")
// 	val, ok = db.Get("user_1")
// 	assert(ok && val == "Alice", "Test 2.2 Failed: Get user_1 should return Alice")

// 	// Test 3: Set with invalid key
// 	log0 := db.Set("", "Invalid", 1001)
// 	assert(log0 == 0, "Test 3.1 Failed: Empty key should return 0")

// 	// Test 4: Update existing key
// 	log2 := db.Set("user_1", "Alice_V2", 1002)
// 	assert(log2 == 2, "Test 4.1 Failed: Second logID should be 2")
// 	val, _ = db.Get("user_1")
// 	assert(val == "Alice_V2", "Test 4.2 Failed: Value should be updated")

// 	// Test 5: Delete non-existing key
// 	dLog0, dOk := db.Delete("user_unknown", 1003)
// 	assert(!dOk && dLog0 == 0, "Test 5.1 Failed: Delete unknown should fail and logID=0")

// 	// Test 6: Delete existing key
// 	dLog3, dOk := db.Delete("user_1", 1004)
// 	assert(dOk && dLog3 == 3, "Test 6.1 Failed: Delete should succeed with logID 3")
// 	val, ok = db.Get("user_1")
// 	assert(!ok && val == "", "Test 6.2 Failed: Deleted key should not exist")

// 	// Test 7: Re-delete already deleted key
// 	dLog4, dOk := db.Delete("user_1", 1005)
// 	assert(!dOk && dLog4 == 0, "Test 7.1 Failed: Re-deleting should fail")

// 	// Test 8: Set again after delete
// 	log4 := db.Set("user_1", "Alice_V3", 1006)
// 	assert(log4 == 4, "Test 8.1 Failed: LogID increments monotonically")
// 	val, ok = db.Get("user_1")
// 	assert(ok && val == "Alice_V3", "Test 8.2 Failed: Re-set value")

// 	fmt.Println(">>> Level 1 Tests Passed! <<<")
// }

// func runLevel2Tests() {
// 	db := NewDatabase()

// 	// 演進操作：
// 	// log1: t=1000, SET k1 = "v1"
// 	// log2: t=2000, SET k2 = "v2"
// 	// log3: t=3000, SET k1 = "v1_updated"
// 	// log4: t=4000, DELETE k2
// 	// log5: t=5000, SET k3 = "v3"
// 	l1 := db.Set("k1", "v1", 1000)
// 	l2 := db.Set("k2", "v2", 2000)
// 	l3 := db.Set("k1", "v1_updated", 3000)
// 	l4, _ := db.Delete("k2", 4000)
// 	l5 := db.Set("k3", "v3", 5000)

// 	assert(l1 == 1 && l2 == 2 && l3 == 3 && l4 == 4 && l5 == 5, "Log IDs should be sequential 1..5")

// 	// Test 1: GetAtLog
// 	// k1 at log 1 -> v1
// 	v, ok := db.GetAtLog("k1", 1)
// 	assert(ok && v == "v1", "Test 1.1 Failed: k1 at log 1 should be v1")

// 	// k1 at log 2 -> still v1
// 	v, ok = db.GetAtLog("k1", 2)
// 	assert(ok && v == "v1", "Test 1.2 Failed: k1 at log 2 should still be v1")

// 	// k1 at log 3 -> v1_updated
// 	v, ok = db.GetAtLog("k1", 3)
// 	assert(ok && v == "v1_updated", "Test 1.3 Failed: k1 at log 3")

// 	// k2 at log 3 -> v2
// 	v, ok = db.GetAtLog("k2", 3)
// 	assert(ok && v == "v2", "Test 1.4 Failed: k2 at log 3")

// 	// k2 at log 4 -> deleted
// 	v, ok = db.GetAtLog("k2", 4)
// 	assert(!ok && v == "", "Test 1.5 Failed: k2 at log 4 should be deleted")

// 	// k3 at log 4 -> not yet created
// 	v, ok = db.GetAtLog("k3", 4)
// 	assert(!ok && v == "", "Test 1.6 Failed: k3 not yet created at log 4")

// 	// Invalid logID
// 	v, ok = db.GetAtLog("k1", 0)
// 	assert(!ok && v == "", "Test 1.7 Failed: logID <= 0 should return false")

// 	// Test 2: GetAtTimestamp
// 	// k1 at t=1500 -> v1
// 	v, ok = db.GetAtTimestamp("k1", 1500)
// 	assert(ok && v == "v1", "Test 2.1 Failed: k1 at t=1500")

// 	// k1 at t=999 -> not exist
// 	v, ok = db.GetAtTimestamp("k1", 999)
// 	assert(!ok && v == "", "Test 2.2 Failed: k1 at t=999")

// 	// k2 at t=4000 -> deleted
// 	v, ok = db.GetAtTimestamp("k2", 4000)
// 	assert(!ok && v == "", "Test 2.3 Failed: k2 at t=4000")

// 	// k2 at t=3999 -> v2
// 	v, ok = db.GetAtTimestamp("k2", 3999)
// 	assert(ok && v == "v2", "Test 2.4 Failed: k2 at t=3999")

// 	// Test 3: GetKeysModifiedBetween
// 	// Between 1500 and 3500: modified are k2 (2000), k1 (3000) -> sorted ["k1", "k2"]
// 	keys := db.GetKeysModifiedBetween(1500, 3500)
// 	assert(len(keys) == 2, "Test 3.1 Failed: count of modified keys")
// 	assert(keys[0] == "k1" && keys[1] == "k2", "Test 3.2 Failed: sorted keys")

// 	// Between 1000 and 5000: k1, k2, k3 (k1 and k2 modified multiple times, must be unique)
// 	keysAll := db.GetKeysModifiedBetween(1000, 5000)
// 	assert(len(keysAll) == 3, "Test 3.3 Failed: duplicate keys deduplicated")
// 	assert(keysAll[0] == "k1" && keysAll[1] == "k2" && keysAll[2] == "k3", "Test 3.4 Failed: sorted all")

// 	// Invalid range
// 	keysEmpty := db.GetKeysModifiedBetween(5000, 1000)
// 	assert(len(keysEmpty) == 0, "Test 3.5 Failed: start > end should return empty")

// 	fmt.Println(">>> Level 2 Tests Passed! <<<")
// }

// func runLevel3Tests() {
// 	db := NewDatabase()

// 	// 初始資料
// 	db.Set("k1", "v1", 1000) // log 1
// 	db.Set("k2", "v2", 1000) // log 2

// 	// Test 1: Begin Transaction
// 	assert(db.BeginTransaction() == true, "Test 1.1 Failed: Begin tx")
// 	assert(db.BeginTransaction() == false, "Test 1.2 Failed: Nested tx should fail")

// 	// Test 2: Operations inside transaction (Read-your-own-writes)
// 	logID := db.Set("k1", "v1_tx", 2000)
// 	assert(logID == -1, "Test 2.1 Failed: Uncommitted Set should return logID -1")

// 	// Read inside tx should see uncommitted value
// 	v, ok := db.Get("k1")
// 	assert(ok && v == "v1_tx", "Test 2.2 Failed: Read-your-own-writes for k1")

// 	// Delete inside tx
// 	delLogID, delOk := db.Delete("k2", 2000)
// 	assert(delOk && delLogID == -1, "Test 2.3 Failed: Uncommitted Delete should return logID -1")
// 	_, ok = db.Get("k2")
// 	assert(!ok, "Test 2.4 Failed: Deleted k2 inside tx should not be visible")

// 	// Add new key inside tx
// 	db.Set("k3", "v3_tx", 2000)

// 	// Test 3: Rollback
// 	assert(db.Rollback() == true, "Test 3.1 Failed: Rollback should succeed")
// 	assert(db.Rollback() == false, "Test 3.2 Failed: Rollback with no active tx should fail")

// 	// Verify state after Rollback (k1 remains v1, k2 remains v2, k3 does not exist)
// 	v, ok = db.Get("k1")
// 	assert(ok && v == "v1", "Test 3.3 Failed: k1 reverted to v1")
// 	v, ok = db.Get("k2")
// 	assert(ok && v == "v2", "Test 3.4 Failed: k2 reverted to v2")
// 	_, ok = db.Get("k3")
// 	assert(!ok, "Test 3.5 Failed: k3 was rolled back")

// 	// Test 4: Commit Transaction
// 	assert(db.BeginTransaction() == true, "Test 4.1 Failed: Begin second tx")
// 	db.Set("k1", "v1_committed", 3000)
// 	db.Delete("k2", 3000)
// 	db.Set("k4", "v4_new", 3000)

// 	committedCount, commitOk := db.Commit(3500)
// 	assert(commitOk && committedCount == 3, "Test 4.2 Failed: Commit 3 operations")

// 	// 修正後的 Test 4.3：正常接雙回傳值驗證
// 	noTxCount, noTxOk := db.Commit(3500)
// 	assert(!noTxOk && noTxCount == 0, "Test 4.3 Failed: Commit with no active tx should return (0, false)")

// 	// Verify state after Commit
// 	v, _ = db.Get("k1")
// 	assert(v == "v1_committed", "Test 4.4 Failed: k1 committed value")
// 	_, ok = db.Get("k2")
// 	assert(!ok, "Test 4.5 Failed: k2 successfully deleted")
// 	v, _ = db.Get("k4")
// 	assert(v == "v4_new", "Test 4.6 Failed: k4 committed")

// 	// Verify WAL Logs generated by Commit
// 	// Before commit we had log 1 and log 2. Commit should have added logs 3, 4, 5
// 	v, ok = db.GetAtLog("k1", 3)
// 	assert(ok && v == "v1_committed", "Test 4.7 Failed: Log 3 is k1 update")
// 	v, ok = db.GetAtLog("k2", 4)
// 	assert(!ok, "Test 4.8 Failed: Log 4 is k2 delete")
// 	v, ok = db.GetAtLog("k4", 5)
// 	assert(ok && v == "v4_new", "Test 4.9 Failed: Log 5 is k4 create")

// 	fmt.Println(">>> Level 3 Tests Passed! <<<")
// }

// func runLevel4Tests() {
// 	db := NewDatabase()

// 	// Step 1: 基礎數據寫入
// 	// log 1: t=1000, SET k1 = "v1"
// 	// log 2: t=1000, SET k2 = "v2"
// 	// log 3: t=2000, SET k1 = "v1_mod"
// 	// log 4: t=2000, DELETE k2
// 	db.Set("k1", "v1", 1000)
// 	db.Set("k2", "v2", 1000)
// 	db.Set("k1", "v1_mod", 2000)
// 	db.Delete("k2", 2000)

// 	// Test 1: Checkpoint 失敗情境 (交易進行中不能 checkpoint)
// 	db.BeginTransaction()
// 	db.Set("tx_k", "tx_v", 2500)
// 	assert(db.Checkpoint(2600) == 0, "Test 1.1 Failed: Checkpoint during transaction must fail")
// 	db.Rollback()

// 	// Test 2: 建立合法的 Checkpoint (應分配 logID = 5)
// 	cpLogID := db.Checkpoint(3000)
// 	assert(cpLogID == 5, "Test 2.1 Failed: Checkpoint logID should be 5")

// 	// 檢查點之後的新操作 (logID = 6, 7)
// 	db.Set("k3", "v3", 4000)
// 	db.Set("k1", "v1_final", 5000)

// 	// Test 3: CompactLogs 失敗驗證
// 	// 非法的 logID
// 	cCount, cOk := db.CompactLogs(999)
// 	assert(!cOk && cCount == 0, "Test 3.1 Failed: Non-existent checkpoint logID")
// 	// 存在的 logID 但不是 CHECKPOINT 類型 (log 3 是 SET)
// 	cCount, cOk = db.CompactLogs(3)
// 	assert(!cOk && cCount == 0, "Test 3.2 Failed: Target logID is not a CHECKPOINT")

// 	// Test 4: 執行合法的日誌壓縮
// 	// 清理 logID < 5 的日誌 (應刪除 log 1, 2, 3, 4 共 4 筆)
// 	cCount, cOk = db.CompactLogs(cpLogID)
// 	assert(cOk && cCount == 4, "Test 4.1 Failed: Should compact exactly 4 old logs")

// 	// 再次 compact 相同 checkpoint (此時小於 5 的已經沒了，應刪除 0 筆但回傳 true)
// 	cCount, cOk = db.CompactLogs(cpLogID)
// 	assert(cOk && cCount == 0, "Test 4.2 Failed: Re-compact should succeed with 0 deleted")

// 	// Test 5: 日誌查詢與歷史向後相容
// 	// 5.1 查詢已被刪除的歷史日誌點 -> 應回傳 false
// 	_, ok := db.GetAtLog("k1", 1)
// 	assert(!ok, "Test 5.1 Failed: Compacted log 1 should not be accessible")
// 	_, ok = db.GetAtLog("k1", 3)
// 	assert(!ok, "Test 5.2 Failed: Compacted log 3 should not be accessible")

// 	// 5.2 查詢未被刪除的點
// 	// log 6 當時 k3 應為 "v3"
// 	v, ok := db.GetAtLog("k3", 6)
// 	assert(ok && v == "v3", "Test 5.3 Failed: Active log 6 should be accessible")

// 	// log 6 當時 k1 還沒被更新成 v1_final (由 checkpoint 帶過來的狀態或最新歷史)
// 	v, ok = db.GetAtLog("k1", 7)
// 	assert(ok && v == "v1_final", "Test 5.4 Failed: Active log 7 should be accessible")

// 	// Test 6: 查詢區間鍵（已被 compact 掉的 log 不會出現在統計中）
// 	// 目前殘留的 log: log 5 (CHECKPOINT), log 6 (k3, t=4000), log 7 (k1, t=5000)
// 	keys := db.GetKeysModifiedBetween(1000, 6000)
// 	assert(len(keys) == 2, "Test 6.1 Failed: Only active logs considered")
// 	assert(keys[0] == "k1" && keys[1] == "k3", "Test 6.2 Failed: Keys should be k1, k3")

// 	fmt.Println(">>> Level 4 Tests Passed! <<<")
// }

// func assert(cond bool, msg string) {
// 	if !cond {
// 		panic(msg)
// 	}
// }

// func main() {
// 	runLevel1Tests()
// 	runLevel2Tests()
// 	runLevel3Tests()
// 	runLevel4Tests()
// }
