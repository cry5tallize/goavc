//go:build linux

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func residentBytes() (uint64, error) {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0, fmt.Errorf("进程RSS记录无效")
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, err
	}
	pageSize := uint64(os.Getpagesize())
	if pages > ^uint64(0)/pageSize {
		return 0, fmt.Errorf("RSS溢出")
	}
	return pages * pageSize, nil
}
