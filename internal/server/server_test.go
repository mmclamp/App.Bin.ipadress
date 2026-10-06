package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"ipaddress/internal/config"
	"ipaddress/internal/logstore"
	"ipaddress/internal/state"
)

// newTestServer 构造可注入 Probe 的测试服务器。
func newTestServer(t *testing.T, probe func(ctx context.Context) (string, error)) *Server {
	t.Helper()
	dir := t.TempDir()
	logs, err := logstore.New(100, "")
	if err != nil {
		t.Fatal(err)
	}
	st, err := state.Load(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(Deps{
		ConfigPath: filepath.Join(dir, "config.json"),
		DataDir:    dir,
		Version:    "test",
		Probe:      probe,
	}, logs, st)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestSendAllFeishu(t *testing.T) {
	var gotMsg string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		if content, ok := m["content"].(map[string]any); ok {
			if text, ok := content["text"].(string); ok {
				gotMsg = text
			}
		}
		_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	defer hook.Close()

	probe := func(ctx context.Context) (string, error) {
		return "203.0.113.7", nil
	}
	srv, _ := newTestServerWithDir(t, probe)
	cfg := config.Default()
	cfg.Channels = append(cfg.Channels, config.ChannelConfig{
		ID: "c1", Name: "飞书", Type: "feishu", Enabled: true, Webhook: hook.URL,
	})
	if err := srv.applyConfig(cfg); err != nil {
		t.Fatal(err)
	}

	res := srv.SendAll(context.Background(), "test")
	if !res.OK {
		t.Fatalf("SendAll 应成功: %+v", res)
	}
	if res.IP != "203.0.113.7" {
		t.Errorf("IP = %q", res.IP)
	}
	want := config.Default().Template // "{ip}" → "203.0.113.7"
	_ = want
	if gotMsg != "203.0.113.7" {
		t.Errorf("消息 = %q, want %q", gotMsg, "203.0.113.7")
	}
}

func TestSendAllProbeFail(t *testing.T) {
	probe := func(ctx context.Context) (string, error) {
		return "", context.DeadlineExceeded
	}
	srv, _ := newTestServerWithDir(t, probe)
	cfg := config.Default()
	cfg.Channels = append(cfg.Channels, config.ChannelConfig{
		ID: "c1", Name: "飞书", Type: "feishu", Enabled: true,
		Webhook: "http://127.0.0.1:1/hook",
	})
	if err := srv.applyConfig(cfg); err != nil {
		t.Fatal(err)
	}
	res := srv.SendAll(context.Background(), "test")
	if res.OK {
		t.Fatal("探测失败时 SendAll 应失败")
	}
	if len(res.Results) != 0 {
		t.Errorf("探测失败不应发送任何渠道: %+v", res.Results)
	}
}

func TestAdminGuard(t *testing.T) {
	probe := func(ctx context.Context) (string, error) { return "203.0.113.7", nil }
	srv := newTestServer(t, probe)
	h := srv.Handler()

	// 带网关用户头但非管理员 → 403
	req := httptest.NewRequest("PUT", "/api/config", strings.NewReader(`{"cron":"0 9 * * *"}`))
	req.Header.Set("X-Trim-Userid", "1000")
	req.Header.Set("X-Trim-Isadmin", "false")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("非管理员应 403，得到 %d", rec.Code)
	}

	// 无网关头（本机调试）放行到业务校验：cron 非法 → 400
	req2 := httptest.NewRequest("PUT", "/api/config", strings.NewReader(`{"cron":"bad spec"}`))
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 400 {
		t.Fatalf("无头应放行至业务校验（400），得到 %d", rec2.Code)
	}

	// 管理员 + 合法配置 → 200
	req3 := httptest.NewRequest("PUT", "/api/config", strings.NewReader(`{"cron":"30 8 * * *"}`))
	req3.Header.Set("X-Trim-Userid", "1000")
	req3.Header.Set("X-Trim-Isadmin", "true")
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("管理员合法配置应 200，得到 %d", rec3.Code)
	}
}

func TestChannelCRUD(t *testing.T) {
	probe := func(ctx context.Context) (string, error) { return "203.0.113.7", nil }
	srv := newTestServer(t, probe)
	h := srv.Handler()

	// 保存渠道
	body := `{"id":"c1","name":"飞书群","type":"feishu","enabled":true,"webhook":"http://127.0.0.1:1/hook"}`
	req := httptest.NewRequest("POST", "/api/channel/save", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("保存渠道应 200: %d %s", rec.Code, rec.Body.String())
	}

	// 读取配置确认
	cfg, err := config.Load(srv.deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Channels) != 1 || cfg.Channels[0].Name != "飞书群" {
		t.Fatalf("渠道未持久化: %+v", cfg.Channels)
	}

	// 删除渠道
	req2 := httptest.NewRequest("POST", "/api/channel/delete", strings.NewReader(`{"id":"c1"}`))
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("删除渠道应 200: %d", rec2.Code)
	}
	cfg2, _ := config.Load(srv.deps.ConfigPath)
	if len(cfg2.Channels) != 0 {
		t.Fatalf("渠道未删除: %+v", cfg2.Channels)
	}
}

func TestProbe(t *testing.T) {
	probe := func(ctx context.Context) (string, error) { return "198.51.100.5", nil }
	srv := newTestServer(t, probe)
	h := srv.Handler()

	req := httptest.NewRequest("POST", "/api/probe", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("probe 应 200: %d", rec.Code)
	}
	var r struct {
		IP string `json:"ip"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if r.IP != "198.51.100.5" {
		t.Fatalf("ip = %q", r.IP)
	}
	if st := srv.st.Get(); st.LastIP != "198.51.100.5" {
		t.Fatalf("状态未记录: %+v", st)
	}
}

func TestIndexPage(t *testing.T) {
	probe := func(ctx context.Context) (string, error) { return "203.0.113.7", nil }
	srv := newTestServer(t, probe)
	h := srv.Handler()
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("首页应 200: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "IP 通知") {
		t.Error("首页应包含标题")
	}
}

func newTestServerWithDir(t *testing.T, probe func(ctx context.Context) (string, error)) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	logs, err := logstore.New(100, "")
	if err != nil {
		t.Fatal(err)
	}
	st, err := state.Load(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(Deps{
		ConfigPath: filepath.Join(dir, "config.json"),
		DataDir:    dir,
		Version:    "test",
		Probe:      probe,
	}, logs, st)
	if err != nil {
		t.Fatal(err)
	}
	return srv, dir
}
