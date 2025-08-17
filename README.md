# douban-proxy

一个用 Go 编写的高性能豆瓣 HTTP 代理服务器，支持智能缓存和跨域访问。

## 核心特性

- **智能缓存**：基于热门度的 LRU 缓存，支持持久化
- **跨域支持**：自动处理 CORS 头部和预检请求
- **通配符域名**：支持 `*.douban.com` 和 `*.doubanio.com`

## 快速使用

### 直接运行

```bash
# 基础启动
douban-proxy

# 查看帮助
douban-proxy -h

# 启用缓存
douban-proxy --enable-cache
```

代理URL格式：`http://localhost:30000/豆瓣完整URL`

### 服务模式（推荐）

```bash
# 安装系统服务
douban-proxy service install --enable-cache

# 启动服务
douban-proxy service start

# 查看服务状态
douban-proxy service status
```

---

*仅用于学习和开发目的，请遵守豆瓣使用协议。*