//go:build !windows && !linux

package perfstats

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// RSSSource 标识进程驻留内存的实际采样接口。
const RSSSource = "ps -o rss= (KiB)"

// macOS的ps按KiB报告RSS；这是开发期门禁观测，不进入codec运行时依赖。
func ResidentBytes() (uint64, error) {
	data, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		return 0, err
	}
	kb, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, err
	}
	if kb > ^uint64(0)/1024 {
		return 0, fmt.Errorf("RSS溢出")
	}
	return kb * 1024, nil
}
