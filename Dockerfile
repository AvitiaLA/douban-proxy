# 第一阶段：构建
FROM golang:1.25 AS builder

WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 go build -o douban-proxy main.go

# 第二阶段：运行
FROM alpine:3.20
WORKDIR /app
COPY --from=builder /app/douban-proxy .
EXPOSE 30000
CMD ["./douban-proxy", "--enable-cache", "--port", "30000"]
