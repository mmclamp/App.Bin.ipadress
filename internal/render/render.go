// Package render 实现消息模板的占位符替换。
//
// 支持的占位符（区分大小写，全大写形式如 {IP} 同样支持）：
//
//	{ip}       公网 IP，例如 "203.0.113.7"
//	{time}     发送时间 HH:MM:SS
//	{date}     发送日期 2006-01-02
//	{hostname} 设备主机名
//
// 示例：模板 "http://{ip}:5000/xxx" 渲染为 "http://203.0.113.7:5000/xxx"。
package render

import (
	"os"
	"strings"
	"time"
)

// Vars 模板渲染所需变量。
type Vars struct {
	IP   string
	Now  time.Time
	Host string
}

// NewVars 基于 ip 与当前时间构造变量集；主机名获取失败时留空。
func NewVars(ip string, now time.Time) Vars {
	host, err := os.Hostname()
	if err != nil {
		host = ""
	}
	return Vars{IP: ip, Now: now, Host: host}
}

// Render 将 tpl 中的占位符替换为实际值，原样保留其他内容。
func Render(tpl string, v Vars) string {
	now := v.Now
	if now.IsZero() {
		now = time.Now()
	}
	r := strings.NewReplacer(
		"{ip}", v.IP,
		"{IP}", v.IP,
		"{time}", now.Format("15:04:05"),
		"{date}", now.Format("2006-01-02"),
		"{hostname}", v.Host,
	)
	return r.Replace(tpl)
}
