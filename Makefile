# 飞牛 fnOS 原生应用「IP 通知」
GO ?= go
ENV := GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0

.PHONY: test build dev clean fpk

# 单元测试
test:
	$(ENV) go test ./...

# 交叉编译 linux/amd64（产物 package/app/bin/ipaddressd）
build:
	./scripts/build.sh

# 打包 .fpk（fnpack 随项目在 scripts/fnpack）
fpk: build
	./scripts/build.sh --fpk

# 本机开发模式（TCP 8949）：make dev CONFIG=./dev-config.json
dev:
	$(ENV) go build -o /tmp/ipaddressd-dev ./cmd/ipaddressd
	./scripts/dev.sh $(CONFIG)

clean:
	rm -rf dist package/app/bin/ipaddressd
