//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

var getProcessMemoryInfo = syscall.NewLazyDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

// PROCESS_MEMORY_COUNTERS遵循Win32 ABI；cb明确给出结构大小，Windows只在同步
// 调用期间写入这块固定长度内存，不跨调用持有Go指针；unsafe仅用于系统API边界。
func residentBytes() (uint64, error) {
	var m struct {
		Size, Faults                             uint32
		PeakWorkingSet, WorkingSet               uintptr
		PeakPaged, Paged, PeakNonPaged, NonPaged uintptr
		Pagefile, PeakPagefile                   uintptr
	}
	m.Size = uint32(unsafe.Sizeof(m))
	ok, _, err := getProcessMemoryInfo.Call(^uintptr(0), uintptr(unsafe.Pointer(&m)), uintptr(m.Size))
	if ok == 0 {
		return 0, fmt.Errorf("读取进程RSS: %w", err)
	}
	return uint64(m.WorkingSet), nil
}
