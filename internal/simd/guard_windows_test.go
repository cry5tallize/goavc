//go:build windows

package simd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// guardedRows的unsafe只用于测试：固定上限的无指针Go byte backing先pin，
// 保护页完全位于该对象内部（不含对象头/相邻对象），退出前恢复可读写再unpin。
// 不将uintptr转回Go指针；uint16视图只在2字节对齐、偶数len的同一对象内建立。
func guardedRows(t *testing.T, width, height, elementBytes int) ([]byte, int) {
	t.Helper()
	page := os.Getpagesize()
	if width < 1 || width > 64 || height < 1 || height > 32 || elementBytes < 1 || elementBytes > 2 || width*elementBytes > page {
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

func TestGuardPage_SAD(t *testing.T) {
	if os.Getenv("GOAVC_GUARD_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestGuardPage_SAD$", "-test.count=1", "-test.v")
		cmd.Env = append(os.Environ(), "GOAVC_GUARD_CHILD=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("保护页子进程失败：%v\n%s", err, out)
		}
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "=== RUN   TestGuardPage_SAD/") && strings.Count(line, "/") == 1 {
				t.Log(line)
			}
		}
		t.Log("保护页子进程通过；每条有效行均在PAGE_NOACCESS之前结束")
		return
	}
	tables := []struct {
		name  string
		table kernelTable
	}{{"Generic", genericTable()}, {"Auto", kernels}}
	if nativeCPU.sse2 {
		tables = append(tables, struct {
			name  string
			table kernelTable
		}{"SSE2", selectKernels(cpuFeatures{sse2: true})})
	}
	if nativeCPU.avx2 {
		tables = append(tables, struct {
			name  string
			table kernelTable
		}{"AVX2", selectKernels(cpuFeatures{sse2: true, avx2: true})})
	}
	if nativeCPU.neon {
		tables = append(tables, struct {
			name  string
			table kernelTable
		}{"NEON", selectKernels(cpuFeatures{neon: true})})
	}
	for _, backend := range tables {
		t.Run(backend.name, func(t *testing.T) {
			for _, width := range []int{1, 3, 4, 7, 8, 13, 15, 16, 17, 31} {
				for _, height := range []int{1, 3, 16, 17} {
					for _, element := range []int{1, 2} {
						t.Run(fmt.Sprintf("%dx%d/u%d", width, height, element*8), func(t *testing.T) {
							a, as := guardedRows(t, width, height, element)
							b, bs := guardedRows(t, width, height, element)
							if element == 1 {
								for y := range height {
									for x := range width {
										b[y*bs+x] = 255
									}
								}
								want := width * height * 255
								if got := backend.table.sad8(a, as, b, bs, width, height); got != want {
									t.Fatalf("SAD8=%d，期望%d", got, want)
								}
							} else {
								// first与stride均为偶数，len也为偶数；视图不超出原Go对象。
								a16 := unsafe.Slice((*uint16)(unsafe.Pointer(&a[0])), len(a)/2)
								b16 := unsafe.Slice((*uint16)(unsafe.Pointer(&b[0])), len(b)/2)
								for y := range height {
									for x := range width {
										b16[y*bs+x] = 1023
									}
								}
								want := width * height * 1023
								if got := backend.table.sad16(a16, as, b16, bs, width, height); got != want {
									t.Fatalf("SAD16=%d，期望%d", got, want)
								}
							}
						})
					}
				}
			}
		})
	}
}
