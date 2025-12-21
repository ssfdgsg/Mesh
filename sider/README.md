# sider

`sider/` 是旁路代理（runtime data plane）：监听一个 TCP 端口，把连接转发到上游，并通过插件机制插入能力（例如灰度分流、整形/限流等）。

注意：本仓库里 `sider/` 与 `router/` 是两个独立 Go module；请在各自目录下运行 `go run` / `go test`。

## 快速开始（本地直连）

1) 准备上游服务（示例：本机 `127.0.0.1:9000` 与 `127.0.0.1:9001`）。

2) 启动 `sider`（使用示例配置）：

```bash
cd sider
go run . --config ./config.example.json
```

3) 访问监听端口（示例配置里是 `:8080`），流量会被转发到 `upstreams`。

## 配置文件

示例文件：`sider/config.example.json`（与 `router` 使用的 `sider.config.json` 同格式）。

配置要点：

- `listeners[].listen`：本地监听地址（例如 `:8080`）
- `listeners[].upstreams`：上游地址列表（例如 `127.0.0.1:9000`）
- `listeners[].plugins`：插件列表（按顺序执行）
- `dial_timeout_ms`：拨号超时（默认 3000ms）

## 命令行参数

- `--config`：本地配置文件路径（默认 `./sider/config.example.json`）
- `--pprof`：开启 pprof（例如 `:6060`，空则关闭）
- `--control-plane`：控制面地址（开启配置流，例如 `http://127.0.0.1:8081`）
- `--node`：可选 node id（用于控制面区分节点）

## 接入控制面（从 router 拉取配置）

1) 启动控制面（见 `router/README.md`）。

2) 启动 `sider` 并开启配置流：

```bash
cd sider
go run . --config ./config.example.json --control-plane http://127.0.0.1:8081 --node node-1
```

`sider` 会先加载 `--config` 作为初始配置，然后订阅控制面的 SSE 配置流；收到更新后会热应用（Apply）新配置。

## 内置插件

- `gray`：灰度发布（基于客户端 IP 的确定性哈希，按百分比分流到 canary）
- `bbr`：基于观测带宽的自适应 pacing（对转发写入进行整形，可作为“BBR 限流”的可插拔实现入口）

## 扩展方式（新增插件）

1) 实现 `sider/internal/sider` 包里的 `Plugin` 接口，以及需要的可选接口（`Router`/`ConnGate`/`ConnWrapper`）。
2) 在插件包的 `init()` 里调用 `sider.RegisterPlugin("your_name", func() sider.Plugin { ... })` 自注册。
3) 在配置里追加 `listeners[].plugins[]` 项。
