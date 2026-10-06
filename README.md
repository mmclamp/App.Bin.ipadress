# IP 通知（ipaddress）— 飞牛 fnOS 原生应用

定时获取 NAS 的**公网 IP**，通过**飞书 / 钉钉机器人 Webhook** 推送通知。Go 语言实现的 fnOS **原生应用**（非 Docker），通过**统一网关**提供 Web 配置页，复用 NAS 登录态鉴权。

## 功能

- ✅ 公网 IP 探测：多源依序 fallback（ipip.net → ip.sb → 3322.net → ipify），校验拒绝私网/CGNAT/环回地址
- ✅ Cron 定时推送（5 段式表达式，配置保存后立即生效）
- ✅ 消息模板：`{ip}`、`{date}`、`{time}`、`{hostname}`，例如 `http://{ip}:5000/xxx` 仅替换 IP
- ✅ 多渠道：飞书 webhook（可选加签）、**飞书自建应用**（tenant_access_token 自动缓存/刷新）、钉钉（可选加签），界面增删改 + 单渠道测试发送
- ✅ Web 配置页：状态 / 设置 / 渠道 / 日志，走 fnOS 统一网关（`/app/ipaddress`），仅管理员可改配置
- ✅ CLI：`ipaddressd send` 可挂 fnOS 计划任务；`serve` 子命令用于本机开发

## 项目结构

```
├── cmd/ipaddressd/        # 主程序（daemon/send/serve/version 子命令)
├── internal/
│   ├── config/            # 配置读写校验（config.json）
│   ├── ipprobe/           # 公网 IP 多源探测
│   ├── channel/           # 渠道接口 + 飞书/钉钉实现（扩展点）
│   ├── render/            # 模板占位符替换
│   ├── scheduler/         # cron 调度（robfig/cron/v3）
│   ├── server/            # Web UI + JSON API（Unix Socket / TCP）
│   ├── state/             # 运行状态持久化（state.json）
│   ├── logstore/          # 环形日志（内存+文件）
│   ├── webui/             # 内嵌静态页面（go:embed）
│   └── version/
├── package/               # fnOS .fpk 包目录
│   ├── manifest           # appname=ipaddress platform=x86 os_min_version=1.1.3100
│   ├── config/privilege   # run-as=package 专用应用用户
│   ├── cmd/main           # start/stop/status（pidfile，官方 Native 案例模式）
│   └── app/ui/config      # 统一网关入口 → ${TRIM_APPDEST}/app.sock
├── scripts/               # build.sh / dev.sh / genicon.go
└── Makefile
```

## 开发

```bash
make test          # 单元测试（CGO_ENABLED=0）
make build         # 交叉编译 linux/amd64 → package/app/bin/ipaddressd
./scripts/dev.sh   # 本机开发模式 http://127.0.0.1:8949
```

> 本机为 Apple Silicon + macOS 27 时，测试需 `CGO_ENABLED=0 GOTOOLCHAIN=go1.22.12`（Makefile 已内置），否则 dyld 报 `missing LC_UUID`。

## 打包 .fpk

fnpack 官方下载（v1.2.3，按平台选择）：

```
https://static2.fnnas.com/fnpack/fnpack-1.2.3-darwin-arm64
https://static2.fnnas.com/fnpack/fnpack-1.2.3-linux-amd64
```

```bash
curl -o /tmp/fnpack https://static2.fnnas.com/fnpack/fnpack-1.2.3-darwin-arm64
chmod +x /tmp/fnpack
FNPACK_BIN=/tmp/fnpack ./scripts/build.sh --fpk
# 产物: dist/ipaddress.fpk
```

打包校验要求包内必须存在：manifest、LICENSE、ICON.PNG、ICON_256.PNG、app/、cmd/、wizard/。

## 安装到飞牛 NAS

1. 把 `dist/ipaddress.fpk` 上传到 NAS
2. fnOS 桌面 → **应用中心** → 手动安装 → 选择 .fpk
3. 桌面出现「IP 通知」图标（仅管理员可见），点击打开配置页
4. 添加渠道（飞书群自定义机器人 Webhook，可选加签密钥）→ 点「测试」→ 群里收到消息即成功
5. 设置 Cron 表达式（默认 `0 9 * * *` 每天 9 点）与模板（默认 `{ip}`）

升级重新打包后在应用中心覆盖安装；配置与状态在 `@appconf`/`@appdata`，升级不丢失。

## 模板占位符

| 占位符 | 含义 | 示例 |
|---|---|---|
| `{ip}` | 公网 IP | `203.0.113.7` |
| `{date}` | 日期 | `2026-10-06` |
| `{time}` | 时间 | `09:00:00` |
| `{hostname}` | 主机名 | `fnos` |

若探测到 IPv6，拼接 `ip:port` 形式 URL 时请写成 `http://[{ip}]:5000/xxx`（方括号需自己加，`{ip}` 始终替换为裸地址）。

## 渠道类型

| 类型 | 凭证 | 说明 |
|---|---|---|
| `feishu` 飞书机器人 | webhook + 可选加签 | 群内"自定义机器人"，即用即得；官方 V1 webhook 形态（未停维护） |
| `feishu-app` 飞书自建应用 | App ID + App Secret + 接收 ID | 官方主推形态：OpenAPI + tenant_access_token（2h，自动缓存/刷新/失效重试）。接收 ID `oc_` 开头发群聊、`ou_` 开头发单聊 |
| `dingtalk` 钉钉机器人 | webhook + 可选加签 | 安全设置选"加签"时填密钥 |

**feishu-app 配置步骤**（飞书开放平台 → 开发者后台）：
1. 创建企业自建应用，凭证页拿 App ID / App Secret
2. 添加应用能力 → 机器人；权限管理 → 开通"以应用的身份发消息"
3. 创建版本并发布（自建企业测试可免审）
4. 把应用机器人添加进目标群；`chat_id` 可调 `GET /open-apis/im/v1/chats`（权限：获取群组信息）查询
5. 本应用渠道配置填入三项 → 点"测试"验证

常见错误提示已内置：`230013` 机器人不在群里、`99991672` 未开权限、`99992355` 频控等。

## 渠道扩展

新增渠道类型四步：

1. `internal/channel/` 下实现 `Channel` 接口（`Type() string` + `Send(ctx, msg) error`）
2. 在 `channel.New` 工厂注册类型
3. `internal/config` 的 `SupportedTypes` 加入新类型 + `ValidateChannel` 按类型校验
4. Web UI 下拉框加选项（必要时补充按类型显隐的表单字段）

## CLI 一次性推送（可挂 fnOS 计划任务）

```bash
/var/apps/ipaddress/target/app/bin/ipaddressd send \
  --config /var/apps/ipaddress/etc/config.json \
  --data /var/apps/ipaddress/var
```

## 关于飞书官方 Golang SDK（事件订阅）的研究结论

官方教程 [处理事件（Golang SDK）](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-events) 讲的是**事件订阅**——应用**接收**飞书侧事件（如收到群消息），需要：

- 在开放平台创建**企业自建应用**（app_id + app_secret），订阅事件
- 两种接收方式：**长连接**（`larkws` WebSocket，免公网 IP/域名，官方推荐，仅自建应用，≤50 连接）或 HTTP 回调服务器（需公网 URL）
- 关键约束：长连接模式 `dispatcher.NewEventDispatcher("", "")` 两个参数必须为空串；`cli.Start(ctx)` 阻塞运行；事件须在 **3 秒内**处理完，否则触发超时重推；集群部署时事件只随机推给一个实例

**与本应用的关系**：本应用是「自定义机器人 webhook」**推送**场景，只需往群机器人地址 POST JSON，不需要 SDK。若将来想做**反向交互**（例如在飞书群里 @机器人 发 "ip" 即回复当前 IP、发 "test" 手动触发推送），就启用事件订阅：在 NAS 上用**长连接模式**最合适（无需公网地址，只需出站网络），依赖 `github.com/larksuite/oapi-sdk-go/v3`，作为 daemon 内的一个 goroutine 挂载即可。已预留的渠道抽象与 server 生命周期可以直接承载该功能。

## 说明

- 网络模式：应用为 fnOS 原生进程，出站直连（fnOS 目前无系统级代理接口，官方已记录该需求）
- 统一网关要求 fnOS ≥ 1.1.3100；Socket 方式无需声明 service_port
- 钉钉机器人安全设置选择「加签」时填入密钥；选择「自定义关键词」时模板需包含该关键词
