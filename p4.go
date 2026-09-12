package main

import (
	"fmt"
	"sort"
)

type DocStore struct {
	docs map[string]*Document
}

type Document struct {
	id     string
	fields map[string]string
}

func NewDocStore() *DocStore {
	return &DocStore{
		docs: make(map[string]*Document),
	}
}

func NewDocument(id string, fields map[string]string) *Document {
	return &Document{
		id:     id,
		fields: fields,
	}
}

func (d *Document) getID() string {
	return d.id
}

func (d *Document) getFields() map[string]string {
	return d.fields
}

func (d *Document) getFieldValueByID(fID string) (string, bool) {
	f, ok := d.fields[fID]
	return f, ok
}

func (d *Document) setFields(fields map[string]string) {
	d.fields = fields
}

func (d *Document) appendFields(fields map[string]string) {
	for k, v := range fields {
		if v != "" {
			d.fields[k] = v
		} else {
			delete(d.fields, k)
		}
	}
}

func (d *DocStore) getDoc(id string) (*Document, bool) {
	doc, ok := d.docs[id]
	return doc, ok
}

func (d *DocStore) setDoc(doc *Document) {
	d.docs[doc.getID()] = doc
}

func (d *DocStore) Insert(id string, fields map[string]string) bool {
	if id == "" || fields == nil {
		return false
	}

	if _, ok := d.docs[id]; ok {
		return false
	}

	doc := NewDocument(id, fields)
	d.setDoc(doc)
	return true
}

func (d *DocStore) Get(id string) (map[string]string, bool) {
	if id == "" {
		return nil, false
	}

	doc, ok := d.getDoc(id)
	if !ok {
		return nil, false
	}

	return doc.getFields(), true
}

func (d *DocStore) Delete(id string) bool {
	if _, ok := d.Get(id); !ok {
		return false
	}

	delete(d.docs, id)
	return true
}

func (d *DocStore) Count() int {
	return len(d.docs)
}

func (d *DocStore) Update(id string, fields map[string]string) bool {
	if id == "" || len(fields) == 0 {
		return false
	}

	doc, ok := d.getDoc(id)
	if !ok {
		return false
	}

	doc.appendFields(fields)
	return true
}

func (d *DocStore) FindByField(field string, value string) []string {
	list := make([]string, 0)

	if field == "" {
		return list
	}

	for k, v := range d.docs {
		dv, ok := v.getFieldValueByID(field)
		if ok && dv == value {
			list = append(list, k)
		}
	}

	sort.Strings(list)
	return list
}

func (d *DocStore) CountDistinctValues(field string) int {
	if field == "" {
		return 0
	}

	values := make(map[string]bool)
	for _, v := range d.docs {
		value, ok := v.getFieldValueByID(field)
		if ok {
			values[value] = true
		}
	}

	return len(values)
}

func runLevel1Tests() {
	store := NewDocStore()

	// Test 1: 參數驗證
	assert(!store.Insert("", map[string]string{"name": "alice"}), "Test 1.1: Empty id fails")
	assert(!store.Insert("doc1", nil), "Test 1.2: Nil fields fails")

	// Test 2: 基本插入與查詢
	assert(store.Insert("doc1", map[string]string{"name": "alice", "role": "admin"}), "Test 2.1: Insert doc1")
	assert(!store.Insert("doc1", map[string]string{"name": "bob"}), "Test 2.2: Duplicate id fails")

	f, ok := store.Get("doc1")
	assert(ok && f["name"] == "alice" && f["role"] == "admin", "Test 2.3: Get doc1 correct")

	// Test 3: 計數
	assert(store.Count() == 1, "Test 3.1: Count is 1")

	// Test 4: 刪除
	assert(store.Delete("doc1"), "Test 4.1: Delete doc1")
	assert(!store.Delete("doc1"), "Test 4.2: Delete non-existent doc1 fails")
	assert(store.Count() == 0, "Test 4.3: Count is 0 after delete")

	_, ok = store.Get("doc1")
	assert(!ok, "Test 4.4: doc1 is gone")

	fmt.Println(">>> Level 1 Tests Passed! <<<")
}

func runLevel2Tests() {
	store := NewDocStore()

	// 寫入基礎文件
	store.Insert("doc1", map[string]string{"type": "user", "status": "active", "city": "taipei"})
	store.Insert("doc2", map[string]string{"type": "user", "status": "pending", "city": "tokyo"})
	store.Insert("doc3", map[string]string{"type": "order", "status": "active", "total": "100"})

	// Test 1: Update 參數驗證與不存在檢查
	assert(!store.Update("", map[string]string{"status": "banned"}), "Test 1.1: Empty id update fails")
	assert(!store.Update("doc1", nil), "Test 1.2: Nil fields update fails")
	assert(!store.Update("doc_unknown", map[string]string{"status": "active"}), "Test 1.3: Non-existent doc fails")

	// Test 2: Update Merge 策略與欄位刪除
	// doc1 原有: type=user, status=active, city=taipei
	// 更新: status 改為 inactive, city 傳空字串 (刪除), 新增 tier=gold
	assert(store.Update("doc1", map[string]string{
		"status": "inactive",
		"city":   "",
		"tier":   "gold",
	}), "Test 2.1: Update doc1 success")

	d1, ok := store.Get("doc1")
	assert(ok, "Test 2.2: Get doc1 ok")
	assert(d1["type"] == "user", "Test 2.3: Unmodified field 'type' preserved")
	assert(d1["status"] == "inactive", "Test 2.4: Updated field 'status' modified")
	assert(d1["tier"] == "gold", "Test 2.5: New field 'tier' added")
	_, hasCity := d1["city"]
	assert(!hasCity, "Test 2.6: Field 'city' with empty string removed")

	// Test 3: FindByField 查詢 (按 ID 字典序排序)
	// 目前 status: doc1=inactive, doc2=pending, doc3=active
	actives := store.FindByField("status", "active")
	assert(len(actives) == 1 && actives[0] == "doc3", "Test 3.1: Find active status")

	// 再加一個 type=user
	store.Insert("doc0", map[string]string{"type": "user", "status": "active"})
	users := store.FindByField("type", "user")
	// 匹配 doc0, doc1, doc2 -> 字典序為 ["doc0", "doc1", "doc2"]
	assert(len(users) == 3, "Test 3.2: 3 user docs")
	assert(users[0] == "doc0" && users[1] == "doc1" && users[2] == "doc2", "Test 3.3: Users sorted by ID")

	// 查無資料
	empty := store.FindByField("type", "non_existent")
	assert(len(empty) == 0, "Test 3.4: Find non-existent value returns empty")
	assert(len(store.FindByField("", "user")) == 0, "Test 3.5: Empty field returns empty")

	// Test 4: CountDistinctValues 統計
	// 目前 type 欄位的值有: "user" (doc0, doc1, doc2), "order" (doc3) -> 共 2 種
	assert(store.CountDistinctValues("type") == 2, "Test 4.1: Distinct types is 2")
	// 目前 status 欄位的值有: "active" (doc0, doc3), "inactive" (doc1), "pending" (doc2) -> 共 3 種
	assert(store.CountDistinctValues("status") == 3, "Test 4.2: Distinct status is 3")
	// 不存在的欄位
	assert(store.CountDistinctValues("ghost") == 0, "Test 4.3: Unknown field is 0")

	fmt.Println(">>> Level 2 Tests Passed! <<<")
}

func assert(cond bool, msg string) {
	if !cond {
		panic(msg)
	}
}

func main() {
	runLevel1Tests()
	runLevel2Tests()
}
