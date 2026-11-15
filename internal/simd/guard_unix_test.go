//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package simd

import (
	"os"
	"syscall"
	"testing"
)

const guardPagesAvailable = true

// mmap 的独立无指针测试映射不受Go堆移动影响；每行之后一页设为PROT_NONE。
// Cleanup先恢复页权限再解除整个映射，不把不可访问页交还给Go分配器。
func guardedRows(t *testing.T, width, height, elementBytes int) ([]byte, int) {
	t.Helper()
	page := os.Getpagesize()
	if width < 1 || width > 64 || height < 1 || height > 32 || elementBytes < 1 || elementBytes > 4 || width*elementBytes > page {
		t.Fatal("保护页测试几何越界")
	}
	b, err := syscall.Mmap(-1, 0, (2*height+1)*page, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := syscall.Mprotect(b, syscall.PROT_READ|syscall.PROT_WRITE); err != nil {
			t.Errorf("恢复页属性：%v", err)
		}
		if err := syscall.Munmap(b); err != nil {
			t.Errorf("解除保护页映射：%v", err)
		}
	})
	for y := range height {
		if err := syscall.Mprotect(b[(2*y+1)*page:(2*y+2)*page], syscall.PROT_NONE); err != nil {
			t.Fatal(err)
		}
	}
	first := page - width*elementBytes
	return b[first : first+(height-1)*2*page+width*elementBytes], 2 * page / elementBytes
}
