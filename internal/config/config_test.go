package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultWhenMissing(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Cron != "0 9 * * *" || cfg.Template != "{ip}" {
		t.Errorf("默认配置不符: %+v", cfg)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.Channels = []ChannelConfig{{
		ID: "c1", Name: "飞书群", Type: TypeFeishu, Enabled: true,
		Webhook: "https://open.feishu.cn/open-apis/bot/v2/hook/xxx", Secret: "",
	}}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Channels) != 1 || got.Channels[0].Name != "飞书群" {
		t.Errorf("渠道不符: %+v", got.Channels)
	}
	// 配置含密钥，权限收紧为 0600
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("配置文件权限 = %v, want 0600", fi.Mode().Perm())
	}
}

func TestValidate(t *testing.T) {
	good := Default()
	if err := good.Validate(); err != nil {
		t.Fatalf("默认配置应合法: %v", err)
	}

	badCron := Default()
	badCron.Cron = "not a cron"
	if err := badCron.Validate(); err == nil {
		t.Error("非法 cron 应报错")
	}

	badType := Default()
	badType.Channels = []ChannelConfig{{ID: "x", Name: "n", Type: "slack", Webhook: "https://x.com"}}
	if err := badType.Validate(); err == nil {
		t.Error("未知渠道类型应报错")
	}

	badWebhook := Default()
	badWebhook.Channels = []ChannelConfig{{ID: "x", Name: "n", Type: TypeFeishu, Webhook: "ftp://x.com"}}
	if err := badWebhook.Validate(); err == nil {
		t.Error("非 http(s) webhook 应报错")
	}

	dupID := Default()
	dupID.Channels = []ChannelConfig{
		{ID: "a", Name: "n", Type: TypeFeishu, Webhook: "https://x.com"},
		{ID: "a", Name: "n2", Type: TypeDingTalk, Webhook: "https://x.com"},
	}
	if err := dupID.Validate(); err == nil {
		t.Error("重复渠道 id 应报错")
	}
}
