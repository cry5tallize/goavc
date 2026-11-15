package decoder

import (
	"fmt"
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/motion"
)

// readMVDComponent 实现 Spec: 9.3.3 的 UEG3，首 bin 依赖 A/B 邻块
// 的绝对 MVD 和。预测矢量与差分矢量分开缓存，不能把 MV 误作 MVD。
func (c *cabacSyntax) readMVDComponent(fs *frameState, bx, by, component int) (int32, error) {
	return c.readMVDForList(fs, bx, by, component, 0)
}

func (c *cabacSyntax) readMVDForList(fs *frameState, bx, by, component, list int) (int32, error) {
	grid, differences := fs.mv, fs.mvd
	if list == 1 {
		grid, differences = fs.mv1, fs.mvd1
	}
	sum := 0
	if fs.mbaff {
		for _, p := range [][2]int{{bx - 1, by}, {bx, by - 1}} {
			other, index := fs.locateMotionQuery(fs.activeMB, p[0], p[1])
			if other < 0 || !grid.Available(index%grid.BlockW, index/grid.BlockW) {
				continue
			}
			m := differences[index]
			if component == 0 {
				sum += absInt(int(m.X))
			} else {
				v := m.Y
				if fs.fieldAt(fs.activeMB) && !fs.fieldAt(other) {
					v /= 2
				} else if !fs.fieldAt(fs.activeMB) && fs.fieldAt(other) {
					v *= 2
				}
				sum += absInt(int(v))
			}
		}
	} else {
		for _, p := range [][2]int{{bx - 1, by}, {bx, by - 1}} {
			if grid.Available(p[0], p[1]) {
				m := differences[p[1]*grid.BlockW+p[0]]
				if component == 0 {
					sum += absInt(int(m.X))
				} else {
					sum += absInt(int(m.Y))
				}
			}
		}
	}
	base := 40 + 7*component
	if c.bin(base+boolToInt(sum > 2)+boolToInt(sum > 32)) == 0 {
		return 0, c.d.Err()
	}
	magnitude, inc := 1, base+3
	for magnitude < 9 && c.d.Err() == nil && c.bin(inc) != 0 {
		if magnitude < 4 {
			inc++
		}
		magnitude++
	}
	if magnitude == 9 {
		k := 3
		for c.d.Err() == nil && c.d.Bypass() != 0 {
			magnitude += 1 << uint(k)
			k++
			if k > 15 {
				return 0, fmt.Errorf("decoder: CABAC MVD escape exceeds bound: %w", h264.ErrInvalidData)
			}
		}
		for j := k - 1; j >= 0; j-- {
			magnitude += int(c.d.Bypass()) << uint(j)
		}
	}
	if magnitude > motion.MaxAbsMV {
		return 0, fmt.Errorf("decoder: CABAC MVD exceeds range: %w", h264.ErrInvalidData)
	}
	if c.d.Bypass() != 0 {
		magnitude = -magnitude
	}
	return int32(magnitude), c.d.Err()
}
