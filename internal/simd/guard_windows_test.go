//go:build windows

package simd

import (
	"os"
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

const guardPagesAvailable = true

// guardedRows的unsafe只用于测试：固定上限的无指针Go byte backing先pin，
// 保护页完全位于该对象内部（不含对象头/相邻对象），退出前恢复可读写再unpin。
// 不将uintptr转回Go指针；uint16视图只在2字节对齐、偶数len的同一对象内建立。
func guardedRows(t *testing.T, width, height, elementBytes int) ([]byte, int) {
	t.Helper()
	page := os.Getpagesize()
	if width < 1 || width > 64 || height < 1 || height > 32 || elementBytes < 1 || elementBytes > 4 || width*elementBytes > page {
		t.Fatal("保护页测试几何越界")
	}
	storage := make([]byte, (2*height+2)*page)
	var previous uint32
	var pin runtime.Pinner
	pin.Pin(&storage[0])
	pin.Pin(&previous)
	alignment := (page - int(uintptr(unsafe.Pointer(&storage[0]))%uintptr(page))) % page
	protect := syscall.NewLazyDLL("kernel32.dll").NewProc("VirtualProtect")
	if err := protect.Find(); err != nil {
		pin.Unpin()
		t.Fatal(err)
	}
	var protected []struct {
		offset int
		access uint32
	}
	t.Cleanup(func() {
		for _, saved := range protected {
			r, _, err := protect.Call(uintptr(unsafe.Pointer(&storage[saved.offset])), uintptr(page), uintptr(saved.access), uintptr(unsafe.Pointer(&previous)))
			if r == 0 {
				t.Errorf("恢复原页属性：%v", err)
			}
		}
		pin.Unpin()
		runtime.KeepAlive(storage)
		runtime.KeepAlive(previous)
	})
	for y := range height {
		offset := alignment + (2*y+1)*page
		r, _, err := protect.Call(uintptr(unsafe.Pointer(&storage[offset])), uintptr(page), 0x01, uintptr(unsafe.Pointer(&previous)))
		if r == 0 {
			t.Fatalf("设置PAGE_NOACCESS：%v", err)
		}
		protected = append(protected, struct {
			offset int
			access uint32
		}{offset, previous})
	}
	first := alignment + page - width*elementBytes
	length := (height-1)*2*page + width*elementBytes
	return storage[first : first+length], 2 * page / elementBytes
}
