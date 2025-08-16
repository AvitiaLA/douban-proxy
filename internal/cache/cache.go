package cache

import (
	"context"
	"crypto/md5"
	"encoding/gob"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jellydator/ttlcache/v3"
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

// Item 缓存条目
type Item struct {
	StatusCode   int
	Headers      http.Header
	Body         []byte
	Timestamp    time.Time // 创建时间
	LastAccessed time.Time // 最后访问时间
	AccessCount  int       // 访问次数
	Size         int64
}

// Manager 缓存管理器
type Manager struct {
	cache       *ttlcache.Cache[string, *Item]
	currentSize int64
	maxSize     int64
	mutex       sync.RWMutex
	// 维护一个按热门度排序的优先队列，避免每次都要全量计算
	lowScoreKeys []string // 保存低分数的key，用于快速淘汰
}

// NewManager 创建新的缓存管理器
func NewManager(maxSizeMB int64, defaultExpiration time.Duration) *Manager {
	cache := ttlcache.New[string, *Item](
		// 不设置条目数量限制，只基于大小
		ttlcache.WithDisableTouchOnHit[string, *Item](), // 禁用访问时更新，让LRU更纯粹
	)

	cm := &Manager{
		cache:   cache,
		maxSize: maxSizeMB * 1024 * 1024, // 转换为字节
	}

	// 设置淘汰回调来跟踪缓存大小
	cache.OnEviction(func(ctx context.Context, reason ttlcache.EvictionReason, item *ttlcache.Item[string, *Item]) {
		cm.mutex.Lock()
		cm.currentSize -= item.Value().Size
		cm.mutex.Unlock()

		// 清理缓存条目并归还到内存池
		cacheItem := item.Value()
		cacheItem.Headers = nil // 释放headers map
		cacheItem.Body = nil    // 释放body切片
		itemPool.Put(cacheItem)
	})

	// 启动自动清理（用于处理容量淘汰）
	go cache.Start()

	return cm
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

// Get 获取缓存项
func (cm *Manager) Get(key string) (*Item, bool) {
	// 先用读锁检查是否存在
	cm.mutex.RLock()
	item := cm.cache.Get(key)
	if item == nil || item.IsExpired() {
		cm.mutex.RUnlock()
		return nil, false
	}
	cacheItem := item.Value()
	cm.mutex.RUnlock()

	// 异步更新访问统计，避免阻塞读取
	go func() {
		cm.mutex.Lock()
		// 再次检查item是否还存在（可能被其他goroutine删除）
		if currentItem := cm.cache.Get(key); currentItem != nil && !currentItem.IsExpired() {
			currentItem.Value().LastAccessed = time.Now()
			currentItem.Value().AccessCount++
		}
		cm.mutex.Unlock()
	}()

	return cacheItem, true
}

// Set 设置缓存项
func (cm *Manager) Set(key string, item *Item) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	// 如果单个项目大小超过最大缓存大小，则不缓存
	if item.Size > cm.maxSize {
		return
	}

	// 检查是否需要清理缓存以腾出空间
	for cm.currentSize+item.Size > cm.maxSize && cm.cache.Len() > 0 {
		cm.evictOldest()
	}

	// 检查是否已存在该键
	if existingItem := cm.cache.Get(key); existingItem != nil {
		cm.currentSize -= existingItem.Value().Size
	}

	cm.cache.Set(key, item, ttlcache.NoTTL)
	cm.currentSize += item.Size
}

// evictOldest 淘汰最老的缓存项
func (cm *Manager) evictOldest() {
	// 优化：使用批量淘汰策略，减少频繁调用
	items := cm.cache.Items()
	if len(items) == 0 {
		return
	}

	// 当缓存条目超过1000个时，批量计算并缓存低分数的key
	if len(items) > 1000 && len(cm.lowScoreKeys) == 0 {
		cm.updateLowScoreKeys(items)
	}

	// 优先从低分数key列表中删除
	if len(cm.lowScoreKeys) > 0 {
		// 从列表末尾取出一个（分数最低的）
		keyToDelete := cm.lowScoreKeys[len(cm.lowScoreKeys)-1]
		cm.lowScoreKeys = cm.lowScoreKeys[:len(cm.lowScoreKeys)-1]

		if item := cm.cache.Get(keyToDelete); item != nil {
			cm.currentSize -= item.Value().Size
			cm.cache.Delete(keyToDelete)
			return
		}
	}

	// 回退到原有逻辑（小缓存或lowScoreKeys为空时）
	var oldestKey string
	var lowestScore float64 = -1

	for key, item := range items {
		score := calculateHotScore(item.Value())
		if lowestScore < 0 || score < lowestScore {
			oldestKey = key
			lowestScore = score
		}
	}

	if oldestKey != "" {
		if item := cm.cache.Get(oldestKey); item != nil {
			cm.currentSize -= item.Value().Size
		}
		cm.cache.Delete(oldestKey)
	}
}

// updateLowScoreKeys 批量计算并缓存低分数的keys
func (cm *Manager) updateLowScoreKeys(items map[string]*ttlcache.Item[string, *Item]) {
	type keyScore struct {
		key   string
		score float64
	}

	var scores []keyScore
	for key, item := range items {
		score := calculateHotScore(item.Value())
		scores = append(scores, keyScore{key, score})
	}

	// 按分数排序，低分在后
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	// 取后20%作为低分数key（最多200个）
	lowCount := len(scores) / 5
	if lowCount > 200 {
		lowCount = 200
	}
	if lowCount < 10 {
		lowCount = len(scores) / 2 // 小缓存时取一半
	}

	cm.lowScoreKeys = make([]string, 0, lowCount)
	for i := len(scores) - lowCount; i < len(scores); i++ {
		cm.lowScoreKeys = append(cm.lowScoreKeys, scores[i].key)
	}
}

// GetStats 获取统计信息
func (cm *Manager) GetStats() (count int, currentSize, maxSize int64) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	return cm.cache.Len(), cm.currentSize, cm.maxSize
}

// Stop 停止缓存管理器
func (cm *Manager) Stop() {
	cm.cache.Stop()
}

// SaveToFile 将缓存保存到文件
func (cm *Manager) SaveToFile(filename string) error {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	// 创建临时文件名，成功后重命名，避免写入过程中程序崩溃导致文件损坏
	tempFile := filename + ".tmp"

	file, err := os.Create(tempFile)
	if err != nil {
		return err
	}

	encoder := gob.NewEncoder(file)

	// 收集所有缓存数据
	cacheData := make(map[string]*Item)
	items := cm.cache.Items()
	for key, item := range items {
		cacheData[key] = item.Value()
	}

	err = encoder.Encode(cacheData)
	if err != nil {
		file.Close()
		os.Remove(tempFile)
		return err
	}

	// 确保文件完全写入并关闭
	err = file.Sync() // 强制写入磁盘
	if err != nil {
		file.Close()
		os.Remove(tempFile)
		return err
	}
	file.Close() // 显式关闭文件

	// Windows下需要先删除目标文件再重命名
	if _, err := os.Stat(filename); err == nil {
		// 目标文件存在，先删除
		if err := os.Remove(filename); err != nil {
			os.Remove(tempFile) // 清理临时文件
			return err
		}
	}

	// 重命名临时文件为最终文件名
	err = os.Rename(tempFile, filename)
	if err != nil {
		os.Remove(tempFile) // 重命名失败，清理临时文件
		return err
	}

	return nil
}

// calculateHotScore 计算缓存项的热门度分数
// 综合考虑访问次数、最近访问时间和缓存年龄
func calculateHotScore(item *Item) float64 {
	now := time.Now()

	// 访问次数权重（越多越热门）
	accessWeight := float64(item.AccessCount)

	// 最近访问时间权重（越近越热门）
	hoursSinceLastAccess := now.Sub(item.LastAccessed).Hours()
	recencyWeight := 1.0 / (1.0 + hoursSinceLastAccess/24.0) // 1天后权重减半

	// 缓存年龄权重（避免过老的缓存占用空间）
	hoursSinceCreated := now.Sub(item.Timestamp).Hours()
	ageWeight := 1.0 / (1.0 + hoursSinceCreated/(24.0*7.0)) // 1周后权重减半

	// 综合分数：访问次数 × 最近访问权重 × 年龄权重
	score := accessWeight * recencyWeight * ageWeight

	return score
}

// LoadFromFile 从文件加载缓存（优化版本）
func (cm *Manager) LoadFromFile(filename string, loadPercent int) error {
	file, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 文件不存在不是错误
		}
		return err
	}
	defer file.Close()

	decoder := gob.NewDecoder(file)
	cacheData := make(map[string]*Item)

	err = decoder.Decode(&cacheData)
	if err != nil {
		return err
	}

	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	// 按访问时间排序，优先加载最近访问的缓存
	type CacheEntry struct {
		Key  string
		Item *Item
	}

	var entries []CacheEntry
	for key, item := range cacheData {
		entries = append(entries, CacheEntry{Key: key, Item: item})
	}

	// 按热门程度排序（综合考虑访问次数和最近访问时间）
	sort.Slice(entries, func(i, j int) bool {
		scoreI := calculateHotScore(entries[i].Item)
		scoreJ := calculateHotScore(entries[j].Item)
		return scoreI > scoreJ // 降序排列，热门的在前
	})

	// 只加载能放入缓存的热门项目
	loadedCount := 0
	loadedSize := int64(0)
	maxLoadSize := cm.maxSize * int64(loadPercent) / 100 // 根据配置加载指定百分比的容量

	for _, entry := range entries {
		if cm.currentSize+entry.Item.Size > maxLoadSize {
			break // 达到加载限制，停止加载
		}

		cm.cache.Set(entry.Key, entry.Item, ttlcache.NoTTL)
		cm.currentSize += entry.Item.Size
		loadedCount++
		loadedSize += entry.Item.Size
	}

	return nil
}

// NewItem 从内存池创建新的缓存项
func NewItem() *Item {
	return itemPool.Get().(*Item)
}
