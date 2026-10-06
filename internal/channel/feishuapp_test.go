package channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// startFeishuMock 启动模拟飞书 OpenAPI 服务，返回服务器与请求记录通道。
func startFeishuMock(t *testing.T, sendResp string) (*httptest.Server, chan map[string]any) {
	t.Helper()
	ch := make(chan map[string]any, 8)
	mux := http.NewServeMux()
	mux.HandleFunc("/open-apis/auth/v3/tenant_access_token/internal", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"t-test-token","expire":7200}`))
	})
	mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		ch <- m
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sendResp))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, ch
}

func TestFeishuAppSend(t *testing.T) {
	srv, msgs := startFeishuMock(t, `{"code":0,"msg":"success"}`)
	oldBase := feishuAPIBase
	feishuAPIBase = srv.URL
	defer func() { feishuAPIBase = oldBase }()
	feishuTokenCache.Lock()
	feishuTokenCache.m = map[string]tokenEntry{}
	feishuTokenCache.Unlock()

	f, err := NewFeishuApp(Options{AppID: "cli_x", AppSecret: "sec", ReceiveID: "oc_group1"})
	if err != nil {
		t.Fatalf("NewFeishuApp: %v", err)
	}
	if f.Type() != "feishu-app" {
		t.Fatalf("Type() = %q", f.Type())
	}
	if err := f.Send(context.Background(), "IP 203.0.113.7"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	m := <-msgs
	if m["receive_id"] != "oc_group1" {
		t.Errorf("receive_id = %v", m["receive_id"])
	}
	if m["msg_type"] != "text" {
		t.Errorf("msg_type = %v", m["msg_type"])
	}
	content, _ := m["content"].(string)
	var inner map[string]string
	if json.Unmarshal([]byte(content), &inner) != nil || inner["text"] != "IP 203.0.113.7" {
		t.Errorf("content 不是字符串化 JSON: %v", m["content"])
	}
}

func TestFeishuAppTokenRetry(t *testing.T) {
	srv, msgs := startFeishuMock(t, `{"code":99991661,"msg":"access token invalid"}`)
	oldBase := feishuAPIBase
	feishuAPIBase = srv.URL
	defer func() { feishuAPIBase = oldBase }()
	feishuTokenCache.Lock()
	feishuTokenCache.m = map[string]tokenEntry{}
	feishuTokenCache.Unlock()

	f, _ := NewFeishuApp(Options{AppID: "cli_retry", AppSecret: "sec", ReceiveID: "ou_user1"})
	// mock 固定返回 99991661：刷新重试后仍失败 → 报错，且发送接口恰好被请求两次
	err := f.Send(context.Background(), "x")
	if err == nil {
		t.Fatal("持续 token 失效应报错")
	}
	close(msgs)
	count := 0
	for range msgs {
		count++
	}
	if count != 2 {
		t.Errorf("token 失效应重试一次，发送接口被请求 %d 次", count)
	}
}

func TestFeishuAppInvalidReceiveID(t *testing.T) {
	if _, err := NewFeishuApp(Options{AppID: "a", AppSecret: "b", ReceiveID: "xxx"}); err == nil {
		t.Fatal("非法 receive_id 应报错")
	}
}
