package proxy

import (
	"bufio"
	"context"
	"net"
	"os"
	"strings"
	"time"

	"github.com/sagernet/fswatch"
	L "github.com/sagernet/sing-box/log"
	E "github.com/sagernet/sing/common/exceptions"
)

var log = L.NewDefaultFactory(
	context.Background(),
	L.Formatter{
		BaseTime:        time.Now(),
		FullTimestamp:   true,
		TimestampFormat: "-0700 2006-01-02 15:04:05",
	},
	os.Stdout,
	"domain",
	nil,
	false,
).Logger()

// AcceptDomain 接受的域名列表
var AcceptDomain = []string{
	"*.douban.com",   // 支持所有douban.com子域名
	"*.doubanio.com", // 支持所有doubanio.com子域名
}

// WildcardMatch 通配符匹配函数
// 支持 * 通配符，例如：
// *.example.com 匹配 api.example.com, www.example.com 等
// img*.example.com 匹配 img1.example.com, img2.example.com 等
func WildcardMatch(pattern, text string) bool {
	if pattern == "*" {
		return true
	}
	if pattern == text {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == text
	}

	// 将通配符模式转换为正则表达式风格的匹配
	parts := strings.Split(pattern, "*")
	if len(parts) == 0 {
		return false
	}

	// 检查开头
	if len(parts[0]) > 0 && !strings.HasPrefix(text, parts[0]) {
		return false
	}

	// 检查结尾
	if len(parts[len(parts)-1]) > 0 && !strings.HasSuffix(text, parts[len(parts)-1]) {
		return false
	}

	// 检查中间部分
	currentPos := len(parts[0])
	for i := 1; i < len(parts)-1; i++ {
		part := parts[i]
		if part == "" {
			continue
		}
		pos := strings.Index(text[currentPos:], part)
		if pos == -1 {
			return false
		}
		currentPos += pos + len(part)
	}

	return true
}

// IsDomainAccepted 检查域名是否被接受（支持通配符）
func IsDomainAccepted(host string) bool {
	for _, domain := range AcceptDomain {
		if WildcardMatch(domain, host) {
			return true
		}
	}
	return false
}

// FileReader 文件阅读器
type FileReader struct {
	LineChan    chan string
	CloseSignal chan struct{}
}

// NewFileReader 创建新的文件阅读器
func NewFileReader(path string) (*FileReader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	reader := FileReader{
		LineChan:    make(chan string),
		CloseSignal: make(chan struct{}),
	}
	go func() {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if len(line) == 0 {
				continue
			}
			lr := []rune(line)
			if lr[0] == '#' || (len(lr) > 1 && lr[0] == '/' && lr[1] == '/') {
				continue
			}
			for i, r := range lr {
				if r == '#' || (r == '/' && i < len(lr)-1 && lr[i+1] == '/') {
					line = strings.TrimSpace(string(lr[:i]))
					break
				}
			}
			reader.LineChan <- line
		}
		reader.CloseSignal <- struct{}{}
	}()
	return &reader, nil
}

// Close 关闭文件阅读器
func (r *FileReader) Close() {
	close(r.LineChan)
	close(r.CloseSignal)
}

// LoadDomainList 加载域名列表
func LoadDomainList(domainListPath string) (*fswatch.Watcher, error) {
	err := loadDomainListData(domainListPath)
	if err != nil {
		return nil, err
	}
	watcher, err := fswatch.NewWatcher(fswatch.Options{
		Path: []string{domainListPath},
		Callback: func(path string) {
			log.Info("Accept domain list changed, reloading")
			loadDomainListData(domainListPath)
		},
	})
	if err != nil {
		log.Error(E.Cause(err, "Create accept domain list watcher"))
		return nil, err
	}
	return watcher, nil
}

// loadDomainListData 加载域名列表数据
func loadDomainListData(domainListPath string) error {
	reader, err := NewFileReader(domainListPath)
	if err != nil {
		return err
	}
	var domainList []string
	var needBreak bool
	for {
		if needBreak {
			break
		}
		select {
		case <-reader.CloseSignal:
			needBreak = true
			continue
		case line := <-reader.LineChan:
			if net.ParseIP(line) != nil {
				continue
			}
			domainList = append(domainList, line)
		}
	}
	if len(domainList) > 0 {
		AcceptDomain = domainList
		log.Info("Custom accept domain list loaded")
	} else {
		log.Warn("Custom accept domain list is empty")
	}
	return nil
}
