#!/bin/bash
# 构建脚本：交叉编译 linux/amd64 非cgo静态二进制并组装 .fpk 包。
# 用法: ./scripts/build.sh [--fpk]
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="$(grep '^version=' package/manifest | cut -b 9-)"
echo "==> 版本: $VERSION"

echo "==> 编译 linux/amd64 静态二进制"
mkdir -p package/app/bin
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -trimpath \
  -ldflags "-s -w -X ipaddress/internal/version.Version=${VERSION}" \
  -o package/app/bin/ipaddressd \
  ./cmd/ipaddressd
chmod +x package/app/bin/ipaddressd

if [ "${1:-}" = "--fpk" ]; then
  echo "==> 打包 .fpk"
  FNPACK_BIN="${FNPACK_BIN:-scripts/fnpack}"
  if ! command -v "$FNPACK_BIN" >/dev/null 2>&1; then
    echo "错误: 未找到 fnpack。请下载对应平台二进制放到 scripts/fnpack：" >&2
    echo "      curl -o scripts/fnpack https://static2.fnnas.com/fnpack/fnpack-1.2.3-darwin-arm64 && chmod +x scripts/fnpack" >&2
    echo "      或通过环境变量 FNPACK_BIN=/绝对路径/fnpack 指定。" >&2
    exit 1
  fi
  mkdir -p dist
  "$FNPACK_BIN" build --directory package
  # fnpack 产出位置可能因版本而异，统一收集到 dist/（跳过 dist 自身，取最新）
  found="$(find . -maxdepth 1 -name '*.fpk' 2>/dev/null | head -1 || true)"
  if [ -n "$found" ] && [ "$(dirname "$found")" != "dist" ]; then
    mkdir -p dist
    mv "$found" dist/
    echo "==> 已移动到 dist/"
  fi
  ls -la dist/ 2>/dev/null || true
fi
echo "==> 完成"
