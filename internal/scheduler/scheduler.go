// Package scheduler 封装 cron 定时调度：
// 支持配置变更后重建调度、查询下次执行时间。
package scheduler

import (
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// Scheduler 基于 5 段式 cron 表达式的任务调度器。
type Scheduler struct {
	mu   sync.Mutex
	cron *cron.Cron
	task func()
}

// NewParser 返回标准 5 段式（分 时 日 月 周）解析器。
func NewParser() cron.Parser {
	return cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
}

// New 创建调度器；触发时任务在独立 goroutine 中执行。
func New(task func()) *Scheduler {
	return &Scheduler{task: task}
}

// Restart 以 spec 重建调度（先停止旧调度）。spec 需已通过校验。
func (s *Scheduler) Restart(spec string, location *time.Location) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if location == nil {
		location = time.Local
	}
	c := cron.New(cron.WithLocation(location), cron.WithParser(cron.NewParser(
		cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow)))
	if _, err := c.AddFunc(spec, func() { go s.task() }); err != nil {
		return err
	}
	if s.cron != nil {
		s.cron.Stop()
	}
	s.cron = c
	c.Start()
	return nil
}

// Stop 停止调度。
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cron != nil {
		s.cron.Stop()
		s.cron = nil
	}
}

// Next 返回下次执行时间；未调度时返回零值。
func (s *Scheduler) Next() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cron == nil {
		return time.Time{}
	}
	entries := s.cron.Entries()
	if len(entries) == 0 {
		return time.Time{}
	}
	return entries[0].Next
}
