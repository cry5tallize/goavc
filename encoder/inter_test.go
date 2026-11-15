package encoder

import (
	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/motion"
	"github.com/cry5tallize/goavc/internal/prediction"
	"github.com/cry5tallize/goavc/internal/testutil/pattern"
	"testing"
)

func TestEncoder_InterPartitionCoverageAndSkip(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width = 64
	c.Height = 64
	c.QP = 0
	e, err := New(c, WithFullSearch(), WithSearchRange(4))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err = e.SendFrame(pattern.Frame(c.Width, c.Height, 0)); err != nil {
		t.Fatal(err)
	}
	e.ReceivePacket()
	f := testFrame(c.Width, c.Height)
	for addr := 0; addr < 16; addr++ {
		kind := addr % 4
		x, y := addr%4*16, addr/4*16
		for i, p := range partitions[kind] {
			v := motion.MV{X: 8, Y: 4}
			switch kind {
			case 1:
				v = motion.MV{X: 8 - int32(i)*16}
			case 2:
				v = motion.MV{Y: 8 - int32(i)*16}
			case 3:
				v = motion.MV{X: 8 - int32(i%2)*16, Y: 8 - int32(i/2)*16}
			}
			prediction.MCBlockLuma(f.Planes[0].Data[(y+p.y)*f.Planes[0].Stride+x+p.x:], f.Planes[0].Stride, &e.reference.Planes[0], x+p.x, y+p.y, p.w, p.h, int(v.X), int(v.Y))
			for comp := 1; comp < 3; comp++ {
				prediction.MCBlockChroma(f.Planes[comp].Data[(y+p.y)/2*f.Planes[comp].Stride+(x+p.x)/2:], f.Planes[comp].Stride, &e.reference.Planes[comp], (x+p.x)/2, (y+p.y)/2, p.w/2, p.h/2, int(v.X), int(v.Y))
			}
		}
	}
	if err = e.SendFrame(f); err != nil {
		t.Fatal(err)
	}
	e.ReceivePacket()
	var counts [4]int
	for addr, info := range e.last.info {
		if !info.inter || info.pcm {
			t.Fatalf("MB %d chose %+v", addr, info)
		}
		counts[info.partition]++
		// RD 可以把同一运动场用更少 MVD 位的等价分区表示；仍严格核对每个
		// 4×4 块的实际矢量，而不是要求 SAD 阶段的原始分区形状保持不变。
		for r := 0; r < 16; r++ {
			kind := addr % 4
			want := motion.MV{X: 8, Y: 4}
			switch kind {
			case 1:
				want = motion.MV{X: 8 - int32(r/8)*16}
			case 2:
				want = motion.MV{Y: 8 - int32(r%4/2)*16}
			case 3:
				want = motion.MV{X: 8 - int32(r%4/2)*16, Y: 8 - int32(r/8)*16}
			}
			got, ref, ok := e.last.mv.Get(addr%4*4+r%4, addr/4*4+r/4)
			if !ok || ref != 0 || got != want {
				t.Fatalf("MB%d block%d MV=%v want=%v ref=%d", addr, r, got, want, ref)
			}
		}
	}
	for kind, count := range counts {
		if count == 0 {
			t.Fatalf("partition %d never selected: %v", kind, counts)
		}
	}
	static, _ := e.Reconstruction()
	defer static.Release()
	if err = e.SendFrame(static); err != nil {
		t.Fatal(err)
	}
	packet, _ := e.ReceivePacket()
	for addr, info := range e.last.info {
		if !info.skip {
			t.Fatalf("static MB %d is not skip", addr)
		}
	}
	if len(packet.Data) > 100 {
		t.Fatal("skip-only AU unexpectedly large", len(packet.Data))
	}
}
