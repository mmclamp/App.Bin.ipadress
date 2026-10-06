// Package version 保存构建时注入的应用版本号。
package version

// Version 由构建脚本通过 -ldflags 注入，默认 "dev"。
var Version = "dev"
