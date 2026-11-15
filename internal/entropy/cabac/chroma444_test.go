package cabac

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"strconv"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
)

// 下列固定值独立摘自ITU-T H.264表9-39..9-43，不引用被测映射表。
var normative444Bases = [2][14][3]int{
	{{105, 166, 227}, {120, 181, 237}, {134, 195, 247}, {149, 210, 257}, {152, 213, 266}, {402, 417, 426}, {484, 572, 952}, {499, 587, 962}, {513, 601, 972}, {660, 690, 708}, {528, 616, 982}, {543, 631, 992}, {557, 645, 1002}, {718, 748, 766}},
	{{277, 338, 227}, {292, 353, 237}, {306, 367, 247}, {321, 382, 257}, {324, 385, 266}, {436, 451, 426}, {776, 864, 952}, {791, 879, 962}, {805, 893, 972}, {675, 699, 708}, {820, 908, 982}, {835, 923, 992}, {849, 937, 1002}, {733, 757, 766}},
}
var normative444Sig = [2][63]int{
	{0, 1, 2, 3, 4, 5, 5, 4, 4, 3, 3, 4, 4, 4, 5, 5, 4, 4, 4, 4, 3, 3, 6, 7, 7, 7, 8, 9, 10, 9, 8, 7, 7, 6, 11, 12, 13, 11, 6, 7, 8, 9, 14, 10, 9, 8, 6, 11, 12, 13, 11, 6, 9, 14, 10, 9, 11, 12, 13, 11, 14, 10, 12},
	{0, 1, 1, 2, 2, 3, 3, 4, 5, 6, 7, 7, 7, 8, 4, 5, 6, 9, 10, 10, 8, 11, 12, 11, 9, 9, 10, 10, 8, 11, 12, 11, 9, 9, 10, 10, 8, 11, 12, 11, 9, 9, 10, 10, 8, 13, 13, 9, 9, 10, 10, 8, 13, 13, 9, 9, 10, 10, 14, 14, 14, 14, 14},
}
var normative444Last = [63]int{0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 3, 3, 3, 3, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4, 5, 5, 5, 5, 6, 6, 6, 6, 7, 7, 7, 7, 8, 8, 8}

func normative444Scan(cat, mode int) []int {
	// Spec: 表8-13/8-14的帧、场反扫描；色度DC另按4或8项扫描。
	four := [2][16]int{
		{0, 1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15},
		{0, 4, 1, 8, 12, 5, 9, 13, 2, 6, 10, 14, 3, 7, 11, 15},
	}
	eight := [2][64]int{
		{0, 1, 8, 16, 9, 2, 3, 10, 17, 24, 32, 25, 18, 11, 4, 5, 12, 19, 26, 33, 40, 48, 41, 34, 27, 20, 13, 6, 7, 14, 21, 28, 35, 42, 49, 56, 57, 50, 43, 36, 29, 22, 15, 23, 30, 37, 44, 51, 58, 59, 52, 45, 38, 31, 39, 46, 53, 60, 61, 54, 47, 55, 62, 63},
		{0, 8, 16, 1, 9, 24, 32, 17, 2, 25, 40, 48, 56, 33, 10, 3, 18, 41, 49, 57, 26, 11, 4, 19, 34, 42, 50, 58, 27, 12, 5, 20, 35, 43, 51, 59, 28, 13, 6, 21, 36, 44, 52, 60, 29, 14, 22, 37, 45, 53, 61, 30, 7, 15, 38, 46, 54, 62, 23, 31, 39, 47, 55, 63},
	}
	switch cat {
	case 3:
		return []int{0, 1, 2, 3}
	case 1, 4, 7, 11:
		return four[mode][1:]
	case 5, 9, 13:
		return eight[mode][:]
	default:
		return four[mode][:]
	}
}

func TestCodedBlockFlagBase_AllNormativeCategories(t *testing.T) {
	want := [14]int{85, 89, 93, 97, 101, 1012, 460, 464, 468, 1016, 472, 476, 480, 1020}
	if CbDC != 6 || CbAC != 7 || Cb4x4 != 8 || Cb8x8 != 9 || CrDC != 10 || CrAC != 11 || Cr4x4 != 12 || Cr8x8 != 13 {
		t.Fatal("444类别编号偏离规范")
	}
	for cat, base := range want {
		got, err := CodedBlockFlagBase(cat)
		if err != nil || got != base {
			t.Fatal(cat, got, base, err)
		}
	}
	for _, cat := range []int{-1, 14, 1 << 30} {
		if _, err := CodedBlockFlagBase(cat); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal(cat, err)
		}
	}
}

func TestResidual444_FixedNormativeCodeword(t *testing.T) {
	// 独立区间推导：初值offset=419，bin=1,1,0，符号=0，码字D180。
	// 此向量不经过任何编码器，检查每类的首个sig/last/level上下文。
	for mode := range 2 {
		for cat, bases := range normative444Bases[mode] {
			d, err := New(bitstream.NewReader([]byte{0xd1, 0x80}))
			if err != nil {
				t.Fatal(err)
			}
			var ctx, want [ContextCount444]Context
			var dst [64]int32
			count, err := d.Residual444(&ctx, cat, dst[:], normative444Scan(cat, mode), mode != 0)
			want[bases[0]] = Context{MPS: 1}
			want[bases[1]] = Context{MPS: 1}
			want[bases[2]+1] = Context{State: 1}
			if err != nil || count != 1 || dst[normative444Scan(cat, mode)[0]] != 1 || ctx != want {
				t.Fatalf("cat=%d field=%d n=%d dst=%v err=%v", cat, mode, count, dst, err)
			}
		}
	}
}

// 不调用Residual，按规范bin/ctxIdx轨迹独立建立单个负二系数向量。
func normative444Fixture(t *testing.T, cat, mode, position int) ([]byte, [ContextCount444]Context) {
	t.Helper()
	ctx, err := InitContexts444(false, 2, 37)
	if err != nil {
		t.Fatal(err)
	}
	w := bitstream.NewWriter()
	e, err := NewEncoder(w)
	if err != nil {
		t.Fatal(err)
	}
	bases := normative444Bases[mode][cat]
	scan := normative444Scan(cat, mode)
	for pos := 0; pos <= position && pos < len(scan)-1; pos++ {
		sig, last := pos, pos
		if len(scan) == 64 {
			sig, last = normative444Sig[mode][pos], normative444Last[pos]
		}
		if pos == position {
			e.Bin(&ctx[bases[0]+sig], 1)
			e.Bin(&ctx[bases[1]+last], 1)
		} else {
			e.Bin(&ctx[bases[0]+sig], 0)
		}
	}
	e.Bin(&ctx[bases[2]+1], 1)
	e.Bin(&ctx[bases[2]+5], 0)
	e.Bypass(1)
	e.Terminate(1)
	if e.Err() != nil {
		t.Fatal(e.Err())
	}
	return w.Bytes(), ctx
}

func TestResidual444_EveryCategoryAndFrameFieldPosition(t *testing.T) {
	for mode := range 2 {
		for cat := range 14 {
			scan := normative444Scan(cat, mode)
			for position := range scan {
				t.Run(fmt.Sprintf("%d/%d/%d", cat, mode, position), func(t *testing.T) {
					data, wantCtx := normative444Fixture(t, cat, mode, position)
					ctx, _ := InitContexts444(false, 2, 37)
					d, err := New(bitstream.NewReader(data))
					if err != nil {
						t.Fatal(err)
					}
					var src, dst [64]int32
					src[scan[position]] = -2
					n, err := d.Residual444(&ctx, cat, dst[:], scan, mode != 0)
					if err != nil || n != 1 || dst != src || ctx != wantCtx || d.Terminate() != 1 || d.Err() != nil {
						t.Fatal(n, dst, src, err, d.Err())
					}
					ctx, _ = InitContexts444(false, 2, 37)
					w := bitstream.NewWriter()
					e, _ := NewEncoder(w)
					n, err = e.Residual444(&ctx, cat, src[:], scan, mode != 0)
					e.Terminate(1)
					if err != nil || n != 1 || ctx != wantCtx || !bytes.Equal(w.Bytes(), data) || e.Err() != nil {
						t.Fatal(n, w.Bytes(), data, err, e.Err())
					}
				})
			}
		}
	}
}

func TestInitContexts444_AllModelsAndQPBoundaries(t *testing.T) {
	f, err := os.Open("context_init.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	// 独立摘自Amd.2表9-24a..i的数值重复区间，不读取生成器alias文件。
	ranges := [][3]int{
		{460, 471, 85}, {472, 483, 85}, {484, 527, 105}, {528, 571, 105}, {572, 615, 166}, {616, 659, 166},
		{660, 674, 402}, {675, 689, 436}, {690, 698, 417}, {699, 707, 451}, {708, 717, 426},
		{718, 732, 402}, {733, 747, 436}, {748, 756, 417}, {757, 765, 451}, {766, 775, 426},
		{776, 819, 277}, {820, 863, 277}, {864, 907, 338}, {908, 951, 338}, {952, 981, 227}, {982, 1011, 227},
		{1012, 1015, 93}, {1016, 1019, 93}, {1020, 1023, 93},
	}
	qps := []int{-128, -1}
	for qp := range 52 {
		qps = append(qps, qp)
	}
	qps = append(qps, 52, 127)
	for mode := range 4 {
		for _, qp := range qps {
			idc := uint32(max(0, mode-1))
			ctx, err := InitContexts444(mode == 0, idc, qp)
			if err != nil {
				t.Fatal(err)
			}
			old, err := InitContexts(mode == 0, idc, qp)
			if err != nil {
				t.Fatal(err)
			}
			for i := range ctx {
				source := i
				for _, r := range ranges {
					if i >= r[0] && i <= r[1] {
						source = r[2] + i - r[0]
						break
					}
				}
				m, errM := strconv.Atoi(rows[source+1][1+mode*2])
				n, errN := strconv.Atoi(rows[source+1][2+mode*2])
				if errM != nil || errN != nil || initMN[mode][i] != [2]int8{int8(m), int8(n)} {
					t.Fatalf("m/n mode=%d ctx=%d source=%d", mode, i, source)
				}
				product := m * max(0, min(51, qp))
				pre := product / 16
				if product < 0 && product%16 != 0 {
					pre--
				}
				pre = max(1, min(126, pre+n))
				want := Context{State: uint8(63 - pre)}
				if pre >= 64 {
					want = Context{State: uint8(pre - 64), MPS: 1}
				}
				if ctx[i] != want || i < ContextCount && old[i] != want {
					t.Fatalf("mode=%d qp=%d ctx=%d got=%v want=%v", mode, qp, i, ctx[i], want)
				}
			}
		}
	}
	for _, intra := range []bool{false, true} {
		_, err := InitContexts444(intra, ^uint32(0), 26)
		if !intra && !errors.Is(err, h264.ErrInvalidData) || intra && err != nil {
			t.Fatal(intra, err)
		}
	}
}

func TestInitContexts444_FixedNormativeMNValues(t *testing.T) {
	// Amd.2表9-24a..i（整合版表9-25..9-33）的独立固定锚点。
	// 表9-24d/e将8×8上下文重排为帧sig、场sig、帧last、场last、level，
	// 因此660..717和718..775均不能连续复制亮度402..459。
	vectors := []struct {
		index int
		mn    [4][2]int8
	}{
		{460, [4][2]int8{{-17, 123}, {-7, 92}, {0, 80}, {11, 80}}},
		{483, [4][2]int8{{-30, 127}, {-23, 126}, {-31, 127}, {-30, 127}}},
		{484, [4][2]int8{{-7, 93}, {-2, 85}, {-13, 103}, {-4, 86}}},
		{571, [4][2]int8{{18, 59}, {13, 68}, {18, 55}, {13, 91}}},
		{572, [4][2]int8{{24, 0}, {11, 28}, {4, 45}, {4, 39}}},
		{659, [4][2]int8{{41, 17}, {17, 70}, {29, 41}, {22, 73}}},
		{660, [4][2]int8{{-17, 120}, {-4, 79}, {-5, 85}, {-3, 78}}},
		{674, [4][2]int8{{-11, 74}, {-20, 63}, {2, 59}, {-11, 68}}},
		{675, [4][2]int8{{-14, 106}, {-5, 85}, {-3, 81}, {-3, 78}}},
		{689, [4][2]int8{{2, 59}, {2, 59}, {2, 58}, {-11, 68}}},
		{690, [4][2]int8{{23, -13}, {9, -2}, {17, -10}, {9, -2}}},
		{698, [4][2]int8{{19, 82}, {36, 59}, {47, 57}, {20, 64}}},
		{699, [4][2]int8{{21, -10}, {21, -13}, {17, -10}, {9, -2}}},
		{707, [4][2]int8{{14, 67}, {42, 62}, {47, 57}, {20, 64}}},
		{708, [4][2]int8{{-3, 75}, {-6, 66}, {-5, 71}, {-9, 71}}},
		{717, [4][2]int8{{-9, 92}, {-8, 76}, {-7, 82}, {-10, 79}}},
		{733, [4][2]int8{{-14, 106}, {-5, 85}, {-3, 81}, {-3, 78}}},
		{748, [4][2]int8{{23, -13}, {9, -2}, {17, -10}, {9, -2}}},
		{757, [4][2]int8{{21, -10}, {21, -13}, {17, -10}, {9, -2}}},
		{766, [4][2]int8{{-3, 75}, {-6, 66}, {-5, 71}, {-9, 71}}},
		{775, [4][2]int8{{-9, 92}, {-8, 76}, {-7, 82}, {-10, 79}}},
		{776, [4][2]int8{{-6, 93}, {-13, 106}, {-21, 126}, {-22, 127}}},
		{863, [4][2]int8{{-1, 83}, {6, 64}, {-2, 75}, {2, 70}}},
		{864, [4][2]int8{{15, 6}, {14, 11}, {19, -6}, {17, -13}}},
		{951, [4][2]int8{{13, 58}, {14, 71}, {25, 44}, {21, 62}}},
		{952, [4][2]int8{{-3, 71}, {-6, 76}, {-23, 112}, {-24, 115}}},
		{1011, [4][2]int8{{-17, 110}, {-10, 90}, {-9, 86}, {-18, 108}}},
		{1012, [4][2]int8{{-3, 70}, {-3, 74}, {-2, 73}, {-5, 79}}},
		{1023, [4][2]int8{{-30, 127}, {-23, 126}, {-31, 127}, {-30, 127}}},
	}
	for _, v := range vectors {
		for mode, pair := range v.mn {
			if initMN[mode][v.index] != pair {
				t.Fatal(v.index, mode, initMN[mode][v.index], pair)
			}
		}
	}
}
