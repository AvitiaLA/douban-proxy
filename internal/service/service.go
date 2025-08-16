package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/template"

	L "github.com/sagernet/sing-box/log"
	"github.com/spf13/cobra"
)

const systemdServiceTemplate = `[Unit]
Description=Douban Proxy Service
After=network.target

[Service]
Type=simple
User={{.User}}
WorkingDirectory={{.WorkingDirectory}}
ExecStart={{.ExecStart}}
Restart=always
RestartSec=10
Environment=PATH=/usr/local/bin:/usr/bin:/bin

[Install]
WantedBy=multi-user.target
`

type ServiceConfig struct {
	User             string
	WorkingDirectory string
	ExecStart        string
	Port             int
}

// InstallConfig 安装服务时的配置参数
type InstallConfig struct {
	EnableCache       bool
	CacheFile         string
	CacheMaxSize      int64
	CacheLoadPercent  int
	BandwidthLimit    int
	RequestLimit      int
	DomainListPath    string
	RunningPort       int
	DisableColor      bool
	EnableCORS        bool
	AddDefaultHeaders bool
	FaviconDomain     string
}

// NewInstallConfigFromFlags 从命令行标志创建安装配置
func NewInstallConfigFromFlags(
	enableCache bool,
	cacheFile string,
	cacheMaxSize int64,
	cacheLoadPercent int,
	bandwidthLimit int,
	requestLimit int,
	domainListPath string,
	runningPort int,
	disableColor bool,
	enableCORS bool,
	addDefaultHeaders bool,
	faviconDomain string,
) *InstallConfig {
	return &InstallConfig{
		EnableCache:       enableCache,
		CacheFile:         cacheFile,
		CacheMaxSize:      cacheMaxSize,
		CacheLoadPercent:  cacheLoadPercent,
		BandwidthLimit:    bandwidthLimit,
		RequestLimit:      requestLimit,
		DomainListPath:    domainListPath,
		RunningPort:       runningPort,
		DisableColor:      disableColor,
		EnableCORS:        enableCORS,
		AddDefaultHeaders: addDefaultHeaders,
		FaviconDomain:     faviconDomain,
	}
}

type ServiceManager struct {
	ServiceName string
	ServiceFile string
	Config      ServiceConfig
}

// NewServiceManager 创建服务管理器
func NewServiceManager() *ServiceManager {
	serviceName := "douban-proxy"
	serviceFile := fmt.Sprintf("/etc/systemd/system/%s.service", serviceName)

	// 获取当前执行文件路径
	execPath, _ := os.Executable()
	execPath, _ = filepath.Abs(execPath)
	workDir := filepath.Dir(execPath)

	// 获取当前用户
	user := os.Getenv("USER")
	if user == "" {
		user = "root"
	}

	config := ServiceConfig{
		User:             user,
		WorkingDirectory: workDir,
		ExecStart:        execPath,
		Port:             30000,
	}

	return &ServiceManager{
		ServiceName: serviceName,
		ServiceFile: serviceFile,
		Config:      config,
	}
}

// InstallWithConfig 使用指定配置安装系统服务
func (sm *ServiceManager) InstallWithConfig(installConfig *InstallConfig) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("服务安装仅支持 Linux 系统")
	}

	// 检查是否为 root 权限
	if os.Geteuid() != 0 {
		return fmt.Errorf("安装服务需要 root 权限，请使用 sudo 运行")
	}

	// 设置端口
	if installConfig.RunningPort != 30000 {
		sm.SetPort(installConfig.RunningPort)
	}

	// 构建服务启动参数
	var serviceArgs []string

	if installConfig.EnableCache {
		serviceArgs = append(serviceArgs, "--enable-cache")
	}

	if installConfig.CacheFile != "" {
		serviceArgs = append(serviceArgs, "--cache-file", installConfig.CacheFile)
	}

	if installConfig.CacheMaxSize > 0 {
		serviceArgs = append(serviceArgs, "--cache-max-size", strconv.FormatInt(installConfig.CacheMaxSize, 10))
	}

	if installConfig.CacheLoadPercent > 0 {
		serviceArgs = append(serviceArgs, "--cache-load-percent", strconv.Itoa(installConfig.CacheLoadPercent))
	}

	if installConfig.BandwidthLimit > 0 {
		serviceArgs = append(serviceArgs, "--bandwidth-limit", strconv.Itoa(installConfig.BandwidthLimit))
	}

	if installConfig.RequestLimit > 0 {
		serviceArgs = append(serviceArgs, "--request-limit", strconv.Itoa(installConfig.RequestLimit))
	}

	if installConfig.DomainListPath != "" {
		serviceArgs = append(serviceArgs, "--domain-list-path", installConfig.DomainListPath)
	}

	if installConfig.RunningPort != 30000 {
		serviceArgs = append(serviceArgs, "--running-port", strconv.Itoa(installConfig.RunningPort))
	}

	if installConfig.DisableColor {
		serviceArgs = append(serviceArgs, "--disable-color")
	}

	if !installConfig.EnableCORS {
		serviceArgs = append(serviceArgs, "--enable-cors=false")
	}

	if !installConfig.AddDefaultHeaders {
		serviceArgs = append(serviceArgs, "--add-default-headers=false")
	}

	if installConfig.FaviconDomain != "" {
		serviceArgs = append(serviceArgs, "--favicon-domain", installConfig.FaviconDomain)
	}

	sm.SetExtraArgs(serviceArgs)

	return sm.install()
}

// Install 安装系统服务（保持向后兼容）
func (sm *ServiceManager) Install() error {
	return sm.install()
}

// install 内部安装方法
func (sm *ServiceManager) install() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("服务安装仅支持 Linux 系统")
	}

	// 检查是否为 root 权限
	if os.Geteuid() != 0 {
		return fmt.Errorf("安装服务需要 root 权限，请使用 sudo 运行")
	}

	// 生成服务文件内容
	tmpl, err := template.New("service").Parse(systemdServiceTemplate)
	if err != nil {
		return fmt.Errorf("解析服务模板失败: %v", err)
	}

	file, err := os.Create(sm.ServiceFile)
	if err != nil {
		return fmt.Errorf("创建服务文件失败: %v", err)
	}
	defer file.Close()

	err = tmpl.Execute(file, sm.Config)
	if err != nil {
		return fmt.Errorf("写入服务文件失败: %v", err)
	}

	// 重新加载 systemd
	err = sm.runCommand("systemctl", "daemon-reload")
	if err != nil {
		return fmt.Errorf("重新加载 systemd 失败: %v", err)
	}

	// 启用服务
	err = sm.runCommand("systemctl", "enable", sm.ServiceName)
	if err != nil {
		return fmt.Errorf("启用服务失败: %v", err)
	}

	fmt.Printf("✅ 服务 %s 安装成功\n", sm.ServiceName)
	fmt.Printf("   服务文件: %s\n", sm.ServiceFile)
	fmt.Printf("   工作目录: %s\n", sm.Config.WorkingDirectory)
	fmt.Printf("   执行文件: %s\n", sm.Config.ExecStart)
	fmt.Printf("   运行端口: %d\n", sm.Config.Port)
	fmt.Println("\n现在可以使用以下命令管理服务:")
	fmt.Printf("   启动: douban-proxy service start\n")
	fmt.Printf("   停止: douban-proxy service stop\n")
	fmt.Printf("   重启: douban-proxy service restart\n")
	fmt.Printf("   状态: douban-proxy service status\n")

	return nil
}

// Uninstall 卸载系统服务
func (sm *ServiceManager) Uninstall() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("服务卸载仅支持 Linux 系统")
	}

	// 检查是否为 root 权限
	if os.Geteuid() != 0 {
		return fmt.Errorf("卸载服务需要 root 权限，请使用 sudo 运行")
	}

	// 停止服务
	sm.runCommand("systemctl", "stop", sm.ServiceName)

	// 禁用服务
	sm.runCommand("systemctl", "disable", sm.ServiceName)

	// 删除服务文件
	err := os.Remove(sm.ServiceFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除服务文件失败: %v", err)
	}

	// 重新加载 systemd
	err = sm.runCommand("systemctl", "daemon-reload")
	if err != nil {
		return fmt.Errorf("重新加载 systemd 失败: %v", err)
	}

	fmt.Printf("✅ 服务 %s 卸载成功\n", sm.ServiceName)
	return nil
}

// Start 启动服务
func (sm *ServiceManager) Start() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("服务控制仅支持 Linux 系统")
	}

	err := sm.runCommand("systemctl", "start", sm.ServiceName)
	if err != nil {
		return fmt.Errorf("启动服务失败: %v", err)
	}

	fmt.Printf("✅ 服务 %s 启动成功\n", sm.ServiceName)
	return nil
}

// Stop 停止服务
func (sm *ServiceManager) Stop() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("服务控制仅支持 Linux 系统")
	}

	err := sm.runCommand("systemctl", "stop", sm.ServiceName)
	if err != nil {
		return fmt.Errorf("停止服务失败: %v", err)
	}

	fmt.Printf("✅ 服务 %s 停止成功\n", sm.ServiceName)
	return nil
}

// Restart 重启服务
func (sm *ServiceManager) Restart() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("服务控制仅支持 Linux 系统")
	}

	err := sm.runCommand("systemctl", "restart", sm.ServiceName)
	if err != nil {
		return fmt.Errorf("重启服务失败: %v", err)
	}

	fmt.Printf("✅ 服务 %s 重启成功\n", sm.ServiceName)
	return nil
}

// Status 查看服务状态
func (sm *ServiceManager) Status() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("服务控制仅支持 Linux 系统")
	}

	cmd := exec.Command("systemctl", "status", sm.ServiceName)
	output, err := cmd.CombinedOutput()

	fmt.Printf("服务 %s 状态:\n", sm.ServiceName)
	fmt.Println(string(output))

	if err != nil {
		// systemctl status 在服务不存在或停止时会返回非零退出码，这是正常的
		if strings.Contains(string(output), "could not be found") {
			fmt.Printf("❌ 服务 %s 未安装\n", sm.ServiceName)
			fmt.Println("使用 'douban-proxy service install' 安装服务")
		}
	}

	return nil
}

// IsInstalled 检查服务是否已安装
func (sm *ServiceManager) IsInstalled() bool {
	_, err := os.Stat(sm.ServiceFile)
	return err == nil
}

// IsRunning 检查服务是否正在运行
func (sm *ServiceManager) IsRunning() bool {
	cmd := exec.Command("systemctl", "is-active", sm.ServiceName)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "active"
}

// SetPort 设置服务端口
func (sm *ServiceManager) SetPort(port int) {
	sm.Config.Port = port
	sm.Config.ExecStart = fmt.Sprintf("%s --running-port %d",
		strings.Fields(sm.Config.ExecStart)[0], port)
}

// SetExtraArgs 设置额外参数
func (sm *ServiceManager) SetExtraArgs(args []string) {
	baseExec := strings.Fields(sm.Config.ExecStart)[0]
	if len(args) > 0 {
		sm.Config.ExecStart = fmt.Sprintf("%s %s", baseExec, strings.Join(args, " "))
	} else {
		sm.Config.ExecStart = baseExec
	}
}

// runCommand 执行系统命令
func (sm *ServiceManager) runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("命令执行失败: %s\n输出: %s", err, string(output))
	}
	return nil
}

// ParsePortFromArgs 从参数中解析端口
func ParsePortFromArgs(args []string) int {
	for i, arg := range args {
		if arg == "--running-port" && i+1 < len(args) {
			if port, err := strconv.Atoi(args[i+1]); err == nil {
				return port
			}
		}
	}
	return 30000 // 默认端口
}

// 为 service install 命令定义专门的参数
var installServiceConfig struct {
	EnableCache       bool
	CacheFile         string
	CacheMaxSize      int64
	CacheLoadPercent  int
	BandwidthLimit    int
	RequestLimit      int
	DomainListPath    string
	RunningPort       int
	DisableColor      bool
	EnableCORS        bool
	AddDefaultHeaders bool
	FaviconDomain     string
}

// GetServiceCommand 返回服务管理命令
func GetServiceCommand() *cobra.Command {
	serviceCommand := &cobra.Command{
		Use:   "service",
		Short: "管理系统服务",
		Long:  "管理 douban-proxy 系统服务的安装、启动、停止等操作",
	}

	serviceInstallCommand := &cobra.Command{
		Use:   "install",
		Short: "安装系统服务",
		Run:   serviceInstallFunc,
	}

	serviceUninstallCommand := &cobra.Command{
		Use:   "uninstall",
		Short: "卸载系统服务",
		Run:   serviceUninstallFunc,
	}

	serviceStartCommand := &cobra.Command{
		Use:   "start",
		Short: "启动服务",
		Run:   serviceStartFunc,
	}

	serviceStopCommand := &cobra.Command{
		Use:   "stop",
		Short: "停止服务",
		Run:   serviceStopFunc,
	}

	serviceRestartCommand := &cobra.Command{
		Use:   "restart",
		Short: "重启服务",
		Run:   serviceRestartFunc,
	}

	serviceStatusCommand := &cobra.Command{
		Use:   "status",
		Short: "查看服务状态",
		Run:   serviceStatusFunc,
	}

	// 为 service install 命令添加专门的参数
	serviceInstallCommand.Flags().BoolVar(&installServiceConfig.EnableCache, "enable-cache", false, "启用响应缓存")
	serviceInstallCommand.Flags().StringVar(&installServiceConfig.CacheFile, "cache-file", "", "缓存持久化文件路径")
	serviceInstallCommand.Flags().Int64Var(&installServiceConfig.CacheMaxSize, "cache-max-size", 0, "最大缓存大小（MB）")
	serviceInstallCommand.Flags().IntVar(&installServiceConfig.CacheLoadPercent, "cache-load-percent", 0, "启动时加载缓存的百分比")
	serviceInstallCommand.Flags().IntVar(&installServiceConfig.BandwidthLimit, "bandwidth-limit", 0, "设置总带宽限制 (MB/s)，0为无限制")
	serviceInstallCommand.Flags().IntVar(&installServiceConfig.RequestLimit, "request-limit", 0, "设置每IP请求限制，0为无限制")
	serviceInstallCommand.Flags().StringVar(&installServiceConfig.DomainListPath, "domain-list-path", "", "设置接受的域名列表")
	serviceInstallCommand.Flags().IntVar(&installServiceConfig.RunningPort, "running-port", 30000, "代理服务器端口")
	serviceInstallCommand.Flags().BoolVar(&installServiceConfig.DisableColor, "disable-color", false, "禁用彩色输出")
	serviceInstallCommand.Flags().BoolVar(&installServiceConfig.EnableCORS, "enable-cors", true, "启用CORS支持")
	serviceInstallCommand.Flags().BoolVar(&installServiceConfig.AddDefaultHeaders, "add-default-headers", true, "添加默认请求头以提高兼容性")
	serviceInstallCommand.Flags().StringVar(&installServiceConfig.FaviconDomain, "favicon-domain", "", "favicon请求的域名")

	// 添加子命令
	serviceCommand.AddCommand(serviceInstallCommand)
	serviceCommand.AddCommand(serviceUninstallCommand)
	serviceCommand.AddCommand(serviceStartCommand)
	serviceCommand.AddCommand(serviceStopCommand)
	serviceCommand.AddCommand(serviceRestartCommand)
	serviceCommand.AddCommand(serviceStatusCommand)

	return serviceCommand
}

// 需要一个日志器用于服务命令
var log L.Logger

// SetLogger 设置日志器
func SetLogger(logger L.Logger) {
	log = logger
}

// 服务管理函数
func serviceInstallFunc(cmd *cobra.Command, args []string) {
	sm := NewServiceManager()

	// 构建安装配置，只包含通过命令行明确指定的参数
	installConfig := &InstallConfig{
		RunningPort:       30000, // 默认端口
		EnableCORS:        true,  // 默认启用
		AddDefaultHeaders: true,  // 默认启用
	}

	// 只有当标志被明确设置时才更新配置
	if cmd.Flags().Changed("enable-cache") {
		installConfig.EnableCache = installServiceConfig.EnableCache
	}

	if cmd.Flags().Changed("cache-file") {
		installConfig.CacheFile = installServiceConfig.CacheFile
	}

	if cmd.Flags().Changed("cache-max-size") {
		installConfig.CacheMaxSize = installServiceConfig.CacheMaxSize
	}

	if cmd.Flags().Changed("cache-load-percent") {
		installConfig.CacheLoadPercent = installServiceConfig.CacheLoadPercent
	}

	if cmd.Flags().Changed("bandwidth-limit") {
		installConfig.BandwidthLimit = installServiceConfig.BandwidthLimit
	}

	if cmd.Flags().Changed("request-limit") {
		installConfig.RequestLimit = installServiceConfig.RequestLimit
	}

	if cmd.Flags().Changed("domain-list-path") {
		installConfig.DomainListPath = installServiceConfig.DomainListPath
	}

	if cmd.Flags().Changed("running-port") {
		installConfig.RunningPort = installServiceConfig.RunningPort
	}

	if cmd.Flags().Changed("disable-color") {
		installConfig.DisableColor = installServiceConfig.DisableColor
	}

	if cmd.Flags().Changed("enable-cors") {
		installConfig.EnableCORS = installServiceConfig.EnableCORS
	}

	if cmd.Flags().Changed("add-default-headers") {
		installConfig.AddDefaultHeaders = installServiceConfig.AddDefaultHeaders
	}

	if cmd.Flags().Changed("favicon-domain") {
		installConfig.FaviconDomain = installServiceConfig.FaviconDomain
	}

	if err := sm.InstallWithConfig(installConfig); err != nil {
		log.Fatal("安装服务失败:", err)
	}
}

func serviceUninstallFunc(cmd *cobra.Command, args []string) {
	sm := NewServiceManager()
	if err := sm.Uninstall(); err != nil {
		log.Fatal("卸载服务失败:", err)
	}
}

func serviceStartFunc(cmd *cobra.Command, args []string) {
	sm := NewServiceManager()
	if !sm.IsInstalled() {
		log.Fatal("服务未安装，请先运行: douban-proxy service install")
	}
	if err := sm.Start(); err != nil {
		log.Fatal("启动服务失败:", err)
	}
}

func serviceStopFunc(cmd *cobra.Command, args []string) {
	sm := NewServiceManager()
	if !sm.IsInstalled() {
		log.Fatal("服务未安装")
	}
	if err := sm.Stop(); err != nil {
		log.Fatal("停止服务失败:", err)
	}
}

func serviceRestartFunc(cmd *cobra.Command, args []string) {
	sm := NewServiceManager()
	if !sm.IsInstalled() {
		log.Fatal("服务未安装，请先运行: douban-proxy service install")
	}
	if err := sm.Restart(); err != nil {
		log.Fatal("重启服务失败:", err)
	}
}

func serviceStatusFunc(cmd *cobra.Command, args []string) {
	sm := NewServiceManager()
	if err := sm.Status(); err != nil {
		log.Fatal("查看服务状态失败:", err)
	}
}
