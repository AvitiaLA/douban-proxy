package config

import (
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
	DenyWebPage       bool

	// 限制配置
	BandwidthLimit int
	RequestLimit   int

	// 缓存配置
	CacheMaxSize     int64
	CacheFile        string
	CacheLoadPercent int

	// 域名配置
	DomainListPath string
	FaviconDomain  string
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		RunningPort:       30000,
		DisableColor:      false,
		EnableCache:       false,
		EnableCORS:        true,
		AddDefaultHeaders: true,
		DenyWebPage:       false,
		BandwidthLimit:    0,
		RequestLimit:      0,
		CacheMaxSize:      100,
		CacheFile:         "cache.gob",
		CacheLoadPercent:  80,
		DomainListPath:    "domainlist.txt",
		FaviconDomain:     "www.douban.com",
	}
}

// Global 全局配置实例
var Global = DefaultConfig()

// InitFlags 初始化命令行参数
func InitFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().BoolVarP(&Global.DisableColor, "disable-color", "", false, "禁用彩色输出")
	cmd.PersistentFlags().IntVarP(&Global.RunningPort, "running-port", "p", 30000, "代理服务器端口")
	cmd.PersistentFlags().StringVarP(&Global.DomainListPath, "domain-list-path", "d", "domainlist.txt", "设置接受的域名列表")
	cmd.PersistentFlags().IntVarP(&Global.BandwidthLimit, "bandwidth-limit", "l", 0, "设置总带宽限制 (MB/s)，0为无限制")
	cmd.PersistentFlags().IntVarP(&Global.RequestLimit, "request-limit", "r", 0, "设置每IP请求限制，0为无限制")
	cmd.PersistentFlags().BoolVarP(&Global.DenyWebPage, "deny-web-page", "", false, "拒绝网页请求")
	cmd.PersistentFlags().BoolVarP(&Global.EnableCache, "enable-cache", "", false, "启用响应缓存")
	cmd.PersistentFlags().Int64VarP(&Global.CacheMaxSize, "cache-max-size", "", 100, "最大缓存大小（MB）")
	cmd.PersistentFlags().BoolVarP(&Global.AddDefaultHeaders, "add-default-headers", "", true, "添加默认请求头以提高兼容性")
	cmd.PersistentFlags().BoolVarP(&Global.EnableCORS, "enable-cors", "", true, "启用CORS支持")
	cmd.PersistentFlags().StringVarP(&Global.FaviconDomain, "favicon-domain", "", "www.douban.com", "favicon请求的域名")
	cmd.PersistentFlags().StringVarP(&Global.CacheFile, "cache-file", "", "cache.gob", "缓存持久化文件路径")
	cmd.PersistentFlags().IntVarP(&Global.CacheLoadPercent, "cache-load-percent", "", 80, "启动时加载缓存的百分比")
}
