package ipprobe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidPublicIP(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"203.0.113.7\n", "203.0.113.7", false},
		{"当前 IP：203.0.113.7  来自于：中国", "203.0.113.7", false},
		{"2001:db8::1", "2001:db8::1", false},
		{"192.168.1.10", "", true},     // 私网
		{"10.0.0.5", "", true},         // 私网
		{"127.0.0.1", "", true},        // 环回
		{"100.64.0.1", "", true},       // CGNAT
		{"224.0.0.1", "", true},        // 组播
		{"no ip here", "", true},       // 无 IP
		{"999.999.999.999", "", true},  // 非法（正则可匹配但 ParseIP 拒绝）
	}
	for _, c := range cases {
		got, err := ValidPublicIP(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ValidPublicIP(%q) 应报错，得到 %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ValidPublicIP(%q) 意外错误: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ValidPublicIP(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPublicIPWithProvidersFallback(t *testing.T) {
	// 第一个源返回私网地址 → 校验拒绝，跳过；第二个源正常返回
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("192.168.1.1"))
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("203.0.113.9"))
	}))
	defer good.Close()

	ip, err := PublicIPWithProviders(context.Background(), bad.Client(), []string{bad.URL, good.URL})
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if ip != "203.0.113.9" {
		t.Fatalf("IP = %q, want 203.0.113.9", ip)
	}
}

func TestPublicIPAllFail(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer dead.Close()
	_, err := PublicIPWithProviders(context.Background(), dead.Client(), []string{dead.URL})
	if err == nil {
		t.Fatal("全部源失败时应返回错误")
	}
}
