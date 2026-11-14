package transform

import "testing"

func TestFieldScan4x4_NormativeVector(t *testing.T) {
	// ITU-T H.264 (11/2007)表8-13：独立记录c_ij坐标，不从实现生成期望值。
	coords := [16][2]int{{0, 0}, {1, 0}, {0, 1}, {2, 0}, {3, 0}, {1, 1}, {2, 1}, {3, 1}, {0, 2}, {1, 2}, {2, 2}, {3, 2}, {0, 3}, {1, 3}, {2, 3}, {3, 3}}
	for k, ij := range coords {
		want := 4*ij[0] + ij[1]
		if FieldScan4x4[k] != want || FieldLumaDCScan[k] != want {
			t.Fatalf("field4/DC[%d]=%d/%d, want %d", k, FieldScan4x4[k], FieldLumaDCScan[k], want)
		}
	}
	if FieldScan4x4 == Zigzag4x4 || FieldLumaDCScan == LumaDCScan {
		t.Fatal("field scan incorrectly uses frame scan")
	}
}

func TestFieldScan8x8_NormativeVector(t *testing.T) {
	// 同版表8-14/图8-9(b)：每个空间位置中的扫描序号，和实现表方向相反。
	indices := [8][8]int{
		{0, 3, 8, 15, 22, 30, 38, 52},
		{1, 4, 14, 21, 29, 37, 45, 53},
		{2, 7, 16, 23, 31, 39, 46, 58},
		{5, 9, 20, 28, 36, 44, 51, 59},
		{6, 13, 24, 32, 40, 47, 54, 60},
		{10, 17, 25, 33, 41, 48, 55, 61},
		{11, 18, 26, 34, 42, 49, 56, 62},
		{12, 19, 27, 35, 43, 50, 57, 63},
	}
	for y, row := range indices {
		for x, k := range row {
			if FieldScan8x8[k] != y*8+x {
				t.Fatalf("field8[%d]=%d, want (%d,%d)", k, FieldScan8x8[k], y, x)
			}
		}
	}
}

func TestFieldScan8x8_CAVLCInterleavingFixedVectors(t *testing.T) {
	// §7.4.5.3.1的4*i+i4x4不受帧/场标志影响。四个CAVLC列表的固定空间落点。
	want := [4][16]int{
		{0, 9, 2, 56, 18, 26, 34, 27, 35, 28, 36, 29, 45, 7, 54, 39},
		{8, 24, 25, 33, 41, 11, 42, 12, 43, 13, 44, 14, 53, 15, 62, 47},
		{16, 32, 40, 10, 49, 4, 50, 5, 51, 6, 52, 22, 61, 38, 23, 55},
		{1, 17, 48, 3, 57, 19, 58, 20, 59, 21, 60, 37, 30, 46, 31, 63},
	}
	var raster [64]int
	for list := range 4 {
		for i := range 16 {
			pos := FieldScan8x8[4*i+list]
			if pos != want[list][i] {
				t.Fatalf("CAVLC list %d item %d maps to %d, want %d", list, i, pos, want[list][i])
			}
			raster[pos] = 100*list + i + 1
		}
	}
	if raster[0] != 1 || raster[8] != 101 || raster[16] != 201 || raster[1] != 301 || raster[63] != 316 {
		t.Fatal("CAVLC field interleaving changed", raster)
	}
}

func TestFieldScan_ChromaDCIsFrameFieldInvariant(t *testing.T) {
	// §8.5.4式(8-309)的空间序号，不是FieldScan4x4截取；没有场选择分支。
	spatial422 := [8]int{0, 2, 1, 5, 3, 6, 4, 7}
	for pos, scan := range spatial422 {
		if DC2x4Scan[scan] != pos {
			t.Fatalf("422 DC scan %d maps to %d, want %d", scan, DC2x4Scan[scan], pos)
		}
	}
	if DC2x2Scan != [4]int{0, 1, 2, 3} {
		t.Fatal("420 DC raster scan changed")
	}
}
