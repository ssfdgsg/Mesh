# Mesh Project Context (claude.md)

## 项目概述 (Project Overview)
`mesh` 是一个轻量级的“数据面 + 控制面”组合代理系统。仓库采用了严格的组件拆分架构，以保证代理运行时与控制逻辑的解耦。

## 架构拆分与核心职责 (Architecture & Responsibilities)

整个项目主要包含三个核心部分：

1. **`sider/` (数据面 / 旁路代理)**
   - **职责**: 负责实际的运行时网络连接处理、连接转发。
   - **特性**: 
     - 支持多监听、多上游的 TCP 与 QUIC 流转发。
     - 支持插件链扩展（`ConnGate` 拦截、`Router` 路由决策、`ConnWrapper` 连接包装）。
     - 支持配置热更新（通过本地文件监控或控制平面 SSE 下发）。
   - **目录**: `internal/sider/` 存放核心代理逻辑、配置解析和插件框架。内置插件存放在 `internal/sider/plugins/<name>/`。

2. **`router/` (控制平面)**
   - **职责**: 负责配置的管理、加载与分发。
   - **特性**:
     - 从本地文件加载配置并规范化（Canonical JSON）。
     - 通过 SSE (Server-Sent Events) 实时广播配置到所有已连接的 `sider` 节点。
     - 提供状态查询和管理 API，并支持挂载外部静态 UI 资源。

3. **`web/` (用户界面 UI)**
   - **职责**: 基于 Vite + React 编写的前端控制台。
   - **特性**: 展示当前状态、健康检查以及配置实时展示与重载操作。

## 核心开发规则 (Critical Development Rules) ⚠️
作为 AI 开发助手，在进行代码修改时需严格遵守以下规则：

1. **绝对隔离**: 运行时代理逻辑 **只能** 放在 `sider/` 下。集中式的控制、管理、配置下发 API 逻辑 **只能** 放在 `router/` 下。**绝不可跨目录造成依赖耦合**。
2. **独立模块**: `sider/` 和 `router/` 是两个**独立的 Go Module**。在执行 `go build`、`go test` 或添加依赖 `go get` 时，请务必先 `cd` 到对应的子目录中。不要在根目录下执行所有 Go 构建命令（根目录只有占位用的 `go.mod`）。
3. **插件开发规范**:
   - 新插件应实现在 `sider/internal/sider/plugins/<plugin_name>/` 下（目录名小写）。
   - 插件必须实现 `internal/sider.Plugin` 以及相关的拦截器接口 (`ConnGate`/`Router`/`ConnWrapper`)。
   - 在插件包的 `init()` 函数中通过 `sider.RegisterPlugin(...)` 进行自注册。

## 构建与运行命令示例 (Build & Run Commands)

- **Sider 测试与运行**:
  ```bash
  cd sider
  go test ./...
  go build -o ../bin/sider.exe .
  go run . --config ./config.example.json
  ```

- **Router 测试与运行**:
  ```bash
  cd router
  go test ./...
  go build -o ../bin/router.exe .
  go run . --addr :8081 --config ./sider.config.json
  ```

- **Web 构建**:
  ```bash
  cd web
  npm install
  npm run build
  ```

## 配置模型 (Configuration Model)
配置主要为 JSON 结构，核心节点为 `listeners` 数组，每个对象包含：
- `listen`: 监听地址 (如 `":8080"`)
- `upstreams`: 上游节点数组 (如 `["127.0.0.1:9000"]`)
- `plugins`: 该监听器启用的插件参数配置。
- 其他网络参数如 `dial_timeout_ms`, `listen_network` (tcp/quic) 等。

## 特性边界与非目标 (Feature Scope & Non-Goals)

当前 `sider` + `router` 架构搭建了一个出色的、轻量级的 L4 数据面与纯粹控制面基础骨架。为了避免架构膨胀，本项目明确定义了以下高级特性为**绝对非目标 (Strictly Out of Scope)**。在后续任何的代码演进中，坚决不应考虑引入这些特性：

1. **动态服务注册与发现 (Dynamic Service Discovery)**
   - **被排拒原因**: 引入对 Kubernetes API、Consul、Eureka 的依赖会大幅度拉高运行时资源与二进制体积。当前系统只接受通过 API 或配置文件加载纯静态的端点列表。

2. **全局链路追踪与 OpenTelemetry (Distributed Tracing)**
   - **被排拒原因**: 注入 Trace ID 与产生 span 会导致可观的内存分配开销，同时引入复杂的 SDK。本项目维持现状，仅记录最基本的 L4 连接日志与状态。

3. **Kubernetes 原生支持与环境绑定 (Kubernetes CRD & Helm)**
   - **被排拒原因**: 避免将自身局限在 Kubernetes 生态之内，排斥诸如透明流量劫持 (iptables) 与复杂的声明式调和控制器 (Reconciler)。

4. **冗余的 七层 (L7) 协议解析**
   - **被排拒原因**: 高级 HTTP 控制、重试机制、复杂路由等交由下游应用层或独立的专职中间件完成，本系统专注于快速、精简的 TCP/QUIC 后端转发。

*注意：本文件旨在为所有 AI 助手提供项目上下文边界和绝对技术纪律，不可替代完整的 `README.md` 中的使用说明。*
