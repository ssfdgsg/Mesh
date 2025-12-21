# Mesh Router UI

该 UI 用于查看/编辑 `router` 管理的 `sider` 配置，并通过 `/v1/ui/*` 与 `router` 通信。

## Dev

1. Start router:

```bash
cd router
go run . --config ./sider.config.json
```

2. Start UI (Vite proxy to `localhost:8081`):

```bash
cd web
npm install
npm run dev
```

3. Open UI:

- Vite dev server: `http://127.0.0.1:5173/`
- Router hosted UI (build 后)：`http://127.0.0.1:8081/ui/`（或 `http://127.0.0.1:8081/`）

## Build for router static hosting

```bash
cd web
npm install
npm run build
```

Then serve it from router:

```bash
cd router
go run . --config ./sider.config.json --ui ../web/dist
```
