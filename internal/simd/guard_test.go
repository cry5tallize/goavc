package simd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"unsafe"
)

// guardChild 将硬件越界隔离到子进程；父进程保留崩溃输出而不杀死其余测试。
func guardChild(t *testing.T) bool {
	t.Helper()
	if !guardPagesAvailable {
		t.Skip("本平台保护页未实现；不计作动态边界通过")
	}
	if os.Getenv("GOAVC_GUARD_CHILD") == t.Name() {
		return true
	}
	cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), "GOAVC_GUARD_CHILD="+t.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("保护页子进程失败：%v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "=== RUN   "+t.Name()+"/") && strings.Count(line, "/") == 1 {
			t.Log(line)
		}
	}
	t.Log("保护页子进程通过；每条有效行均在不可访问页之前结束")
	return false
}

type guardBackend struct {
	name  string
	table kernelTable
}

func guardBackends() []guardBackend {
	tables := []guardBackend{{"Generic", genericTable()}, {"Auto", kernels}}
	if nativeCPU.sse2 {
		tables = append(tables, guardBackend{"SSE2", selectKernels(cpuFeatures{sse2: true})})
	}
	if nativeCPU.avx2 {
		tables = append(tables, guardBackend{"AVX2", selectKernels(cpuFeatures{sse2: true, avx2: true})})
	}
	if nativeCPU.neon {
		tables = append(tables, guardBackend{"NEON", selectKernels(cpuFeatures{neon: true})})
	}
	return tables
}

// 页起点及行距均对齐到采样宽度；视图只覆盖同一 backing 内的有效字节。
func guardUint16(b []byte) []uint16 { return unsafe.Slice((*uint16)(unsafe.Pointer(&b[0])), len(b)/2) }
func guardInt32(b []byte) []int32   { return unsafe.Slice((*int32)(unsafe.Pointer(&b[0])), len(b)/4) }
func TestGuardPage_SAD(t *testing.T) {
	if !guardChild(t) {
		return
	}
	tables := guardBackends()
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
