// Package ipprobe 获取本机公网 IPv6：直接读取本机网卡上的全局地址。
// IPv6 无 NAT，网卡上的全局地址即公网可达地址，无需再请求外部检测服务。
package ipprobe

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// PublicIP 读取本机全部网卡，返回首个公网 IPv6 字符串。
func PublicIP(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", fmt.Errorf("枚举网卡地址失败: %w", err)
	}
	return FirstGlobalIPv6(addrs)
}

// FirstGlobalIPv6 从地址列表中返回首个公网 IPv6。
// 仅接受全局单播 2000::/3；ULA(fc00::/7)、链路本地(fe80::/10)、
// IPv4/IPv4 映射、环回、组播等均不在该范围或被排除。
// 多个候选时按列表顺序（即 net.InterfaceAddrs 的接口顺序）取第一个。
func FirstGlobalIPv6(addrs []net.Addr) (string, error) {
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipnet.IP
		if ip.To4() != nil || !ip.IsGlobalUnicast() || !isGlobalV6(ip) {
			continue
		}
		return ip.String(), nil
	}
	return "", errors.New("未在本机网卡上找到公网 IPv6 地址（NAS 可能未获得全局 IPv6）")
}

// isGlobalV6 判断是否落在 IANA 全局单播 2000::/3 内。
func isGlobalV6(ip net.IP) bool {
	return len(ip) == net.IPv6len && ip[0]&0xE0 == 0x20
}
