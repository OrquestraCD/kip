// This file was automatically generated by genny.
// Any changes will be lost if this file is regenerated.
// see https://github.com/cheekybits/genny

package conmap

import (
	"sync"
	"time"
)

// Mostly taken from
// https://github.com/cheekybits/gennylib/blob/master/maps/concurrentmap.go
// We've added an Items() function. Don't use cheekybits/genny since
// that doesn't have a good golang parser, use the one from justnoise
// (that's ME!)

type StringTimeTime struct {
	sync.RWMutex
	data map[string]time.Time
}

type NodeStringTimeTime struct {
	Key   string
	Value time.Time
}

func NewStringTimeTime() *StringTimeTime {
	return &StringTimeTime{
		data: make(map[string]time.Time),
	}
}

func (m *StringTimeTime) Set(key string, value time.Time) {
	m.Lock()
	m.data[key] = value
	m.Unlock()
}

func (m *StringTimeTime) Delete(key string) {
	m.Lock()
	delete(m.data, key)
	m.Unlock()
}

func (m *StringTimeTime) Get(key string) time.Time {
	m.RLock()
	value := m.data[key]
	m.RUnlock()
	return value
}

func (m *StringTimeTime) GetOK(key string) (time.Time, bool) {
	m.RLock()
	value, exists := m.data[key]
	m.RUnlock()
	return value, exists
}

func (m *StringTimeTime) Len() int {
	m.RLock()
	len := len(m.data)
	m.RUnlock()
	return len
}

func (m *StringTimeTime) Items() []NodeStringTimeTime {
	m.RLock()
	items := make([]NodeStringTimeTime, 0, len(m.data))
	for k, v := range m.data {
		items = append(items, NodeStringTimeTime{Key: k, Value: v})
	}
	m.RUnlock()
	return items
}
