# 豆瓣代理服务器

一个用 Go 编写的高性能豆瓣 HTTP 代理服务器，支持智能缓存和跨域访问。

## 核心特性

- **智能缓存**：基于热门度的 LRU 缓存，支持持久化
- **跨域支持**：自动处理 CORS 头部和预检请求
- **通配符域名**：支持 `*.douban.com` 和 `*.doubanio.com`
- **模块化设计**：配置管理、缓存、处理器、中间件分离

## 快速使用

```bash
# 基础启动
./douban-proxy

# 启用缓存
./douban-proxy --enable-cache

# 自定义端口
./douban-proxy --running-port 8080
```

代理URL格式：`http://localhost:30000/豆瓣完整URL`

## 主要配置

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--running-port` | 30000 | 服务器端口 |
| `--enable-cache` | false | 启用缓存 |
| `--cache-max-size` | 100 | 缓存大小（MB） |
| `--cache-load-percent` | 80 | 启动时加载缓存百分比 |
| `--bandwidth-limit` | 0 | 带宽限制（MB/s） |
| `--request-limit` | 0 | 每IP请求限制 |

## 编译

```bash
go build -o douban-proxy main.go
```

## 项目结构

```
douban-proxy/
├── main.go                    # 主程序
├── internal/
│   ├── config/               # 配置管理
│   ├── cache/                # 缓存系统
│   ├── handler/              # HTTP处理
│   ├── middleware/           # 中间件
│   ├── proxy/                # 代理逻辑
│   └── stats/                # 统计监控
└── README.md
```

---

*仅用于学习和开发目的，请遵守豆瓣使用协议。*