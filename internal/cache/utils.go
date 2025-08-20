package cache

import (
	"crypto/md5"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// 内存池 - 减少GC压力
var (
	// 缓存条目对象池
	itemPool = sync.Pool{
		New: func() interface{} {
			return &Item{}
		},
	}

	// 字符串构建器池
	stringBuilderPool = sync.Pool{
		New: func() interface{} {
			return &strings.Builder{}
		},
	}
)

// NewItem 从内存池创建新的缓存项
func NewItem() *Item {
	return itemPool.Get().(*Item)
}

// GenerateKey 生成缓存键
func GenerateKey(method, urlStr string, headers http.Header) string {
	// 从池中获取字符串构建器，用完后归还
	keyBuilder := stringBuilderPool.Get().(*strings.Builder)
	defer func() {
		keyBuilder.Reset()
		stringBuilderPool.Put(keyBuilder)
	}()

	// 解析URL以提取路径和查询参数
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		// 如果解析失败，使用完整URL作为后备
		keyBuilder.WriteString(method)
		keyBuilder.WriteByte(':')
		keyBuilder.WriteString(urlStr)
		hash := md5.Sum([]byte(keyBuilder.String()))
		return fmt.Sprintf("%x", hash)
	}

	// 使用内存池的strings.Builder优化字符串拼接
	keyBuilder.WriteString(method)
	keyBuilder.WriteByte(':')
	keyBuilder.WriteString(parsedURL.Path)
	if parsedURL.RawQuery != "" {
		keyBuilder.WriteByte('?')
		keyBuilder.WriteString(parsedURL.RawQuery)
	}

	hash := md5.Sum([]byte(keyBuilder.String()))
	return fmt.Sprintf("%x", hash)
}

// GetLastAccessed 获取最后访问时间
func (item *Item) GetLastAccessed() time.Time {
	ns := atomic.LoadInt64(&item.lastAccessedNs)
	if ns == 0 {
		return item.Timestamp
	}
	return time.Unix(0, ns)
}

// SetLastAccessed 设置最后访问时间
func (item *Item) SetLastAccessed(t time.Time) {
	atomic.StoreInt64(&item.lastAccessedNs, t.UnixNano())
}

// GetAccessCount 获取访问次数
func (item *Item) GetAccessCount() int {
	return int(atomic.LoadInt32(&item.accessCount))
}

// IncAccessCount 增加访问次数
func (item *Item) IncAccessCount() {
	atomic.AddInt32(&item.accessCount, 1)
}

// SetAccessCount 设置访问次数
func (item *Item) SetAccessCount(count int) {
	atomic.StoreInt32(&item.accessCount, int32(count))
}
