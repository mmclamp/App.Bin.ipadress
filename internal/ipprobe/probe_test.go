package ipprobe

import (
	"context"
	"net"
	"strings"
	"testing"
)

// ipaddr 构造测试用 net.Addr（掩码长度不影响本包逻辑）。
func ipaddr(s string) net.Addr {
	ip := net.ParseIP(s)
	mask := net.CIDRMask(64, 128)
	if ip.To4() != nil {
		mask = net.CIDRMask(24, 32)
	}
	return &net.IPNet{IP: ip, Mask: mask}
}

func TestFirstGlobalIPv6(t *testing.T) {
	cases := []struct {
		name    string
		addrs   []net.Addr
		want    string
		wantErr bool
	}{
		{"跳过私网 v4 与链路本地，取全局 v6", []net.Addr{
			ipaddr("192.168.1.10"), ipaddr("fe80::1"), ipaddr("2409:871a:18a0:400::130"),
		}, "2409:871a:18a0:400::130", false},
		{"ULA 私网被排除", []net.Addr{
			ipaddr("fd12:3456:789a::1"), ipaddr("2409:871a:18a0:400::131"),
		}, "2409:871a:18a0:400::131", false},
		{"组播与环回被排除", []net.Addr{
			ipaddr("ff02::1"), ipaddr("::1"), ipaddr("2409:871a:18a0:400::132"),
		}, "2409:871a:18a0:400::132", false},
		{"IPv4 映射形式被排除", []net.Addr{
			ipaddr("::ffff:203.0.113.7"), ipaddr("2409:871a:18a0:400::133"),
		}, "2409:871a:18a0:400::133", false},
		{"多个全局取首个", []net.Addr{
			ipaddr("2409:871a:18a0:400::134"), ipaddr("2409:871a:18a0:400::135"),
		}, "2409:871a:18a0:400::134", false},
		{"仅 v4 时报错", []net.Addr{ipaddr("192.168.1.10"), ipaddr("10.0.0.5")}, "", true},
		{"空列表报错", []net.Addr{}, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := FirstGlobalIPv6(c.addrs)
			if c.wantErr {
				if err == nil {
					t.Fatalf("应报错，得到 %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("意外错误: %v", err)
			}
			if got != c.want {
				t.Fatalf("FirstGlobalIPv6 = %q, want %q", got, c.want)
			}
		})
	}
}

func TestPublicIPCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PublicIP(ctx); !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("已取消的 ctx 应返回取消错误，得到: %v", err)
	}
}
