// Package state 维护运行时状态（最近公网 IP、各渠道最近发送结果），
// 持久化到数据目录（$TRIM_PKGVAR/state.json），重启后可恢复展示。
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ChannelState 单个渠道的最近一次发送结果。
type ChannelState struct {
	Time  time.Time `json:"time"`
	OK    bool      `json:"ok"`
	Error string    `json:"error,omitempty"`
}

// State 应用运行状态。
type State struct {
	LastIP       string                  `json:"last_ip,omitempty"`
	LastSendTime time.Time               `json:"last_send_time,omitempty"`
	LastSendOK   *bool                   `json:"last_send_ok,omitempty"`
	Channels     map[string]ChannelState `json:"channels,omitempty"`
}

// Store 线程安全的状态存储。
type Store struct {
	mu   sync.Mutex
	path string
	st   State
}

// NewMemory 创建仅存于内存的状态存储（CLI 一次性任务使用）。
func NewMemory() *Store {
	return &Store{st: State{Channels: map[string]ChannelState{}}}
}

// Load 从 path 读取状态；文件不存在时返回空状态。
func Load(path string) (*Store, error) {
	s := &Store{path: path, st: State{Channels: map[string]ChannelState{}}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.st); err != nil {
		// 状态文件损坏不应阻断启动，回到空状态。
		s.st = State{Channels: map[string]ChannelState{}}
	}
	if s.st.Channels == nil {
		s.st.Channels = map[string]ChannelState{}
	}
	return s, nil
}

// Get 返回状态快照。
func (s *Store) Get() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.st
	out.Channels = make(map[string]ChannelState, len(s.st.Channels))
	for k, v := range s.st.Channels {
		out.Channels[k] = v
	}
	return out
}

// RecordIP 记录最近一次获取到的公网 IP。
func (s *Store) RecordIP(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.LastIP = ip
	s.saveLocked()
}

// RecordSend 记录一次整体发送结果：results 以渠道 id 为键，
// 全部成功时整体标记成功，任一失败则标记失败。
func (s *Store) RecordSend(results map[string]ChannelState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	allOK := true
	s.st.Channels = make(map[string]ChannelState, len(results))
	for id, r := range results {
		cs := r
		cs.Time = now
		s.st.Channels[id] = cs
		if !cs.OK {
			allOK = false
		}
	}
	s.st.LastSendTime = now
	s.st.LastSendOK = &allOK
	s.saveLocked()
}

// saveLocked 持久化（调用方需持有锁）；失败静默，状态仅用于展示。
func (s *Store) saveLocked() {
	data, err := json.MarshalIndent(&s.st, "", "  ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, s.path)
}
