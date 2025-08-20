package handler

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"douban-proxy/internal/cache"
	"douban-proxy/internal/config"
	"douban-proxy/internal/proxy"
	"douban-proxy/internal/stats"

	"github.com/go-chi/render"
	R "github.com/juju/ratelimit"
	L "github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
)

var (
	log = L.NewDefaultFactory(
		context.Background(),
		L.Formatter{
			BaseTime:        time.Now(),
			FullTimestamp:   true,
			TimestampFormat: "-0700 2006-01-02 15:04:05",
		},
		os.Stdout,
		"handler",
		nil,
		false,
	).Logger()
	client           *http.Client
	cacheManager     *cache.Manager
	bandwidthLimiter *R.Bucket
)

// HTTPError HTTP错误结构
type HTTPError struct {
	Message string `json:"message"`
	Example string `json:"example"`
}

func (e *HTTPError) Error() string {
	return e.Message
}

// NewError 创建新的HTTP错误
func NewError(msg string) *HTTPError {
	return &HTTPError{
		Message: msg,
		Example: "https://abc.com/https://m.douban.com/movie/1234567",
	}
}

// InitHTTPClient 初始化HTTP客户端
func InitHTTPClient() {
	client = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			MaxIdleConns:        100,              // 最大空闲连接数
			MaxIdleConnsPerHost: 10,               // 每个主机最大空闲连接数
			IdleConnTimeout:     90 * time.Second, // 空闲连接超时
			DisableCompression:  true,             // 禁用压缩，让反向代理处理
		},
		Timeout: 30 * time.Second, // 请求超时30秒
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// InitCache 初始化缓存
func InitCache() {
	if config.Global.EnableCache {
		cacheManager = cache.NewManager(config.Global.CacheMemorySize, config.Global.CacheDir, log, config.Global.CacheCleanupHours)
		log.Debug("Cache enabled: memory=", config.Global.CacheMemorySize, "MB, disk=", config.Global.CacheDir, ", cleanup interval: ", config.Global.CacheCleanupHours, "h")
	}
}

// InitBandwidthLimiter 初始化带宽限制器
func InitBandwidthLimiter(limitMB int) {
	if limitMB > 0 {
		bandwidthLimiter = R.NewBucketWithRate(float64(limitMB*1024*1024), int64(limitMB*1024*1024))
		log.Debug("Bandwidth limit: ", limitMB, "MB/s")
	}
}

// Hello 主页处理器
func Hello(w http.ResponseWriter, r *http.Request) {
	render.Status(r, http.StatusOK)
	render.PlainText(w, r, "Hello to visit douban-proxy")
}

// LimitReader 带宽限制读取器
type LimitReader struct {
	reader io.Reader
	bucket *R.Bucket
}

// NewLimitReader 创建新的限制读取器
func NewLimitReader(reader io.Reader, bucket *R.Bucket) *LimitReader {
	return &LimitReader{
		reader: reader,
		bucket: bucket,
	}
}

func (lr *LimitReader) Read(p []byte) (int, error) {
	sliceLen := int64(len(p))
	available := lr.bucket.TakeAvailable(sliceLen)
	if available == 0 {
		return 0, nil
	}
	if available == sliceLen {
		return lr.reader.Read(p)
	}
	temp := make([]byte, available)
	defer copy(p, temp)
	return lr.reader.Read(temp)
}

// Final 最终处理器
func Final(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats.Global.IncRequest()

	// 获取原始URI，去掉开头的"/"
	rawURI := r.URL.RequestURI()[1:]

	// 处理favicon和其他静态资源请求
	if rawURI == "favicon.ico" || strings.HasPrefix(rawURI, "favicon.") {
		faviconURL, _ := url.Parse(fmt.Sprintf("https://%s/favicon.ico", config.Global.FaviconDomain))
		log.DebugContext(ctx, "Proxy favicon: ", faviconURL)
		sendRequestWithURL(w, r, faviconURL)
		return
	}

	// 尝试URL解码
	decodedURI, err := url.QueryUnescape(rawURI)
	if err != nil {
		decodedURI = rawURI
		log.DebugContext(ctx, "Decode failed, raw URI: ", rawURI)
	}

	requestURIURL, err := url.Parse(decodedURI)
	if err != nil {
		responseWithError(w, r, E.Cause(err, "Parse request uri as url"))
		return
	}

	if proxy.IsDomainAccepted(requestURIURL.Host) {
		log.DebugContext(ctx, "Proxy to: ", requestURIURL.Host, requestURIURL.Path)
		sendRequestWithURL(w, r, requestURIURL)
		return
	}

	if r.Referer() != "" {
		rawRefererURL, err := url.Parse(r.Referer())
		if err != nil {
			responseWithError(w, r, E.Cause(err, "Parse referer url"))
			return
		}
		refererURL, err := url.Parse(rawRefererURL.RequestURI()[1:])
		if err != nil {
			responseWithError(w, r, E.Cause(err, "Parse referer url request uri as url"))
			return
		}
		if proxy.IsDomainAccepted(refererURL.Host) {
			finalURL, err := refererURL.Parse(r.URL.RequestURI())
			if err != nil {
				responseWithError(w, r, E.Cause(err, "Parse request uri as path with referer url"))
				return
			}
			responseWithRedirect(w, r, finalURL)
			return
		}
	}

	if requestURIURL.Scheme == "" {
		responseWithError(w, r, E.New("URL scheme request"))
		return
	}
	if requestURIURL.Host == "" {
		responseWithError(w, r, E.New("URL host request"))
		return
	}
	responseWithError(w, r, E.New("Unsupported url host"))
}

// responseWithError 错误响应
func responseWithError(w http.ResponseWriter, r *http.Request, err error) {
	log.ErrorContext(r.Context(), err)
	render.Status(r, http.StatusInternalServerError)
	render.JSON(w, r, NewError(err.Error()))
}

// responseWithRedirect 重定向响应
func responseWithRedirect(w http.ResponseWriter, r *http.Request, URL *url.URL) {
	log.DebugContext(r.Context(), "Redirect: ", r.URL.RequestURI(), " -> /", URL.String())
	w.Header().Set("Location", "/"+URL.String())
	w.WriteHeader(http.StatusTemporaryRedirect)
}

// SaveCache 保存缓存
func SaveCache() error {
	if cacheManager != nil {
		if err := cacheManager.SaveToFile(""); err != nil {
			return err
		}
		memCount, memSize, diskCount, diskSize := cacheManager.GetStats()
		log.Debug("Cache saved: memory=", memCount, " items (", memSize/(1024*1024), "MB), disk=", diskCount, " items (", diskSize/(1024*1024), "MB)")
	}
	return nil
}

// StopCache 停止缓存
func StopCache() {
	if cacheManager != nil {
		cacheManager.Stop()
	}
}

// sendRequestWithURL 发送请求到指定URL
func sendRequestWithURL(w http.ResponseWriter, r *http.Request, URL *url.URL) {
	ctx := r.Context()

	// 生成缓存键并处理请求去重
	var cacheKey string
	var shouldFetch bool
	if cacheManager != nil {
		cacheKey = cache.GenerateKey(r.Method, URL.String(), r.Header)

		// 使用无锁获取或开始获取
		if cacheItem, found, shouldStart := cacheManager.GetOrStartFetch(cacheKey); found {
			stats.Global.IncCacheHit() // 统计缓存命中
			sizeKB := float64(len(cacheItem.Body)) / 1024
			requests, hits, _, _, hitRate := stats.Global.GetStats()
			// 获取当前缓存状态
			memCount, memSize, diskCount, diskSize := cacheManager.GetStats()
			cacheCount := memCount + diskCount
			currentCacheSize := memSize + diskSize
			log.DebugContext(ctx, "Cache HIT: ", URL, " | ", fmt.Sprintf("%.1fKB", sizeKB), " | Stats: ", requests, " reqs, ", hits, " hits (", fmt.Sprintf("%.1f%%", hitRate), ") | Cache: ", cacheCount, " items, ", fmt.Sprintf("%.1fMB", float64(currentCacheSize)/(1024*1024)))

			// 设置响应头（从缓存中恢复）
			for key, values := range cacheItem.Headers {
				w.Header().Del(key)
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}

			// 添加缓存标识头
			w.Header().Set("X-Cache", "HIT")
			w.Header().Set("X-Cache-Date", cacheItem.Timestamp.Format(time.RFC3339))

			// 移除可能导致问题的头部
			w.Header().Del("Set-Cookie")
			w.Header().Del("Authorization")

			w.WriteHeader(cacheItem.StatusCode)
			w.Write(cacheItem.Body)
			return
		} else if shouldStart {
			// 需要开始新的请求
			shouldFetch = true
			stats.Global.IncCacheMiss() // 统计缓存未命中
			requests, hits, _, _, hitRate := stats.Global.GetStats()
			// 获取当前缓存状态
			memCount, memSize, diskCount, diskSize := cacheManager.GetStats()
			cacheCount := memCount + diskCount
			currentCacheSize := memSize + diskSize
			log.DebugContext(ctx, "Cache MISS: ", URL, " | Stats: ", requests, " reqs, ", hits, " hits (", fmt.Sprintf("%.1f%%", hitRate), ") | Cache: ", cacheCount, " items, ", fmt.Sprintf("%.1fMB", float64(currentCacheSize)/(1024*1024)))
		} else {
			// 有其他请求正在处理相同资源且已完成，但结果为空（可能失败了）
			stats.Global.IncCacheMiss()
			requests, hits, _, _, hitRate := stats.Global.GetStats()
			memCount, memSize, diskCount, diskSize := cacheManager.GetStats()
			cacheCount := memCount + diskCount
			currentCacheSize := memSize + diskSize
			log.DebugContext(ctx, "Cache WAIT FAILED: ", URL, " | Stats: ", requests, " reqs, ", hits, " hits (", fmt.Sprintf("%.1f%%", hitRate), ") | Cache: ", cacheCount, " items, ", fmt.Sprintf("%.1fMB", float64(currentCacheSize)/(1024*1024)))
			// 继续执行请求，但不使用去重机制
			shouldFetch = false
		}
	} else {
		shouldFetch = false
	}

	request, err := http.NewRequest(r.Method, URL.String(), r.Body)
	if err != nil {
		responseWithError(w, r, E.Cause(err, "Build request"))
		return
	}

	// 改进的请求头透传 - 复制所有头部，除了特定的代理相关头部
	skipHeaders := map[string]bool{
		"Host":             true,
		"Connection":       true,
		"Proxy-Connection": true,
		"Upgrade":          true,
		"Te":               true,
		"Trailer":          true,
	}

	for key, values := range r.Header {
		if skipHeaders[key] {
			continue
		}
		request.Header.Del(key)
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}

	// 设置正确的Host头
	request.Host = URL.Host

	// 为豆瓣API添加必要的默认头部（如果请求中没有的话且启用了默认头部）
	if config.Global.AddDefaultHeaders {
		if request.Header.Get("User-Agent") == "" {
			request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
		}

		if request.Header.Get("Accept") == "" {
			request.Header.Set("Accept", "application/json, text/plain, */*")
		}

		if request.Header.Get("Accept-Language") == "" {
			request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
		}

		if request.Header.Get("Referer") == "" && URL.Host == "m.douban.com" {
			request.Header.Set("Referer", "https://m.douban.com/")
		}
	}

	response, err := client.Do(request)
	if err != nil {
		responseWithError(w, r, E.Cause(err, "Send request"))
		return
	}
	defer response.Body.Close()

	// 智能缓存策略：只缓存小于10MB的响应
	const maxCacheSize = 10 * 1024 * 1024 // 10MB
	var responseBody []byte
	var shouldCache bool

	if cacheManager != nil && response.StatusCode == http.StatusOK {
		// 检查Content-Length
		contentLength := response.ContentLength
		if contentLength > 0 && contentLength <= maxCacheSize {
			shouldCache = true
			responseBody, err = io.ReadAll(response.Body)
			if err != nil {
				responseWithError(w, r, E.Cause(err, "Read response body"))
				return
			}
			response.Body.Close()
		} else if contentLength <= 0 {
			// 没有Content-Length，边读边判断
			buffer := bytes.NewBuffer(nil)
			limitReader := io.LimitReader(response.Body, maxCacheSize+1)
			n, err := io.Copy(buffer, limitReader)
			if err != nil && err != io.EOF {
				responseWithError(w, r, E.Cause(err, "Read response body"))
				return
			}

			if n <= maxCacheSize {
				shouldCache = true
				responseBody = buffer.Bytes()
				response.Body.Close()
			} else {
				// 大于10MB，不缓存，直接流式传输
				shouldCache = false
				// 重新创建response.Body，包含已读取的数据
				response.Body = io.NopCloser(io.MultiReader(buffer, response.Body))
			}
		} else {
			// 大文件，不缓存
			shouldCache = false
		}
	}

	isRedirectResponse := common.Any([]int{http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect}, func(it int) bool {
		return it == response.StatusCode
	})

	// 复制响应头，过滤掉不应该缓存的头部
	responseHeaders := make(http.Header)
	skipCacheHeaders := map[string]bool{
		"Set-Cookie":    true,
		"Authorization": true,
		"Date":          true,
		"Expires":       true,
		"Last-Modified": true,
		"Etag":          true,
		"Cache-Control": true,
		"Pragma":        true,
		"Vary":          true,
	}

	for key, values := range response.Header {
		w.Header().Del(key)
		for _, value := range values {
			if key == "Content-Security-Policy" {
				var policies []string
				for _, policy := range strings.Split(value, "; ") {
					policies = append(policies, strings.ReplaceAll(policy, `'none'`, `'self'`)+" "+URL.Host)
				}
				value = strings.Join(policies, "; ")
			} else if isRedirectResponse && key == "Location" && len(value) > 0 && []rune(value)[0] != '/' {
				if locationURL, err := url.Parse(value); err == nil && proxy.IsDomainAccepted(locationURL.Host) {
					value = "/" + value
				}
			}
			w.Header().Add(key, value)

			// 只缓存不依赖于请求的头部
			if !skipCacheHeaders[key] {
				responseHeaders.Add(key, value)
			}
		}
	}

	// 添加缓存标识头
	if cacheManager != nil {
		w.Header().Set("X-Cache", "MISS")
	}

	w.WriteHeader(response.StatusCode)

	// 写入响应体并处理缓存
	if cacheManager != nil && shouldCache && responseBody != nil {
		// 检测缓存类型
		cacheType := cache.DetectCacheType(response.Header)

		// 使用内存池创建缓存响应条目
		cacheItem := cache.NewItem()
		now := time.Now()
		*cacheItem = cache.Item{
			StatusCode: response.StatusCode,
			Headers:    responseHeaders,
			Body:       responseBody,
			Timestamp:  now,
			Size:       int64(len(responseBody)),
			CacheType:  cacheType, // 设置缓存类型
		}
		// 使用方法设置访问统计
		cacheItem.SetLastAccessed(now)
		cacheItem.SetAccessCount(1) // 首次创建时访问计数为1

		// 如果是通过去重机制开始的请求，使用 CompleteFetch
		if shouldFetch {
			cacheManager.CompleteFetch(cacheKey, cacheItem, nil)
		} else {
			// 普通请求直接设置缓存
			cacheManager.Set(cacheKey, cacheItem)
		}

		stats.Global.AddDataSize(int64(len(responseBody))) // 统计数据大小

		sizeKB := float64(len(responseBody)) / 1024
		log.DebugContext(ctx, "Cache STORED: ", URL, " | Type: ", cacheType.String(), " | Size: ", fmt.Sprintf("%.1fKB", sizeKB), " | Key: ", cacheKey[:8], "...")

		if bandwidthLimiter != nil {
			io.Copy(w, NewLimitReader(bytes.NewReader(responseBody), bandwidthLimiter))
		} else {
			w.Write(responseBody)
		}
	} else {
		// 如果缓存失败但是通过去重机制开始的请求，需要通知其他等待的请求
		if cacheManager != nil && shouldFetch {
			var err error
			if response.StatusCode != http.StatusOK {
				err = fmt.Errorf("HTTP %d", response.StatusCode)
			}
			cacheManager.CompleteFetch(cacheKey, nil, err)
		}

		if cacheManager != nil && response.StatusCode != http.StatusOK {
			log.DebugContext(ctx, "Skip cache status ", response.StatusCode, ": ", URL)
		}
		if bandwidthLimiter != nil {
			io.Copy(w, NewLimitReader(response.Body, bandwidthLimiter))
		} else {
			io.Copy(w, response.Body)
		}
	}

	log.DebugContext(ctx, "Proxy OK: ", URL, ", method: ", request.Method, ", status: ", response.StatusCode)
}
