package filter

import "testing"

func TestPreparedLuma8_LocalAllocation(t *testing.T) {
	p := PrepareEdge(32, 0, 0, 8)
	for _, c := range []struct {
		name                   string
		q, along, across, size int
	}{
		{"vertical", 4, 29, 1, 442},
		{"horizontal", 4*29 + 1, 1, 29, 191},
		{"vertical short", 4, 29, 1, 441},
		{"horizontal short", 4*29 + 1, 1, 29, 190},
	} {
		t.Run(c.name, func(t *testing.T) {
			bs := [4]uint8{1, 4, 3, 2}
			if n := testing.AllocsPerRun(100, func() {
				var data [512]byte
				for i := range data {
					data[i] = 100
				}
				p.Luma8(data[:c.size], c.q, c.along, c.across, bs)
				if data[c.q] != 100 {
					panic("常量不应改变")
				}
			}); n != 0 {
				t.Fatalf("局部数组分配 %g 次，期望0", n)
			}
		})
	}
}
