// Package ipprobe 通过多个公网 IP 检测服务获取本机公网 IP。
// 依序尝试，任一成功即返回；结果必须为公网单播地址。
package ipprobe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// defaultProviders 公网 IP 检测源，按序尝试。
// 国内可达源优先；若返回 IPv6，拼接 ip:port 类模板时注意加方括号。
var defaultProviders = []string{
	"https://myip.ipip.net",
	"https://api.ip.sb/ip",
	"https://api.3322.net",
	"https://api4.ipify.org",
	"https://api.ipify.org",
}

// ipPattern 从响应文本中提取首个 IP 字面量（部分源返回整句文本）。
var ipPattern = regexp.MustCompile(`((?:\d{1,3}\.){3}\d{1,3})|(([0-9a-fA-F]{0,4}:){2,7}[0-9a-fA-F]{0,4})`)

// perProviderTimeout 单个检测源的超时时间。
const perProviderTimeout = 5 * time.Second

// PublicIP 依次请求各检测源，返回第一个校验通过的公网 IP 字符串。
func PublicIP(ctx context.Context, client *http.Client) (string, error) {
	return PublicIPWithProviders(ctx, client, defaultProviders)
}

// PublicIPWithProviders 允许自定义检测源列表，便于测试。
func PublicIPWithProviders(ctx context.Context, client *http.Client, providers []string) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	var errs []string
	for _, url := range providers {
		ip, err := fetchOne(ctx, client, url)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", url, err))
			continue
		}
		return ip, nil
	}
	return "", fmt.Errorf("所有公网 IP 检测源均失败: %s", strings.Join(errs, "; "))
}

func fetchOne(ctx context.Context, client *http.Client, url string) (string, error) {
	pctx, cancel := context.WithTimeout(ctx, perProviderTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(pctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ipaddress-fnos-app/0.1")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if err != nil {
		return "", err
	}
	ip, err := ValidPublicIP(string(body))
	if err != nil {
		return "", err
	}
	return ip, nil
}

// ValidPublicIP 从文本中提取并校验公网 IP；拒绝私网、环回、链路本地、
// CGNAT(100.64/10) 与组播等非公网地址，防止把内网地址当公网 IP 推送。
func ValidPublicIP(text string) (string, error) {
	m := ipPattern.FindString(text)
	if m == "" {
		return "", errors.New("响应中未找到 IP 地址")
	}
	ip := net.ParseIP(m)
	if ip == nil {
		return "", fmt.Errorf("无法解析 IP: %q", m)
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return "", fmt.Errorf("非公网地址: %s", ip)
	}
	if v4 := ip.To4(); v4 != nil {
		// CGNAT 100.64.0.0/10
		if v4[0] == 100 && v4[1] >= 64 && v4[1] < 128 {
			return "", fmt.Errorf("CGNAT 地址: %s", ip)
		}
	}
	return ip.String(), nil
}
