// Package config 提供应用配置的读写与校验。
// 配置以 JSON 形式保存在 fnOS 应用配置目录（$TRIM_PKGETC/config.json）。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/robfig/cron/v3"
)

// 渠道类型常量。
const (
	TypeFeishu   = "feishu"     // 飞书自定义机器人 webhook
	TypeDingTalk = "dingtalk"   // 钉钉自定义机器人 webhook
	TypeFeishuApp = "feishu-app" // 飞书自建应用（OpenAPI + tenant_access_token）
)

// SupportedTypes 返回当前支持的渠道类型列表。
func SupportedTypes() []string {
	return []string{TypeFeishu, TypeDingTalk}
}

// IsValidType 报告渠道类型是否受支持。
func IsValidType(t string) bool {
	for _, s := range SupportedTypes() {
		if t == s {
			return true
		}
	}
	return false
}

// ChannelConfig 描述一个通知渠道（如飞书、钉钉机器人 webhook）。
type ChannelConfig struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Enabled  bool   `json:"enabled"`
	Webhook  string `json:"webhook,omitempty"`
	Secret   string `json:"secret,omitempty"`
	Template string `json:"template,omitempty"` // 为空时回退到全局模板
	// 自建应用类渠道（feishu-app）专用字段
	AppID     string `json:"app_id,omitempty"`
	AppSecret string `json:"app_secret,omitempty"`
	ReceiveID string `json:"receive_id,omitempty"` // oc_ 开头=群 chat_id；ou_ 开头=单聊 open_id
}

// ValidateChannel 校验单个渠道配置。
func (c *ChannelConfig) ValidateChannel() error {
	if c.ID == "" {
		return errors.New("渠道缺少 id")
	}
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("渠道名称不能为空")
	}
	if !IsValidType(c.Type) {
		return fmt.Errorf("不支持的渠道类型: %s", c.Type)
	}
	switch c.Type {
	case TypeFeishuApp:
		if strings.TrimSpace(c.AppID) == "" || strings.TrimSpace(c.AppSecret) == "" {
			return errors.New("飞书自建应用需填写 App ID 与 App Secret")
		}
		id := strings.TrimSpace(c.ReceiveID)
		if !strings.HasPrefix(id, "oc_") && !strings.HasPrefix(id, "ou_") {
			return errors.New("接收 ID 需以 oc_(群 chat_id) 或 ou_(单聊 open_id) 开头")
		}
	default:
		if err := validateWebhook(c.Webhook); err != nil {
			return fmt.Errorf("webhook 无效: %w", err)
		}
	}
	return nil
}

// Config 应用配置。
type Config struct {
	Cron     string          `json:"cron"`
	Template string          `json:"template"`
	Timezone string          `json:"timezone,omitempty"`
	Channels []ChannelConfig `json:"channels"`
}

// Default 返回默认配置。
func Default() *Config {
	return &Config{
		Cron:     "0 9 * * *",
		Template: "{ip}",
		Channels: []ChannelConfig{},
	}
}

// Validate 校验配置合法性，返回首个错误。
func (c *Config) Validate() error {
	spec := c.Cron
	if strings.TrimSpace(spec) == "" {
		return errors.New("cron 表达式不能为空")
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(spec); err != nil {
		return fmt.Errorf("cron 表达式无效: %w", err)
	}
	if strings.TrimSpace(c.Template) == "" {
		return errors.New("消息模板不能为空")
	}
	seen := map[string]bool{}
	for i, ch := range c.Channels {
		if seen[ch.ID] {
			return fmt.Errorf("渠道 id 重复: %s", ch.ID)
		}
		seen[ch.ID] = true
		if err := ch.ValidateChannel(); err != nil {
			return fmt.Errorf("第 %d 个渠道(%q): %w", i+1, ch.Name, err)
		}
	}
	return nil
}

func validateWebhook(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("必须是 http(s) URL")
	}
	if u.Host == "" {
		return errors.New("缺少主机地址")
	}
	return nil
}

// Load 从 path 读取配置；文件不存在时返回默认配置且不报错。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return nil, err
	}
	cfg := Default()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置 %s 失败: %w", path, err)
	}
	if cfg.Channels == nil {
		cfg.Channels = []ChannelConfig{}
	}
	return cfg, nil
}

// Save 原子化写入配置（先写临时文件再重命名）。
func (c *Config) Save(path string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
