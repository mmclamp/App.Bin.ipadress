#!/bin/bash
# 本机开发模式：构建当前平台二进制并启动开发服务器。
# 用法: ./scripts/dev.sh [配置文件路径]
set -euo pipefail
cd "$(dirname "$0")/.."

ENV="GOTOOLCHAIN=go1.22.12 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0"
BIN=/tmp/ipaddressd-dev
DATA=/tmp/ipaddress-dev-data

mkdir -p "$DATA"
if [ ! -f "$DATA/config.json" ]; then
  cp package/app/config.default.json "$DATA/config.json"
  echo "==> 已创建开发配置 $DATA/config.json"
fi

echo "==> 构建"
env $ENV go build -o "$BIN" ./cmd/ipaddressd

echo "==> 启动 http://127.0.0.1:8949"
"$BIN" serve --addr 127.0.0.1:8949 --config "${1:-$DATA/config.json}" --data "$DATA"
