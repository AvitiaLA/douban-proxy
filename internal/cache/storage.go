package cache

import (
	"context"
	"encoding/gob"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jellydator/ttlcache/v3"
)

// MemoryStorage 内存存储实现
type MemoryStorage struct {
	cache       *ttlcache.Cache[string, *Item]
	currentSize int64
	maxSize     int64
	mutex       sync.RWMutex
}

// NewMemoryStorage 创建内存存储
func NewMemoryStorage(maxSizeMB int64) *MemoryStorage {
	cache := ttlcache.New[string, *Item](
		ttlcache.WithDisableTouchOnHit[string, *Item](),
	)

	ms := &MemoryStorage{
		cache:   cache,
		maxSize: maxSizeMB * 1024 * 1024,
	}

	// 设置淘汰回调
	cache.OnEviction(func(ctx context.Context, reason ttlcache.EvictionReason, item *ttlcache.Item[string, *Item]) {
		ms.mutex.Lock()
		ms.currentSize -= item.Value().Size
		ms.mutex.Unlock()

		// 清理缓存条目
		cacheItem := item.Value()
		cacheItem.Headers = nil
		cacheItem.Body = nil
		itemPool.Put(cacheItem)
	})

	go cache.Start()
	return ms
}

// Get 从内存获取
func (ms *MemoryStorage) Get(key string) (*Item, bool) {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()

	item := ms.cache.Get(key)
	if item == nil || item.IsExpired() {
		return nil, false
	}
	return item.Value(), true
}

// Set 设置到内存
func (ms *MemoryStorage) Set(key string, item *Item) error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	// 检查空间
	for ms.currentSize+item.Size > ms.maxSize && ms.cache.Len() > 0 {
		ms.evictOldest()
	}

	if item.Size > ms.maxSize {
		return fmt.Errorf("项目过大，无法存储")
	}

	// 检查是否已存在
	if existingItem := ms.cache.Get(key); existingItem != nil {
		ms.currentSize -= existingItem.Value().Size
	}

	ms.cache.Set(key, item, ttlcache.NoTTL)
	ms.currentSize += item.Size
	return nil
}

// Delete 从内存删除
func (ms *MemoryStorage) Delete(key string) error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	ms.cache.Delete(key)
	return nil
}

// Keys 获取所有键
func (ms *MemoryStorage) Keys() ([]string, error) {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()

	items := ms.cache.Items()
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	return keys, nil
}

// Clear 清空内存
func (ms *MemoryStorage) Clear() error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	ms.cache.DeleteAll()
	ms.currentSize = 0
	return nil
}

// Size 获取内存使用大小
func (ms *MemoryStorage) Size() (int64, error) {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()

	return ms.currentSize, nil
}

// evictOldest 淘汰最老的项目
func (ms *MemoryStorage) evictOldest() {
	items := ms.cache.Items()
	if len(items) == 0 {
		return
	}

	var oldestKey string
	var oldestTime time.Time

	for key, item := range items {
		if oldestKey == "" || item.Value().Timestamp.Before(oldestTime) {
			oldestKey = key
			oldestTime = item.Value().Timestamp
		}
	}

	if oldestKey != "" {
		ms.cache.Delete(oldestKey)
	}
}

// FileDiskStorage 基于文件的磁盘存储实现
type FileDiskStorage struct {
	baseDir string
	mutex   sync.RWMutex
}

// NewFileDiskStorage 创建新的文件磁盘存储
func NewFileDiskStorage(baseDir string) (*FileDiskStorage, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("创建缓存目录失败: %v", err)
	}
	return &FileDiskStorage{baseDir: baseDir}, nil
}

// getFilePath 获取键对应的文件路径
func (fds *FileDiskStorage) getFilePath(key string) string {
	// 使用键的前两个字符作为子目录，避免单个目录文件过多
	if len(key) >= 2 {
		subDir := key[:2]
		return filepath.Join(fds.baseDir, subDir, key+".cache")
	}
	return filepath.Join(fds.baseDir, key+".cache")
}

// Get 从磁盘获取缓存项
func (fds *FileDiskStorage) Get(key string) (*Item, bool) {
	fds.mutex.RLock()
	defer fds.mutex.RUnlock()

	filePath := fds.getFilePath(key)
	file, err := os.Open(filePath)
	if err != nil {
		return nil, false
	}
	defer file.Close()

	decoder := gob.NewDecoder(file)
	item := &Item{}
	if err := decoder.Decode(item); err != nil {
		return nil, false
	}

	return item, true
}

// Set 将缓存项保存到磁盘
func (fds *FileDiskStorage) Set(key string, item *Item) error {
	fds.mutex.Lock()
	defer fds.mutex.Unlock()

	filePath := fds.getFilePath(key)

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}

	// 创建临时文件，成功后重命名
	tempFile := filePath + ".tmp"
	file, err := os.Create(tempFile)
	if err != nil {
		return err
	}

	encoder := gob.NewEncoder(file)
	err = encoder.Encode(item)
	if err != nil {
		file.Close()
		os.Remove(tempFile)
		return err
	}

	// 确保数据写入磁盘
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(tempFile)
		return err
	}
	file.Close()

	// 原子性地重命名文件
	if err := os.Rename(tempFile, filePath); err != nil {
		os.Remove(tempFile)
		return err
	}

	return nil
}

// Delete 从磁盘删除缓存项
func (fds *FileDiskStorage) Delete(key string) error {
	fds.mutex.Lock()
	defer fds.mutex.Unlock()

	filePath := fds.getFilePath(key)
	err := os.Remove(filePath)
	if os.IsNotExist(err) {
		return nil // 文件不存在不算错误
	}
	return err
}

// Keys 获取所有缓存键
func (fds *FileDiskStorage) Keys() ([]string, error) {
	fds.mutex.RLock()
	defer fds.mutex.RUnlock()

	var keys []string
	err := filepath.WalkDir(fds.baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".cache") {
			// 从文件名提取键
			fileName := filepath.Base(path)
			key := strings.TrimSuffix(fileName, ".cache")
			keys = append(keys, key)
		}
		return nil
	})
	return keys, err
}

// Clear 清空所有缓存
func (fds *FileDiskStorage) Clear() error {
	fds.mutex.Lock()
	defer fds.mutex.Unlock()

	return os.RemoveAll(fds.baseDir)
}

// Size 计算磁盘缓存大小
func (fds *FileDiskStorage) Size() (int64, error) {
	fds.mutex.RLock()
	defer fds.mutex.RUnlock()

	var totalSize int64
	err := filepath.WalkDir(fds.baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".cache") {
			info, err := d.Info()
			if err != nil {
				return err
			}
			totalSize += info.Size()
		}
		return nil
	})
	return totalSize, err
}
