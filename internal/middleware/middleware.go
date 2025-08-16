package middleware

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"douban-proxy/internal/config"

	L "github.com/sagernet/sing-box/log"
	M "github.com/sagernet/sing/common/metadata"
)

var log = L.NewDefaultFactory(
	context.Background(),
	L.Formatter{
		BaseTime:        time.Now(),
		FullTimestamp:   true,
		TimestampFormat: "-0700 2006-01-02 15:04:05",
	},
	os.Stdout,
	"middleware",
	nil,
	false,
).Logger()

// CORS 跨域处理中间件
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 只有启用CORS时才设置CORS头部
		if config.Global.EnableCORS {
			// 设置CORS头部
			origin := r.Header.Get("Origin")
			if origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			} else {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, HEAD, PATCH")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Accept-Language, Content-Type, Cookie, Authorization, X-Requested-With, Origin, Referer, User-Agent, Cache-Control, Pragma, If-Modified-Since, If-None-Match")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Type, Cache-Control, ETag, Last-Modified, X-Cache, X-Cache-Date")
			w.Header().Set("Access-Control-Max-Age", "86400") // 24小时

			// 处理预检请求
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// SetContext 设置请求上下文
func SetContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(L.ContextWithNewID(r.Context())))
	})
}

// CommonLog 通用日志中间件
func CommonLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.InfoContext(r.Context(), "New ", r.Method, " request from ", r.RemoteAddr, " to ", r.URL.RequestURI())
		next.ServeHTTP(w, r)
	})
}

// IPLimiter IP限制器
type IPLimiter struct {
	records map[string]*AccessRecord
	limit   int
	mutex   sync.RWMutex
}

// AccessRecord 访问记录
type AccessRecord struct {
	count int
}

// NewIPLimiter 创建新的IP限制器
func NewIPLimiter(limit int) *IPLimiter {
	return &IPLimiter{
		records: make(map[string]*AccessRecord),
		limit:   limit,
	}
}

// GetAccess 获取访问权限
func (ir *IPLimiter) GetAccess(address string) bool {
	// 先读取检查
	ir.mutex.RLock()
	_, exist := ir.records[address]
	ir.mutex.RUnlock()

	if exist {
		// 存在记录，原子性检查和增加计数
		ir.mutex.Lock()
		// 双检查：再次确认记录仍然存在且未达到限制
		if record, stillExist := ir.records[address]; stillExist && record.count < ir.limit {
			record.count++
			ir.mutex.Unlock()
			return true
		}
		ir.mutex.Unlock()
		return false
	} else {
		// 不存在记录，需要创建
		ir.mutex.Lock()
		// 双检查：可能其他goroutine已经创建了
		if _, nowExist := ir.records[address]; !nowExist {
			ir.records[address] = &AccessRecord{1}
			ir.mutex.Unlock()
			return true
		}
		// 已存在，递归调用（很少发生）
		ir.mutex.Unlock()
		return ir.GetAccess(address)
	}
}

// Leave 离开（减少计数）
func (ir *IPLimiter) Leave(address string) {
	ir.mutex.RLock()
	record, exist := ir.records[address]
	ir.mutex.RUnlock()
	if exist {
		if record.count == 1 {
			ir.mutex.Lock()
			delete(ir.records, address)
			ir.mutex.Unlock()
		} else {
			record.count = record.count - 1
		}
	}
}

var limiter *IPLimiter

// InitRequestLimit 初始化请求限制
func InitRequestLimit(limit int) {
	if limit > 0 {
		limiter = NewIPLimiter(limit)
		log.Info("Request limit is set as ", limit, " each IP")
	}
}

// RequestLimit 请求限制中间件
func RequestLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if limiter == nil {
			next.ServeHTTP(w, r)
			return
		}

		ip := M.ParseSocksaddr(r.RemoteAddr).Addr.String()
		access := limiter.GetAccess(ip)
		if !access {
			log.WarnContext(r.Context(), "Match request limit")
			w.WriteHeader(http.StatusTooManyRequests)
			if config.Global.RequestLimit == 1 {
				w.Write([]byte("You can only initiate 1 request at the same time"))
			} else {
				w.Write([]byte(fmt.Sprint("You can only initiate ", config.Global.RequestLimit, " requests at the same time")))
			}
			return
		}
		next.ServeHTTP(w, r)
		limiter.Leave(ip)
	})
}
