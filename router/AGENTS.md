# Repository Guidelines

## 项目边界

`router/` 是中心路由控制平面（control plane），与 `sider/` **不互相依赖**。任何需要共享的代码请在两个项目目录中各自维护一份副本，避免跨目录引用。

## Project Structure

- `router/main.go`：`router` 入口（控制平面服务/CLI 的主程序）。

## Build & Test

- `cd router; go run .`：本地运行 `router`。
- `cd router; go test ./...`：运行 `router` 单元测试（如未来新增测试）。

## Coding Style

- Go 代码遵循 `gofmt`；导出符号使用 `CamelCase`，包名使用小写。

