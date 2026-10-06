// Package channel 定义通知渠道抽象，并提供飞书、钉钉机器人 webhook 实现。
// 新增渠道只需实现 Channel 接口并在 New 工厂中注册。
package channel

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// sendTimeout 单次 webhook 推送超时。
const sendTimeout = 10 * time.Second

// httpClient 各渠道共用的出站 HTTP 客户端（直连，不使用代理）。
var httpClient = &http.Client{Timeout: sendTimeout}

// Channel 通知渠道接口。
type Channel interface {
	// Type 返回渠道类型标识，如 "feishu"、"dingtalk"。
	Type() string
	// Send 推送一条文本消息。
	Send(ctx context.Context, message string) error
}

// Options 渠道构造参数，来自 config.ChannelConfig。
type Options struct {
	Webhook   string // 完整 webhook 地址（webhook 类渠道）
	Secret    string // 加签密钥，可为空
	AppID     string // 自建应用 App ID（feishu-app）
	AppSecret string // 自建应用 App Secret（feishu-app）
	ReceiveID string // oc_(群) / ou_(单聊)（feishu-app）
}

// New 渠道工厂：按类型构造 Channel 实例。
func New(channelType string, opts Options) (Channel, error) {
	switch channelType {
	case "feishu":
		return NewFeishu(opts), nil
	case "dingtalk":
		return NewDingTalk(opts), nil
	case "feishu-app":
		return NewFeishuApp(opts)
	default:
		return nil, fmt.Errorf("不支持的渠道类型: %q", channelType)
	}
}
