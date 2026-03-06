# 构建阶段
FROM golang:1.24 AS builder

WORKDIR /build

# 复制 go.mod 和 go.sum
COPY sider/go.mod sider/go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY sider/ .

# 构建二进制文件
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o sider .

# 运行阶段 - 使用 distroless 镜像（更小更安全）
FROM gcr.io/distroless/base-debian11

WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /build/sider .

EXPOSE 8080 6060

ENTRYPOINT ["/app/sider"]
CMD ["-config=/etc/sider/config.json", "-pprof=:6060"]