package channel

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// DingTalk 钉钉自定义机器人 webhook 渠道。
type DingTalk struct {
	webhook string
	secret  string
}

// NewDingTalk 构造钉钉渠道；secret 为空表示未启用加签。
func NewDingTalk(opts Options) *DingTalk {
	return &DingTalk{webhook: strings.TrimSpace(opts.Webhook), secret: opts.Secret}
}

// Type 实现 Channel 接口。
func (d *DingTalk) Type() string { return "dingtalk" }

// Send 推送文本消息到钉钉群。
// 协议：POST {"msgtype":"text","text":{"content":...}}，
// 启用加签时 webhook 追加 timestamp 与 sign 查询参数。
func (d *DingTalk) Send(ctx context.Context, message string) error {
	url := d.signedURL()
	payload := map[string]any{
		"msgtype": "text",
		"text":    map[string]string{"content": message},
	}
	return postJSON(ctx, url, payload, func(body []byte) error {
		var r struct {
			Errcode int    `json:"errcode"`
			Errmsg  string `json:"errmsg"`
		}
		if err := json.Unmarshal(body, &r); err != nil {
			return fmt.Errorf("解析钉钉响应失败: %w (%s)", err, truncate(body))
		}
		if r.Errcode != 0 {
			return fmt.Errorf("钉钉返回错误 errcode=%d errmsg=%s", r.Errcode, r.Errmsg)
		}
		return nil
	})
}

// signedURL 按 secret 是否存在返回最终请求地址。
func (d *DingTalk) signedURL() string {
	if d.secret == "" {
		return d.webhook
	}
	ts := time.Now().UnixMilli()
	return DingTalkSignURL(d.webhook, d.secret, ts)
}

// DingTalkSignURL 计算钉钉加签 URL：
// stringToSign = timestamp + "\n" + secret，
// 以 secret 为 HMAC-SHA256 密钥求摘要后 base64 编码并 URL 转义，
// 追加为 timestamp 与 sign 查询参数。
func DingTalkSignURL(webhook, secret string, timestampMilli int64) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestampMilli, secret)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stringToSign))
	sign := url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	sep := "&"
	if !strings.Contains(webhook, "?") {
		sep = "?"
	}
	return fmt.Sprintf("%s%stimestamp=%d&sign=%s", webhook, sep, timestampMilli, sign)
}
