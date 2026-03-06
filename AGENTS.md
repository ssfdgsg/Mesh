# 仓库指南（重构）

## 概要
说明代码组织原则：运行时的旁路代理逻辑全部放到 `sider/`（及其 `internal/sider/`），中心控制/管理平面放到 `router/`。这样保持关注点分离，避免将代理运行时依赖混入控制面。

## 关键分离规则
1. 旁路代理与运行时逻辑放入 `sider/`：
    - `sider/` 是可运行命令，负责启动监听器、端口转发和连接处理。
    - `internal/sider/` 保存核心代理逻辑、JSON 配置加载和插件框架（接口 + 注册）。
    - 内置插件放在 `internal/sider/plugins/<name>/`，通过 `init()` 调用 `sider.RegisterPlugin(...)` 自注册。
2. 中心控制平面放入 `router/`：
    - `router/` 只包含控制和平面相关逻辑（配置下发、集中调度、状态查询、管理 API 等）。
    - 禁止将代理运行时 listener、连接处理或 TCP 转发逻辑放入 `router/`。

## 推荐目录概览（简短）
1. `go.mod` — 单一 Go 模块声明。
2. `sider/` — 旁路代理可执行命令（运行时、监听器、转发）。
3. `internal/sider/` — 代理核心、配置及插件框架。
4. `internal/sider/plugins/<name>/` — 内置插件目录，按小写命名并自注册。
5. `router/` — 中心控制平面（控制、配置分发、监控接口）。
6. `sider/config.example.json` — 示例运行时配置。

## 构建与运行（示例）
```bash
# 在本地使用示例配置运行代理
go run ./sider --config ./sider/config.example.json

# 运行所有测试
go test ./...

# 在 Windows 上构建可执行文件
go build -o bin/sider.exe ./sider
```

## 插件与配置要点
1. 在 JSON 配置中将插件加入 `listeners[].plugins[]` 节点。
2. 新插件应实现 `internal/sider.Plugin`（以及可选接口），并在包的 `init()` 中通过 `sider.RegisterPlugin(...)` 注册。
3. 插件代码与运行时应放在 `internal/sider/plugins/` 下，避免跨目录依赖到 `router/`。

## 开发规范（简短）
1. 格式化：使用 `gofmt` / `go fmt ./...`（可用 `goimports` 清理导入）。
2. 命名：插件目录与注册名使用小写（例如 `gray`）。
3. 错误处理：添加上下文时用 `%w` 包装；优先小而组合化接口（例如 `ConnGate`、`Router`、`ConnWrapper`）。
4. 测试：使用标准库 `testing`，倾向表驱动测试并把测试文件放在被测试包目录。

## 提交与 PR 要点
1. 清晰的提交说明（动词开头），若仓库采用 Conventional Commits 可遵循。
2. PR 中说明改动、运行命令、相关配置 JSON 片段及对网络/代理行为的风险评估。

--- 

将旁路代理实现、插件、配置和运行时放在 `sider/`，把所有集中式控制、管理与下发逻辑放在 `router/`，以保持清晰的关注点分离与可维护性。

## 核心项目约束 (Core Project Constraints)
**绝对约束：保持极致轻量，绝不引入复杂的服务网格特性。**

在协助开发与维护本项目代码时，请务必永远遵守以下界限约束。任何突破下述约束的功能点均被视为代码库污染：

1. **禁止引入动态服务发现**: 控制面与数据面决不可对接 Kubernetes API、Consul、Eureka 等。始终通过配置文件与单纯的管理 API 进行静态驱动。
2. **禁止引入分布式追踪**: 决不可在代码中引入 `go.opentelemetry.io` 或类似 SDK。
3. **禁止绑定 Kubernetes 生态**: 绝不可提供针对 Helm/CRD 的原生支持目录（即 `k8s/`）。
4. **禁止底层透明劫持**: 绝不引入 iptables/eBPF，仅保留应用显式配置代理的模式。