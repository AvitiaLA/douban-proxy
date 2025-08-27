# 第一阶段：构建
FROM golang:1.25 AS builder
WORKDIR /app

# 复制 go.mod 和 go.sum 下载依赖
COPY go.mod go.sum ./
RUN go mod download

# 复制源码
COPY . .

# 构建二进制文件
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o douban-proxy main.go

# 第二阶段：运行
FROM alpine:3.20
WORKDIR /app

# 复制构建好的二进制
COPY --from=builder /app/douban-proxy .

# 暴露默认端口（保留即可）
EXPOSE 30000

# 启动程序，自动读取环境变量 PORT
CMD ["sh", "-c", "./douban-proxy --enable-cache"]
