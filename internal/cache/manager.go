package cache

import (
	"fmt"
	"sync"
	"time"

	L "github.com/sagernet/sing-box/log"
)

// Manager 缓存管理器 - 使用组合模式协调各个组件
type Manager struct {
	// 存储层 - 可以独立替换
	memoryStorage Storage
	diskStorage   Storage

	// 管理层 - 可以独立替换
	hotspotMgr HotspotManager
	strategy   CacheStrategy

	// 并发控制
	inFlightMutex sync.Mutex
	inFlight      map[string]*InFlightRequest

	// 系统控制
	logger   L.Logger
	stopChan chan struct{}
}

// NewManager 创建新的缓存管理器
// memorySizeMB: 内存缓存大小（MB），用于存储热点数据
// cacheDir: 磁盘缓存目录
func NewManager(memorySizeMB int64, cacheDir string, logger L.Logger, cleanupHours int) *Manager {
	// 创建存储层
	memoryStorage := NewMemoryStorage(memorySizeMB)

	diskStorage, err := NewFileDiskStorage(cacheDir)
	if err != nil {
		logger.Error("创建磁盘存储失败: ", err)
		diskStorage = nil
	}

	// 创建管理层
	hotspotMgr := NewSimpleHotspotManager(3.0) // 热点阈值3.0
	strategy := NewDefaultCacheStrategy()

	cm := &Manager{
		memoryStorage: memoryStorage,
		diskStorage:   diskStorage,
		hotspotMgr:    hotspotMgr,
		strategy:      strategy,
		inFlight:      make(map[string]*InFlightRequest),
		logger:        logger,
		stopChan:      make(chan struct{}),
	}

	// 启动定期清理任务
	go cm.startPeriodicTasks(cleanupHours)

	logger.Info(fmt.Sprintf("缓存管理器初始化完成 - 内存: %dMB, 磁盘: %s",
		memorySizeMB, cacheDir))

	return cm
}

// Get 获取缓存项 - 协调内存和磁盘存储
func (cm *Manager) Get(key string) (*Item, bool) {
	// 首先检查内存缓存
	if item, found := cm.memoryStorage.Get(key); found {
		// 记录访问
		cm.hotspotMgr.RecordAccess(key)
		return item, true
	}

	// 内存中没有，检查磁盘缓存
	if cm.diskStorage != nil {
		if item, found := cm.diskStorage.Get(key); found {
			// 记录访问
			cm.hotspotMgr.RecordAccess(key)

			// 检查是否应该提升到内存缓存
			if cm.strategy.ShouldPromoteToMemory(key, cm.hotspotMgr) {
				cm.promoteToMemory(key, item)
			}

			return item, true
		}
	}

	return nil, false
}

// Set 设置缓存项 - 协调存储策略
func (cm *Manager) Set(key string, item *Item) {
	// 检查是否应该缓存
	if !cm.strategy.ShouldCache(key, item) {
		return
	}

	// 默认存储到磁盘
	if cm.diskStorage != nil {
		if err := cm.diskStorage.Set(key, item); err != nil {
			cm.logger.Error("磁盘缓存写入失败: ", err)
		}
	}

	// 如果是热点数据，也存储到内存
	if cm.hotspotMgr.IsHotspot(key) {
		if err := cm.memoryStorage.Set(key, item); err != nil {
			cm.logger.Debug("内存缓存写入失败: ", err)
		}
	}
}

// GetOrStartFetch 获取缓存项或开始获取过程（去重）
func (cm *Manager) GetOrStartFetch(key string) (*Item, bool, bool) {
	// 首先尝试从缓存获取
	if item, found := cm.Get(key); found {
		return item, true, false
	}

	// 检查是否有正在进行的请求
	cm.inFlightMutex.Lock()
	if inFlightReq, exists := cm.inFlight[key]; exists {
		// 有正在进行的请求，等待其完成
		cm.inFlightMutex.Unlock()
		<-inFlightReq.doneChan
		if inFlightReq.result != nil && inFlightReq.err == nil {
			return inFlightReq.result, true, false
		}
		return nil, false, false
	}

	// 没有正在进行的请求，创建一个新的
	inFlightReq := &InFlightRequest{
		doneChan: make(chan struct{}),
	}
	cm.inFlight[key] = inFlightReq
	cm.inFlightMutex.Unlock()

	return nil, false, true
}

// CompleteFetch 完成获取过程并存储结果
func (cm *Manager) CompleteFetch(key string, item *Item, err error) {
	cm.inFlightMutex.Lock()
	inFlightReq, exists := cm.inFlight[key]
	if exists {
		inFlightReq.result = item
		inFlightReq.err = err
		delete(cm.inFlight, key)
		close(inFlightReq.doneChan)
	}
	cm.inFlightMutex.Unlock()

	// 如果成功获取到数据，存储到缓存
	if item != nil && err == nil {
		cm.Set(key, item)
	}
}

// GetStats 获取统计信息
func (cm *Manager) GetStats() (memoryCount int, memorySize int64, diskCount int, diskSize int64) {
	// 获取内存统计
	memoryKeys, _ := cm.memoryStorage.Keys()
	memoryCount = len(memoryKeys)
	memorySize, _ = cm.memoryStorage.Size()

	// 获取磁盘统计
	if cm.diskStorage != nil {
		diskKeys, _ := cm.diskStorage.Keys()
		diskCount = len(diskKeys)
		diskSize, _ = cm.diskStorage.Size()
	}

	return
}

// Stop 停止缓存管理器
func (cm *Manager) Stop() {
	close(cm.stopChan) // 停止定期清理协程

	// 清理所有正在进行的请求
	cm.inFlightMutex.Lock()
	for key, req := range cm.inFlight {
		req.err = fmt.Errorf("cache manager stopped")
		close(req.doneChan)
		delete(cm.inFlight, key)
	}
	cm.inFlightMutex.Unlock()
}

// SaveToFile 保存缓存（磁盘缓存已实时保存，此方法用于兼容性）
func (cm *Manager) SaveToFile(filename string) error {
	cm.logger.Debug("磁盘缓存已实时保存，无需额外保存操作")
	return nil
}

// LoadFromFile 加载缓存（磁盘缓存已自动加载，此方法用于兼容性）
func (cm *Manager) LoadFromFile(filename string, loadPercent int) error {
	cm.logger.Debug("磁盘缓存已自动加载，无需额外加载操作")
	return nil
}

// CleanupAfterLoad 兼容性方法
func (cm *Manager) CleanupAfterLoad() {
	cm.logger.Debug("新架构无需启动后清理")
}

// promoteToMemory 将数据提升到内存缓存
func (cm *Manager) promoteToMemory(key string, item *Item) {
	// 尝试添加到内存存储
	if err := cm.memoryStorage.Set(key, item); err != nil {
		cm.logger.Debug("提升到内存失败: ", err)
	} else {
		cm.logger.Debug("数据提升到内存: ", key)
	}
}

// startPeriodicTasks 启动定期任务
func (cm *Manager) startPeriodicTasks(cleanupHours int) {
	if cleanupHours <= 0 {
		cm.logger.Debug("定期清理间隔无效，跳过定期任务")
		return
	}

	ticker := time.NewTicker(time.Duration(cleanupHours) * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 清理热点统计
			cm.hotspotMgr.Cleanup()

			// 检查内存中的冷数据，移除到磁盘
			cm.evictColdDataFromMemory()

		case <-cm.stopChan:
			return
		}
	}
}

// evictColdDataFromMemory 从内存中移除冷数据
func (cm *Manager) evictColdDataFromMemory() {
	memoryKeys, _ := cm.memoryStorage.Keys()

	var evictedCount int
	for _, key := range memoryKeys {
		if cm.strategy.ShouldEvictFromMemory(key, cm.hotspotMgr) {
			cm.memoryStorage.Delete(key)
			evictedCount++
		}
	}

	if evictedCount > 0 {
		cm.logger.Info(fmt.Sprintf("从内存移除 %d 个冷数据项", evictedCount))
	}
}
