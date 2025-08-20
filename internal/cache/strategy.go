package cache

import (
	"net/http"
	"strings"
)

// DefaultCacheStrategy 默认缓存策略实现
type DefaultCacheStrategy struct {
	promoteThreshold float64 // 提升到内存的阈值
	evictThreshold   float64 // 从内存移除的阈值
}

// NewDefaultCacheStrategy 创建默认缓存策略
func NewDefaultCacheStrategy() *DefaultCacheStrategy {
	return &DefaultCacheStrategy{
		promoteThreshold: 3.0, // 分数超过3.0提升到内存
		evictThreshold:   1.0, // 分数低于1.0从内存移除
	}
}

// ShouldCache 判断是否应该缓存
func (dcs *DefaultCacheStrategy) ShouldCache(key string, item *Item) bool {
	// 默认所有响应都缓存到磁盘
	return item.StatusCode == 200
}

// ShouldPromoteToMemory 判断是否应该提升到内存
func (dcs *DefaultCacheStrategy) ShouldPromoteToMemory(key string, hotspotMgr HotspotManager) bool {
	return hotspotMgr.GetScore(key) >= dcs.promoteThreshold
}

// ShouldEvictFromMemory 判断是否应该从内存移除
func (dcs *DefaultCacheStrategy) ShouldEvictFromMemory(key string, hotspotMgr HotspotManager) bool {
	return hotspotMgr.GetScore(key) < dcs.evictThreshold
}

// 可长期缓存的MIME类型
var longTermCacheTypes = map[string]bool{
	"image/": true,
}

// DetectCacheType 根据HTTP头部检测缓存类型
func DetectCacheType(headers http.Header) CacheType {
	contentType := headers.Get("Content-Type")
	if contentType == "" {
		return CacheTypeOther
	}

	// 检查是否为长期缓存类型
	for prefix := range longTermCacheTypes {
		if strings.HasPrefix(contentType, prefix) {
			return CacheTypeImage // 复用现有枚举值表示长期缓存
		}
	}

	return CacheTypeOther
}
