package version

import (
	"fmt"
	"runtime"
	"time"
)

var (
	// 这些变量在构建时通过ldflags设置
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
	GoVersion = runtime.Version()
)

// Info 返回完整版本信息字符串
func Info() string {
	var buildTimeFormatted string
	if BuildTime != "unknown" {
		if t, err := time.Parse(time.RFC3339, BuildTime); err == nil {
			buildTimeFormatted = t.Format("Mon Jan 2 15:04:05 MST 2006")
		} else {
			buildTimeFormatted = BuildTime
		}
	} else {
		buildTimeFormatted = time.Now().Format("Mon Jan 2 15:04:05 MST 2006")
	}

	return fmt.Sprintf("douban-proxy %s %s %s %s with %s %s",
		Version,
		GitCommit,
		runtime.GOOS,
		runtime.GOARCH,
		GoVersion,
		buildTimeFormatted,
	)
}
