package cache

import (
	"net/http"
	"time"
)

// Storage 存储接口 - 统一磁盘和内存存储
type Storage interface {
	Get(key string) (*Item, bool)
	Set(key string, item *Item) error
	Delete(key string) error
	Keys() ([]string, error)
	Clear() error
	Size() (int64, error)
}

// HotspotManager 热点管理器接口
type HotspotManager interface {
	// 记录访问
	RecordAccess(key string)
	// 判断是否为热点
	IsHotspot(key string) bool
	// 获取热点分数
	GetScore(key string) float64
	// 获取最冷的键
	GetColdestKeys(limit int) []string
	// 清理过期统计
	Cleanup()
}

// CacheStrategy 缓存策略接口
type CacheStrategy interface {
	// 判断是否应该缓存
	ShouldCache(key string, item *Item) bool
	// 判断是否应该提升到内存
	ShouldPromoteToMemory(key string, hotspotMgr HotspotManager) bool
	// 判断是否应该从内存移除
	ShouldEvictFromMemory(key string, hotspotMgr HotspotManager) bool
}

// CacheType 缓存类型
type CacheType int

const (
	CacheTypeImage CacheType = iota // 图片缓存
	CacheTypeOther                  // 其他缓存
)

// String 返回缓存类型的字符串表示
func (ct CacheType) String() string {
	switch ct {
	case CacheTypeImage:
		return "LongTerm"
	case CacheTypeOther:
		return "ShortTerm"
	default:
		return "Unknown"
	}
}

// Item 缓存条目
type Item struct {
	StatusCode     int
	Headers        http.Header
	Body           []byte
	Timestamp      time.Time // 创建时间
	lastAccessedNs int64     // 最后访问时间（纳秒时间戳，用于原子操作）
	accessCount    int32     // 访问次数（用于原子操作）
	Size           int64
	CacheType      CacheType // 缓存类型
}

// InFlightRequest 正在处理的请求信息
type InFlightRequest struct {
	doneChan chan struct{}
	result   *Item
	err      error
}
