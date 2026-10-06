package channel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// newMockWebhook 启动一个记录请求的 mock webhook。
func newMockWebhook(t *testing.T, status int, resp string) (*httptest.Server, *[]map[string]any, *[]*url.URL) {
	t.Helper()
	var bodies []map[string]any
	var urls []*url.URL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		bodies = append(bodies, m)
		urls = append(urls, r.URL)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies, &urls
}

func TestFeishuSend(t *testing.T) {
	srv, bodies, _ := newMockWebhook(t, 200, `{"code":0,"msg":"success"}`)
	f := NewFeishu(Options{Webhook: srv.URL})
	if f.Type() != "feishu" {
		t.Fatalf("Type() = %q", f.Type())
	}
	if err := f.Send(context.Background(), "测试 203.0.113.7"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(*bodies) != 1 {
		t.Fatalf("请求次数 = %d", len(*bodies))
	}
	body := (*bodies)[0]
	if body["msg_type"] != "text" {
		t.Errorf("msg_type = %v", body["msg_type"])
	}
	content, _ := body["content"].(map[string]any)
	if content == nil || content["text"] == "" {
		t.Error("content.text 为空")
	}
}

func TestFeishuSendSigned(t *testing.T) {
	srv, bodies, _ := newMockWebhook(t, 200, `{"code":0,"msg":"success"}`)
	f := NewFeishu(Options{Webhook: srv.URL, Secret: "sec-abc"})
	if err := f.Send(context.Background(), "加签测试"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	body := (*bodies)[0]
	if body["timestamp"] == nil || body["sign"] == "" {
		t.Errorf("缺少加签字段: %v", body)
	}
}

func TestFeishuSendBadResponse(t *testing.T) {
	srv, _, _ := newMockWebhook(t, 200, `{"code":19021,"msg":"Bad Signature"}`)
	f := NewFeishu(Options{Webhook: srv.URL})
	if err := f.Send(context.Background(), "x"); err == nil {
		t.Fatal("飞书 code!=0 应返回错误")
	}
}

func TestFeishuSign(t *testing.T) {
	sign := FeishuSign("1655626800", "test-secret")
	if sign != FeishuSign("1655626800", "test-secret") {
		t.Error("签名不稳定")
	}
	// base64(sha256) 固定 44 字节（含 1 个 '='）
	if len(sign) != 44 || !strings.HasSuffix(sign, "=") {
		t.Errorf("签名格式异常: %q (len=%d)", sign, len(sign))
	}
}

func TestDingTalkSend(t *testing.T) {
	srv, bodies, urls := newMockWebhook(t, 200, `{"errcode":0,"errmsg":"ok"}`)
	d := NewDingTalk(Options{Webhook: srv.URL, Secret: "SECxxxxxxxx"})
	if d.Type() != "dingtalk" {
		t.Fatalf("Type() = %q", d.Type())
	}
	if err := d.Send(context.Background(), "钉钉测试 203.0.113.7"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(*bodies) != 1 {
		t.Fatalf("请求次数 = %d", len(*bodies))
	}
	body := (*bodies)[0]
	if body["msgtype"] != "text" {
		t.Errorf("msgtype = %v", body["msgtype"])
	}
	text, _ := body["text"].(map[string]any)
	if text == nil || text["content"] == "" {
		t.Error("text.content 为空")
	}
	u := (*urls)[0]
	if u.Query().Get("timestamp") == "" || u.Query().Get("sign") == "" {
		t.Errorf("缺少加签参数: %s", u.String())
	}
}

func TestDingTalkSignURL(t *testing.T) {
	got := DingTalkSignURL("https://oapi.dingtalk.com/robot/send?access_token=abc", "SECxxx", 1655626800000)
	if !strings.Contains(got, "timestamp=1655626800000") {
		t.Errorf("URL 缺少 timestamp: %s", got)
	}
	if !strings.Contains(got, "sign=") {
		t.Errorf("URL 缺少 sign: %s", got)
	}
}

func TestFactory(t *testing.T) {
	if _, err := New("feishu", Options{Webhook: "https://example.com/hook"}); err != nil {
		t.Errorf("feishu 工厂失败: %v", err)
	}
	if _, err := New("dingtalk", Options{Webhook: "https://example.com/hook"}); err != nil {
		t.Errorf("dingtalk 工厂失败: %v", err)
	}
	if _, err := New("nope", Options{Webhook: "https://x.com"}); err == nil {
		t.Error("未知类型应报错")
	}
}
