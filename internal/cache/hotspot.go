package cache

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// AccessStats 访问统计信息
type AccessStats struct {
	count        int32     // 访问次数
	lastAccessed time.Time // 最后访问时间
	score        float64   // 热点分数
}

// SimpleHotspotManager 简单热点管理器实现
type SimpleHotspotManager struct {
	stats        map[string]*AccessStats
	mutex        sync.RWMutex
	hotThreshold float64 // 热点阈值
}

// NewSimpleHotspotManager 创建简单热点管理器
func NewSimpleHotspotManager(hotThreshold float64) *SimpleHotspotManager {
	return &SimpleHotspotManager{
		stats:        make(map[string]*AccessStats),
		hotThreshold: hotThreshold,
	}
}

// RecordAccess 记录访问
func (shm *SimpleHotspotManager) RecordAccess(key string) {
	shm.mutex.Lock()
	defer shm.mutex.Unlock()

	stats, exists := shm.stats[key]
	if !exists {
		stats = &AccessStats{
			count:        0,
			lastAccessed: time.Now(),
		}
		shm.stats[key] = stats
	}

	atomic.AddInt32(&stats.count, 1)
	stats.lastAccessed = time.Now()

	// 重新计算分数
	stats.score = shm.calculateScore(stats)
}

// IsHotspot 判断是否为热点
func (shm *SimpleHotspotManager) IsHotspot(key string) bool {
	return shm.GetScore(key) >= shm.hotThreshold
}

// GetScore 获取热点分数
func (shm *SimpleHotspotManager) GetScore(key string) float64 {
	shm.mutex.RLock()
	defer shm.mutex.RUnlock()

	stats, exists := shm.stats[key]
	if !exists {
		return 0
	}
	return stats.score
}

// GetColdestKeys 获取最冷的键
func (shm *SimpleHotspotManager) GetColdestKeys(limit int) []string {
	shm.mutex.RLock()
	defer shm.mutex.RUnlock()

	type keyScore struct {
		key   string
		score float64
	}

	var scores []keyScore
	for key, stats := range shm.stats {
		scores = append(scores, keyScore{key, stats.score})
	}

	// 按分数升序排列（最冷的在前）
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score < scores[j].score
	})

	// 取前limit个
	if limit > len(scores) {
		limit = len(scores)
	}

	result := make([]string, limit)
	for i := 0; i < limit; i++ {
		result[i] = scores[i].key
	}
	return result
}

// Cleanup 清理过期统计
func (shm *SimpleHotspotManager) Cleanup() {
	shm.mutex.Lock()
	defer shm.mutex.Unlock()

	now := time.Now()
	for key, stats := range shm.stats {
		// 清理7天未访问的统计
		if now.Sub(stats.lastAccessed) > 7*24*time.Hour {
			delete(shm.stats, key)
		}
	}
}

// calculateScore 计算分数
func (shm *SimpleHotspotManager) calculateScore(stats *AccessStats) float64 {
	now := time.Now()

	// 访问次数权重
	accessWeight := float64(atomic.LoadInt32(&stats.count))

	// 最近访问时间权重（越近越热门）
	hoursSinceLastAccess := now.Sub(stats.lastAccessed).Hours()
	recencyWeight := 1.0 / (1.0 + hoursSinceLastAccess/24.0) // 1天后权重减半

	// 综合分数
	return accessWeight * recencyWeight
}
