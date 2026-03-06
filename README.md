# mesh: sider (旁路代理) + router (控制平面)

mesh 是一个轻量级的“数据面 + 控制面”组合：
- `sider` 负责运行时代理与连接转发
- `router` 负责配置分发、管理 API 与可选 UI

本仓库把运行时代理与控制平面严格拆分，避免两者互相耦合。

## 功能说明

- 旁路代理（`sider/`）
  - 多监听、多上游的 TCP 转发
  - QUIC 字节流转发（可选）
  - 插件链扩展：`ConnGate` / `Router` / `ConnWrapper`
  - 配置热更新（本地文件 + 控制面 SSE）
  - 可选 `pprof` 调试
- 控制平面（`router/`）
  - 从本地配置文件加载并规范化为 canonical JSON
  - SSE 广播配置给所有 `sider` 节点
  - 轮询文件变更并自动推送
  - 管理 API + 状态接口
  - 可挂载 UI 静态资源
- UI（`web/`）
  - 健康检查、状态与配置展示
  - 配置 reload 与实时 SSE 订阅

## 技术介绍

### 架构与职责

- `sider/`：数据面运行时，可执行命令，负责监听、连接处理与转发
- `internal/sider/`：核心代理逻辑、配置解析、插件框架与内置插件
- `router/`：控制平面服务，负责配置加载、分发与管理接口
- `web/`：控制面 UI（Vite + React），可选部署

> 约束：运行时代理逻辑只放在 `sider/`（及 `internal/sider/`），控制/管理逻辑只放在 `router/`，避免跨目录耦合。

### 配置模型（JSON）

- `listeners[]`：一个进程内的多条转发规则
- `listen`/`upstreams`/`plugins`：单 listener 的兼容字段
- `dial_timeout_ms`：上游拨号超时（默认 3000ms）
- `listen_network`/`upstream_network`：`tcp`（默认）或 `quic`
- `listen_tls`/`upstream_tls`：QUIC 所需 TLS 配置

示例（最小化）：

```json
{
  "dial_timeout_ms": 3000,
  "listeners": [
    {
      "listen": ":8080",
      "upstreams": ["127.0.0.1:9000"],
      "plugins": []
    }
  ]
}
```

QUIC 示例：`sider/config.quic.example.json`

### 连接处理链路

每条连接的处理流程：
1. `Accept()` 新连接并构建 `ConnInfo`
2. 依次调用 `ConnGate.AllowConn(...)` 决策是否放行
3. 依次调用 `Router.SelectUpstream(...)` 决定上游
4. `DialContext()` 连接上游
5. 通过 `ConnWrapper.Wrap(...)` 包装两侧连接
6. `io.CopyBuffer` 双向转发并支持半关闭

### 控制面配置流

`router` 会读取配置文件并转成 canonical JSON，通过 SSE 推送给已连接的 `sider`：
- 启动时广播一次，保证新连接可立即获取
- 按 `--poll` 轮询文件元信息（mtime/size）触发 reload
- 提供手动 `push` 接口触发 reload + broadcast

### 关键技术细节与优化

- 内存与吞吐
  - 转发使用 32KB `sync.Pool` 复用缓冲区，降低频繁分配带来的 GC 压力
  - 双向转发使用 `io.CopyBuffer` 并支持 `CloseWrite()` 半关闭，减少连接悬挂
- 连接处理与稳定性
  - `Accept()` 临时错误采用短暂退避，降低抖动时的 CPU 占用
  - 支持 `dial_timeout_ms`，防止上游连接阻塞导致 goroutine 堆积
  - 热更新通过 `Runner.Apply()` 停旧起新，保证旧 listener 能被完整回收
- 插件执行链
  - `ConnGate` 先决策放行，`Router` 决定上游，`ConnWrapper` 做连接包装
  - 插件按配置顺序串联，保证可预测的链路行为
- 配置分发与 SSE
  - canonical JSON 使用单行格式，降低 SSE 带宽与解析成本
  - Hub 缓存 last config，新连接可立即获取最新配置
  - SSE 推送对慢客户端采用丢弃策略，避免广播阻塞
  - SSE keepalive 心跳减少代理/负载均衡缓冲导致的断流
  - `sider` 侧 SSE 自动重连，指数退避上限 30s
- QUIC 细节
  - QUIC 基于 `quic-go` 实现 raw stream 转发
  - TLS 采用 TLS1.3，内置默认 ALPN `sider-quic`
  - 上游 TLS 支持 `root_ca_file` 与自动推断 `server_name`

## 目录结构

- `sider/`：旁路代理（runtime data plane）
- `internal/sider/`：代理核心、配置、插件框架
- `internal/sider/plugins/<name>/`：内置插件（小写命名，自注册）
- `router/`：控制平面服务（配置分发与管理 API）
- `web/`：控制面 UI 源码
- `sider/config.example.json`：示例运行时配置

## 快速开始

注意：`sider/` 与 `router/` 是独立 Go module，请进入各自目录执行 Go 命令。

1) 启动一个上游服务（示例）：

```bash
go run ./sider/cmd/tcpsink --listen 127.0.0.1:9000
```

2) 启动控制面：

```bash
cd router
cp ../sider/config.example.json ./sider.config.json
go run . --addr :8081 --config ./sider.config.json
```

3) 启动代理：

```bash
cd ../sider
go run . --config ./config.example.json
# 或接入控制面（SSE 热更新）
go run . --config ./config.example.json --control-plane http://127.0.0.1:8081 --node node-1
```

## 控制面 API

- `GET /healthz`
- `GET /v1/sider/config/stream`（SSE，event=`config`）
- `POST /v1/sider/config/push`
- `GET /v1/ui/status`
- `GET /v1/ui/config`
- `POST /v1/ui/config/reload`
- `GET /v1/ui/config/stream`

## 插件

内置插件位于 `internal/sider/plugins/`，通过 `init()` 调用 `sider.RegisterPlugin(...)` 自注册。

- `gray`：基于客户端 IP 的灰度分流（stable/canary + canary_percent + salt）
- `bbr`：按观测带宽 pacing 的写入节流

扩展方式：
1) 实现 `internal/sider.Plugin`（及可选 `ConnGate`/`Router`/`ConnWrapper`）
2) 在插件包的 `init()` 中注册
3) 在配置里追加 `listeners[].plugins[]`

## UI（可选）

```bash
cd web
npm install
npm run build

cd ../router
go run . --config ./sider.config.json --ui ../web/dist
```

访问：
- `http://127.0.0.1:8081/ui/`
- `http://127.0.0.1:8081/`

## 开发与构建

- 格式化：`gofmt` / `go fmt ./...`
- 测试：`go test ./...`（在 `sider/` 或 `router/` 目录内）
- Windows 构建示例：
  - `cd sider && go build -o ../bin/sider.exe .`
  - `cd router && go build -o ../bin/router.exe .`

---

将旁路代理实现、插件、配置和运行时放在 `sider/`，把所有集中式控制、管理与下发逻辑放在 `router/`，以保持清晰的关注点分离与可维护性。

## 项目边界与非目标 (Project Scope & Non-Goals)

本项目致力于**保持极致轻量级**的纯粹 L4 代理与简单控制面架构。为避免架构膨胀与复杂化，以下高级网格特性被明确定义为**非目标 (Out of Scope)**，不再计划引入：

- **动态服务注册与发现**: 不支持对接 Kubernetes API、Consul 或 Eureka 等服务发现后端。配置将专注于静态驱动与简单 API 推送。
- **链路追踪与深度可观测性**: 不原生集成 OpenTelemetry 的全链路追踪能力。仅保留基础连接日志与最核心状态。
- **Kubernetes 深度集成**: 不支持 CRD 抽象、Helm 部署体系，也不引入自动注入 (Auto-injection) 相关的本地流量劫持。
- **复杂的 L7 路由体系**: 不构建全功能的 HTTP header/path 路由，保持为专注 TCP/QUIC 的旁路代理。
