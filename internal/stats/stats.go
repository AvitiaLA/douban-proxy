package stats

import (
	"fmt"
	"sync"
)

// ProxyStats 简单的性能统计
type ProxyStats struct {
	RequestCount  int64
	CacheHits     int64
	CacheMisses   int64
	TotalDataSize int64
	mutex         sync.RWMutex
}

// IncRequest 增加请求计数
func (ps *ProxyStats) IncRequest() {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	ps.RequestCount++
}

// IncCacheHit 增加缓存命中计数
func (ps *ProxyStats) IncCacheHit() {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	ps.CacheHits++
}

// IncCacheMiss 增加缓存未命中计数
func (ps *ProxyStats) IncCacheMiss() {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	ps.CacheMisses++
}

// AddDataSize 添加数据大小
func (ps *ProxyStats) AddDataSize(size int64) {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	ps.TotalDataSize += size
}

// GetStats 获取统计信息
func (ps *ProxyStats) GetStats() (requests, hits, misses, totalSize int64, hitRate float64) {
	ps.mutex.RLock()
	defer ps.mutex.RUnlock()
	
	hitRate = 0
	if ps.RequestCount > 0 {
		hitRate = float64(ps.CacheHits) / float64(ps.RequestCount) * 100
	}
	
	return ps.RequestCount, ps.CacheHits, ps.CacheMisses, ps.TotalDataSize, hitRate
}

// String 返回统计信息的字符串表示
func (ps *ProxyStats) String() string {
	requests, hits, _, dataSize, hitRate := ps.GetStats()
	return fmt.Sprintf("Requests: %d, Cache hits: %d, Hit rate: %.1f%%, Data: %dMB", 
		requests, hits, hitRate, dataSize/(1024*1024))
}

// Global 全局统计实例
var Global = &ProxyStats{}
