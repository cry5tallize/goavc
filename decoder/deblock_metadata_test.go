package decoder

import (
	"errors"
	"runtime"
	"testing"
	"unsafe"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/internal/motion"
	"github.com/cry5tallize/goavc/syntax"
)

func TestDeblockNZ_FailedSkipDoesNotPublish(t *testing.T) {
	fs := deblockMotionFixture(t, 1, 1, true, false, false)
	fs.decoded = make([]bool, 1)
	s := &sliceEntry{
		hdr:  &syntax.SliceHeader{Type: syntax.SliceTypeB},
		data: bitstream.NewReader([]byte{0x40}), // mb_skip_run=1；故意不提供两列表参考。
	}
	if err := decodeMappedCAVLC(fs, s, 0, 0, 1); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("缺参考应返回 ErrInvalidData，实际 %v", err)
	}
	if fs.decoded[0] || fs.info[0].deblockNZReady || fs.info[0].deblockUniform {
		t.Fatal("失败的宏块发布了去块元数据")
	}
}

func TestDeblockNZ_MacroblockLayoutBudget(t *testing.T) {
	// 仅查询类型布局；预算按实际 Sizeof 记账。
	var m mbInfo
	end, align := unsafe.Offsetof(m.chromaMode)+unsafe.Sizeof(m.chromaMode), unsafe.Alignof(m)
	oldSize := (end + align - 1) / align * align
	maxSize := oldSize + align
	if runtime.GOARCH == "amd64" {
		maxSize = oldSize
	}
	if unsafe.Sizeof(m) > maxSize {
		t.Fatalf("宏块元数据从 %d 增至 %d 字节", oldSize, unsafe.Sizeof(m))
	}
}

func TestDeblockNZ_UniformInternalEdges(t *testing.T) {
	fs := deblockMotionFixture(t, 2, 2, true, false, false)
	// 夹具本身的 MV 并不一致，因此先构造与 Direct 同样的完整网格矩形。
	for _, grid := range []*motion.Grid{fs.mv, fs.mv1} {
		grid.SetRect(4, 4, 4, 4, motion.MV{X: 7, Y: -9}, 0)
	}
	for _, raw := range []uint8{0, 11} {
		fs.lumaNZ[3*16+5] = raw
		fs.info[3].deblockUniform = true
		fs.prepareDeblockNZ(3)
		for edge := 4; edge < 16; edge += 4 {
			for _, vertical := range []bool{false, true} {
				if got, want := fs.edgeStrengths(1, 1, edge, vertical), edgeStrengthsReference(fs, 1, 1, edge, vertical); got != want {
					t.Fatalf("一致运动边 %d（vertical=%t）= %v，期望 %v", edge, vertical, got, want)
				}
			}
		}
	}
}

func TestDeblockNZ_PreparationAndReset(t *testing.T) {
	var raw [16]uint8
	raw[5], raw[10] = 11, 3
	fs := frameState{lumaNZ: raw[:], info: []mbInfo{{transform8: true}}}
	fs.prepareDeblockNZ(0)
	if m := fs.info[0]; !m.deblockNZReady || m.deblockNZ != 0xcc33 || raw[5] != 11 || raw[10] != 3 {
		t.Fatalf("8×8 掩码或原计数错误: %#x %v", m.deblockNZ, raw)
	}
	// 复用和 Skip 都不能读取上次宏块的派生值。
	fs.info[0] = mbInfo{skip: true}
	if fs.info[0].deblockNZReady {
		t.Fatal("新宏块尚未提交")
	}
	fs.prepareDeblockNZ(0)
	if m := fs.info[0]; !m.deblockNZReady || m.deblockNZ != 0 {
		t.Fatalf("Skip 掩码错误: %#x", m.deblockNZ)
	}
	fs.info[0] = mbInfo{}
	fs.mbaff = true
	fs.prepareDeblockNZ(0)
	if fs.info[0].deblockNZReady {
		t.Fatal("MBAFF 不得发布逐行快路的掩码")
	}
}
