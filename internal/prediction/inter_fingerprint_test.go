package prediction

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/goavc/h264/internal/picture"
)

// 在K09a之前记录M7逐采样钳位算法的输出；覆盖大于一个tile的矩形，
// 单独保护纯标量重构的位精确性，不替代现有规范解析值或外部Oracle。
func TestMCBlock_M7Fingerprint(t *testing.T) {
	sum := sha256.New()
	for _, depth := range []int{8, 10} {
		p := picture.Plane{Width: 37, Height: 35, Stride: 42, BitDepth: depth}
		length := (p.Height-1)*p.Stride + p.Width
		if depth == 8 {
			p.Data = make([]byte, length+1)[1:]
		} else {
			p.Data16 = make([]uint16, length+1)[1:]
		}
		for y := range p.Height {
			for x := range p.Width {
				p.SetSample(x, y, uint16((x*x*11+y*79+x*y*17)%((1<<depth)-1)))
			}
		}
		for _, size := range []int{1, 3, 7, 13, 16, 17, 31} {
			for _, pos := range [][2]int{{-5, -4}, {0, 0}, {2, 3}, {15, 13}, {34, 33}, {40, 41}} {
				for _, sampling := range [][2]int{{1, 1}, {2, 2}, {2, 1}} {
					for dy := -4; dy < 4; dy++ {
						for dx := -4; dx < 4; dx++ {
							stride := size + 3
							length := (size-1)*stride + size
							if depth == 8 {
								dst := make([]byte, length)
								for i := range dst {
									dst[i] = 0xa5
								}
								MCBlockChromaFormat(dst, stride, &p, pos[0], pos[1], size, size, dx, dy, sampling[0], sampling[1])
								sum.Write(dst)
							} else {
								dst := make([]uint16, length)
								for i := range dst {
									dst[i] = 0xa5a5
								}
								MCBlockChromaFormatHigh(dst, stride, &p, pos[0], pos[1], size, size, dx, dy, sampling[0], sampling[1])
								out := make([]byte, len(dst)*2)
								for i, value := range dst {
									binary.LittleEndian.PutUint16(out[i*2:], value)
								}
								sum.Write(out)
							}
						}
					}
				}
			}
		}
	}
	got := fmt.Sprintf("%x", sum.Sum(nil))
	const want = "ff62c7838da485c15516101ba5438203a6320500d05f85c6383a458b0209bddd"
	if got != want {
		t.Fatalf("M7 MC指纹=%s，期望%s", got, want)
	}
}
