# Repository Guidelines

## 项目边界

本仓库包含两个**互不依赖**的 Go 项目：

- `sider/`：旁路代理（L4/TCP 端口转发 + 插件机制）
- `router/`：中心路由控制平面（control plane）

不要在两个项目之间直接 `import` 代码；如需共享实现，请在两个目录内各自放一份相同文件（显式复制），避免形成隐式耦合。

## Project Structure

- `sider/main.go`：`sider` 入口。
- `sider/internal/sider/`：代理核心、配置加载、插件接口与注册表。
- `sider/internal/sider/plugins/<name>/`：内置插件（如 `gray`、`bbr`）。
- `sider/config.example.json`：示例配置。

## Build & Test

- `cd sider; go run . --config ./config.example.json`：本地运行 `sider`。
- `cd sider; go test ./...`：运行 `sider` 单元测试。
- `cd sider; go test ./... -race`：竞态检测（建议在合并前跑一次）。
- `cd sider; go build -o bin/sider .`：构建二进制（Windows 输出为 `bin/sider.exe`）。

## Coding Style

- 使用 `gofmt`（或 `go fmt ./...`）格式化；保持 import 整洁（可选 `goimports`）。
- 新插件：实现 `internal/sider.Plugin`，按需实现 `ConnGate`/`Router`/`ConnWrapper`，并在 `init()` 中调用 `sider.RegisterPlugin("name", ...)`。

