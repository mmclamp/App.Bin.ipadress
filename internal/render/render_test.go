package render

import (
	"strings"
	"testing"
	"time"
)

func TestRenderPlaceholders(t *testing.T) {
	now := time.Date(2026, 10, 6, 9, 30, 5, 0, time.FixedZone("CST", 8*3600))
	v := Vars{IP: "203.0.113.7", Now: now, Host: "fnos"}

	cases := []struct {
		tpl, want string
	}{
		{"{ip}", "203.0.113.7"},
		{"http://{ip}:port/xxx", "http://203.0.113.7:port/xxx"},
		{"{IP}", "203.0.113.7"},
		{"{date} {time}", "2026-10-06 09:30:05"},
		{"{hostname}: {ip}", "fnos: 203.0.113.7"},
		{"no placeholders", "no placeholders"},
		{"{unknown} {ip}", "{unknown} 203.0.113.7"},
	}
	_ = cases
	for _, c := range cases {
		if got := Render(c.tpl, v); got != c.want {
			t.Errorf("Render(%q) = %q, want %q", c.tpl, got, c.want)
		}
	}
}

func TestRenderZeroNow(t *testing.T) {
	// Now 为零值时使用当前时间渲染，不应 panic
	got := Render("{ip} {date}", Vars{IP: "1.2.3.4"})
	if got == "" {
		t.Fatal("Render 返回空")
	}
	if !strings.HasPrefix(got, "1.2.3.4 ") {
		t.Fatalf("Render 前缀不符: %q", got)
	}
}
