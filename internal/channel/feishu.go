package channel

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Feishu 飞书自定义机器人 webhook 渠道。
type Feishu struct {
	webhook string
	secret  string
}

// NewFeishu 构造飞书渠道；secret 为空表示未启用加签。
func NewFeishu(opts Options) *Feishu {
	return &Feishu{webhook: strings.TrimSpace(opts.Webhook), secret: opts.Secret}
}

// Type 实现 Channel 接口。
func (f *Feishu) Type() string { return "feishu" }

// Send 推送文本消息到飞书群。
// 协议：POST {"msg_type":"text","content":{"text":...}}，
// 启用加签时顶层附加 timestamp 与 sign。
func (f *Feishu) Send(ctx context.Context, message string) error {
	payload := map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": message},
	}
	if f.secret != "" {
		ts := time.Now().Unix()
		payload["timestamp"] = ts
		payload["sign"] = FeishuSign(fmt.Sprintf("%d", ts), f.secret)
	}
	return postJSON(ctx, f.webhook, payload, func(body []byte) error {
		var r struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if err := json.Unmarshal(body, &r); err != nil {
			return fmt.Errorf("解析飞书响应失败: %w (%s)", err, truncate(body))
		}
		if r.Code != 0 {
			return fmt.Errorf("飞书返回错误 code=%d msg=%s", r.Code, r.Msg)
		}
		return nil
	})
}

// FeishuSign 计算飞书加签：
// stringToSign = timestamp + "\n" + secret，
// 以 stringToSign 为 HMAC-SHA256 密钥、空内容求摘要后 base64 编码。
func FeishuSign(timestamp, secret string) string {
	stringToSign := timestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// postJSON 发送 JSON POST 并用 check 校验响应体。
func postJSON(ctx context.Context, url string, payload any, check func(body []byte) error) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(body))
	}
	return check(body)
}

func truncate(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}
