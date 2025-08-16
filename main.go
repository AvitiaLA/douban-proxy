package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"douban-proxy/internal/config"
	"douban-proxy/internal/handler"
	"douban-proxy/internal/middleware"
	"douban-proxy/internal/proxy"
	"douban-proxy/internal/service"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	L "github.com/sagernet/sing-box/log"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/spf13/cobra"
)

var log = L.NewDefaultFactory(
	context.Background(),
	L.Formatter{
		BaseTime:        time.Now(),
		FullTimestamp:   true,
		TimestampFormat: "-0700 2006-01-02 15:04:05",
	},
	os.Stdout,
	"",
	nil,
	false,
).Logger()

var rootCommand = &cobra.Command{
	Use:   "douban-proxy",
	Short: "一个用于代理豆瓣请求的HTTP服务",
	Run:   run,
}

func init() {
	config.InitFlags(rootCommand)

	// 设置服务包的日志器
	service.SetLogger(log)

	// 添加服务命令
	rootCommand.AddCommand(service.GetServiceCommand())
}

func main() {
	if err := rootCommand.Execute(); err != nil {
		log.Fatal(err)
	}
}

func run(*cobra.Command, []string) {
	// 初始化HTTP客户端
	handler.InitHTTPClient()

	// 初始化带宽限制器
	handler.InitBandwidthLimiter(config.Global.BandwidthLimit)

	// 初始化请求限制器
	middleware.InitRequestLimit(config.Global.RequestLimit)

	// 初始化缓存
	handler.InitCache()

	// 加载域名列表监控
	if watcher, err := proxy.LoadDomainList(config.Global.DomainListPath); err == nil {
		err = watcher.Start()
		if err == nil {
			defer watcher.Close()
		} else {
			watcher.Close()
		}
	}

	// 设置监听地址
	listen := M.ParseSocksaddr(":" + strconv.Itoa(config.Global.RunningPort))
	listener := listenTCP(listen)

	// 仅监听 TCP（反向代理无需内置 TLS）
	log.Info("TCP port ", listen.Port)

	// 设置路由
	chiRouter := chi.NewRouter()
	chiRouter.Group(func(r chi.Router) {
		r.Use(chimiddleware.RealIP)
		r.Use(middleware.SetContext)
		r.Use(middleware.CORS)
		r.Use(middleware.CommonLog)
		r.Use(middleware.RequestLimit)
		r.Get("/", handler.Hello)
		r.HandleFunc("/*", handler.Final)
	})

	server := &http.Server{
		Addr:    listener.Addr().String(),
		Handler: chiRouter,
	}

	// 启动HTTP服务器
	go func() {
		err := server.Serve(listener)
		if err != nil {
			log.Fatal(err)
		}
	}()

	// 等待系统信号
	osSignals := make(chan os.Signal, 1)
	signal.Notify(osSignals, syscall.SIGINT, syscall.SIGKILL, syscall.SIGTERM)
	defer signal.Stop(osSignals)
	<-osSignals

	// 清理缓存
	done := make(chan bool, 1)
	go func() {
		handler.SaveCache()
		done <- true
	}()

	// 等待保存完成，但最多等待5秒
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}

	handler.StopCache()
}

func listenTCP(address M.Socksaddr) net.Listener {
	var listener net.Listener
	for {
		var err error
		listener, err = net.Listen("tcp", address.String())
		if err == nil {
			break
		}
		address.Port = address.Port + 1
	}
	return listener
}
