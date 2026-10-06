// Package webui 内嵌 Web 配置页静态资源。
package webui

import "embed"

// FS 内嵌的静态文件根目录（static/）。
//
//go:embed static
var FS embed.FS
