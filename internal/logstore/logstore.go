// Package logstore 提供带内存环形缓冲的应用日志：
// 最近 N 条保留在内存中供 Web UI 展示，同时追加写入日志文件。
package logstore

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry 单条日志。
type Entry struct {
	Time  time.Time `json:"time"`
	Level string    `json:"level"`
	Msg   string    `json:"msg"`
}

// Ring 环形日志缓冲。
type Ring struct {
	mu      sync.Mutex
	entries []Entry
	cap     int
	file    *os.File
}

// New 创建容量为 cap 的环形缓冲；logPath 非空时尝试追加写入该文件，
// 文件不可写时降级为纯内存日志（不视为错误，保证服务可用）。
func New(cap int, logPath string) (*Ring, error) {
	r := &Ring{cap: cap}
	if logPath != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err == nil {
			f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
			if err == nil {
				r.file = f
			}
		}
	}
	return r, nil
}

// Add 记录一条日志（内存 + 文件，文件写入失败忽略）。
func (r *Ring) Add(level, format string, args ...any) {
	e := Entry{Time: time.Now(), Level: level, Msg: fmt.Sprintf(format, args...)}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, e)
	if len(r.entries) > r.cap {
		r.entries = r.entries[len(r.entries)-r.cap:]
	}
	if r.file != nil {
		fmt.Fprintf(r.file, "%s [%s] %s\n", e.Time.Format("2006-01-02 15:04:05"), e.Level, e.Msg)
	}
}

// Snapshot 返回最近 limit 条日志（limit<=0 返回全部）。
func (r *Ring) Snapshot(limit int) []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 || limit > len(r.entries) {
		limit = len(r.entries)
	}
	out := make([]Entry, limit)
	copy(out, r.entries[len(r.entries)-limit:])
	return out
}

// Close 关闭日志文件。
func (r *Ring) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		err := r.file.Close()
		r.file = nil
		return err
	}
	return nil
}
