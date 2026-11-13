package cabac

import (
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/transform"
	"testing"
)

func TestResidual_SinglePositiveCoefficient(t *testing.T) {
	// 所有上下文从 state=0,MPS=0 开始；前三个决策为 1,1,0，
	// 依次表示 significant、last、abs_level_minus1；最后旁路符号为0。
	// 初值 offset=420：第一次 LPS -> range480/offset300，第二次
	// LPS -> range480/offset120，第三次 MPS -> range480/offset240；
	// 旁路读0得到 offset480，输出符号1。因此取 offset=419：
	// 相同决策后 offset232，旁路读0得464，符号0，幅值+1。
	w := bitstream.NewWriter()
	w.WriteBits(9, 419)
	w.WriteBits(7, 0)
	d, err := New(bitstream.NewReader(w.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	var ctx [ContextCount]Context
	var dst [16]int32
	scan := []int{0, 1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15}
	n, err := d.Residual(&ctx, Luma4x4, dst[:], scan)
	if err != nil || n != 1 || dst[0] != 1 {
		t.Fatalf("n=%d dst=%v err=%v", n, dst, err)
	}
}

func FuzzDecoder_Residual(f *testing.F) {
	f.Add([]byte{209, 128, 0}, uint8(2))
	f.Fuzz(func(t *testing.T, data []byte, cat uint8) {
		d, err := New(bitstream.NewReader(data))
		if err != nil {
			return
		}
		ctx, _ := InitContexts(true, 0, 26)
		var dst [64]int32
		scan := []int{0, 1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15}
		c := int(cat % 6)
		if c == ChromaDC {
			scan = []int{0, 1, 2, 3}
		} else if c == LumaAC || c == ChromaAC {
			scan = scan[1:]
		} else if c == Luma8x8 {
			scan = transform.Zigzag8x8[:]
		}
		n, err := d.Residual(&ctx, c, dst[:], scan)
		if err == nil && (n < 1 || n > len(scan)) {
			t.Fatal(n)
		}
	})
}
