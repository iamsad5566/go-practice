package main

import (
	"fmt"
	"sort"
	"strings"
)

type KVStore struct {
	Storage map[string]*Data
}

type Data struct {
	Key       string
	Value     string
	TTL       int64
	CreatedAt int64
	UpdatedAt int64
	DeletedAt int64
	Log       []*LogData
}

type LogData struct {
	UpdatedAt int64
	Value     string
}

func NewLogData(timestamp int64, value string) *LogData {
	return &LogData{
		UpdatedAt: timestamp,
		Value:     value,
	}
}

func NewData(k, v string, ttl, timestamp int64) *Data {
	return &Data{
		Key:       k,
		Value:     v,
		TTL:       ttl,
		CreatedAt: timestamp,
		UpdatedAt: timestamp,
		DeletedAt: 0,
		Log:       []*LogData{NewLogData(timestamp, v)},
	}
}

func (d *Data) IsExpired(timestamp int64) bool {
	return d.TTL != -1 && d.TTL <= timestamp
}

func (d *Data) IsDeleted() bool {
	return d.DeletedAt != 0
}

func NewKVStore() *KVStore {
	return &KVStore{
		Storage: make(map[string]*Data),
	}
}

func (k *KVStore) Set(timestamp int64, key, value string) {
	data := k.Storage[key]
	if data == nil || data.IsExpired(timestamp) || data.IsDeleted() {
		data = NewData(key, value, -1, timestamp)
	} else {
		data.UpdatedAt = timestamp
		data.Value = value
		data.Log = append(data.Log, NewLogData(timestamp, value))
	}
}

func (k *KVStore) Get(timestamp int64, key string) (string, bool) {
	data := k.Storage[key]
	if data == nil || data.IsExpired(timestamp) {
		return "", false
	}

	return data.Value, true
}

func (k *KVStore) SetWithTTL(timestamp int64, key string, value string, ttl int64) bool {
	if ttl <= 0 {
		return false
	}

	k.Storage[key] = NewData(key, value, timestamp+ttl, timestamp)
	return true
}

func (k *KVStore) Scan(timestamp int64, prefix string) []string {
	dt := make([]*Data, 0, len(k.Storage))

	for _, value := range k.Storage {
		if strings.HasPrefix(value.Key, prefix) && !value.IsExpired(timestamp) {
			dt = append(dt, value)
		}
	}

	sort.Slice(dt, func(i, j int) bool {
		return dt[i].Key < dt[j].Key
	})

	res := make([]string, len(dt))
	for i := 0; i < len(res); i++ {
		res[i] = fmt.Sprintf("%s(%s)", dt[i].Key, dt[i].Value)
	}

	return res
}

func (k *KVStore) Delete(timestamp int64, key string) bool {
	v := k.Storage[key]
	if v == nil || v.IsExpired(timestamp) {
		return false
	}

	delete(k.Storage, key)
	return true
}

func (k *KVStore) GetAt(timestamp int64, key string, asOfTime int64) (string, bool) {
	if asOfTime > timestamp {
		return "", false
	}

	data := k.Storage[key]
	if data == nil || data.IsExpired(asOfTime) {
		return "", false
	}

	value := data.Log[0].Value

	for i, v := range data.Log {
		if timestamp < v.UpdatedAt {
			// 不可能找到 i = 0 且 v.UpdatedAt < asOfTime
			value = data.Log[i-1].Value
			break
		}

	}

	return value, true
}

func (k *KVStore) RollbackTo(timestamp int64, targetTime int64) bool {
	if targetTime >= timestamp {
		return false
	}

	for key, v := range k.Storage {
		historicalV, ok := k.GetAt(timestamp, key, targetTime)
		if ok {
			v.Value = historicalV
			for i, ob := range v.Log {
				if ob.UpdatedAt > targetTime {
					v.Log = v.Log[0:i]
					break
				}
			}
			v.UpdatedAt = v.Log[len(v.Log)-1].UpdatedAt
		}
	}
	return true
}
