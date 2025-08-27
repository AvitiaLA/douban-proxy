package config

import (
	"fmt"
	"os"

	"douban-proxy/internal/version"

	"github.com/spf13/cobra"
)

// Config 应用程序配置
type Config struct {
	// 服务器配置
	RunningPort int

	// 功能开关
	DisableColor      bool
	EnableCache       bool
	EnableCORS        bool
	AddDefaultHeaders bool

	// 限制配置
	BandwidthLimit int
	RequestLimit   int

	// 缓存配置
	CacheMemorySize   int64  // 内存缓存大小（MB）
	CacheDir          string // 磁盘缓存目录
	CacheCleanupHours int    // 缓存清理间隔（小时）

	// 域名配置
	DomainListPath string
	FaviconDomain  string
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	port := 30000 // 默认端口
	if envPort := os.Getenv("PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			port = p
		}
	}

	return &Config{
		RunningPort:       port,   // 使用环境变量 PORT 或默认值
		DisableColor:      false,
		EnableCache:       false,
		EnableCORS:        true,
		AddDefaultHeaders: true,
		BandwidthLimit:    0,
		RequestLimit:      0,
		CacheMemorySize:   50,              // 默认50MB内存缓存
		CacheDir:          "/etc/db-proxy", // 默认缓存目录
		CacheCleanupHours: 12,              // 默认12小时清理一次
		DomainListPath:    "domainlist.txt",
		FaviconDomain:     "www.douban.com",
	}
}

// Global 全局配置实例
var Global = DefaultConfig()

// InitFlags 初始化命令行参数
func InitFlags(cmd *cobra.Command) {
	// 版本信息标志
	var showVersion bool
	cmd.Flags().BoolVarP(&showVersion, "version", "v", false, "显示版本信息")

	// 处理版本信息
	cmd.PreRun = func(cmd *cobra.Command, args []string) {
		if showVersion {
			fmt.Println(version.Info())
			os.Exit(0)
		}
	}

	cmd.PersistentFlags().BoolVarP(&Global.DisableColor, "disable-color", "", false, "禁用彩色输出")
	cmd.PersistentFlags().IntVarP(&Global.RunningPort, "running-port", "p", 30000, "代理服务器端口")
	cmd.PersistentFlags().StringVarP(&Global.DomainListPath, "domain-list-path", "d", "domainlist.txt", "设置接受的域名列表")
	cmd.PersistentFlags().IntVarP(&Global.BandwidthLimit, "bandwidth-limit", "l", 0, "设置总带宽限制 (MB/s)，0为无限制")
	cmd.PersistentFlags().IntVarP(&Global.RequestLimit, "request-limit", "r", 0, "设置每IP请求限制，0为无限制")
	cmd.PersistentFlags().BoolVarP(&Global.EnableCache, "enable-cache", "", false, "启用响应缓存")
	cmd.PersistentFlags().Int64VarP(&Global.CacheMemorySize, "cache-memory-size", "", 50, "内存缓存大小（MB）")
	cmd.PersistentFlags().StringVarP(&Global.CacheDir, "cache-dir", "", "/etc/db-proxy", "磁盘缓存目录")
	cmd.PersistentFlags().BoolVarP(&Global.AddDefaultHeaders, "add-default-headers", "", true, "添加默认请求头以提高兼容性")
	cmd.PersistentFlags().BoolVarP(&Global.EnableCORS, "enable-cors", "", true, "启用CORS支持")
	cmd.PersistentFlags().StringVarP(&Global.FaviconDomain, "favicon-domain", "", "www.douban.com", "favicon请求的域名")
	cmd.PersistentFlags().IntVarP(&Global.CacheCleanupHours, "cache-cleanup-hours", "", 12, "缓存清理间隔（小时）")
}
