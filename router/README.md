# router (control plane)

`router/` 是控制/管理平面：从本地配置文件加载 `sider` 运行时配置，并通过 SSE 将配置推送给所有已连接的 `sider` 节点（`sider --control-plane ...`）。

注意：本仓库里 `sider/` 与 `router/` 是两个独立 Go module；请在各自目录下运行 `go run` / `go test`。

## Quick start

1) 准备一份 sider 配置文件（默认路径：`router/sider.config.json`）。

你可以直接复制 `sider/config.example.json` 作为起点：

```bash
cd router
cp ../sider/config.example.json ./sider.config.json
```

2) 启动 router：

```bash
cd router
go run . --addr :8081 --config ./sider.config.json
```

3) （可选）让 sider 订阅配置流：

```bash
cd sider
go run . --config ./config.example.json --control-plane http://127.0.0.1:8081 --node node-1
```

4) 修改 `router/sider.config.json` 后，触发一次广播（reload + push）：

```bash
curl -X POST http://127.0.0.1:8081/v1/sider/config/push
```

## HTTP endpoints

- `GET /healthz`
- `GET /v1/sider/config/stream` (SSE, event=`config`)
- `POST /v1/sider/config/push`
- `GET /v1/ui/status`
- `GET /v1/ui/config`
- `POST /v1/ui/config/reload`
- `GET /v1/ui/config/stream` (alias of sider stream)

## Run with UI

Build UI:

```bash
cd ../web
npm install
npm run build
```

Run router and serve UI at `/ui/`:

```bash
cd ../router
go run . --config ./sider.config.json --ui ../web/dist
```

UI 入口：

- `http://127.0.0.1:8081/ui/`
- `http://127.0.0.1:8081/ui`
- `http://127.0.0.1:8081/`（同一份 UI 的别名入口）
