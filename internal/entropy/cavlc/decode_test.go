package cavlc

import (
	"errors"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/internal/transform"
)

func TestDecodeBlock_SingleTrailingOne(t *testing.T) {
	// nC=0，totalCoeff=1、trailingOnes=1：
	// coeff_token(sym=5, len=2, bits=1) = "01"
	// trailing sign=0 -> +1
	// total_zeros=0 -> 表 9-7 行 0 列 0 = "1"
	w := bitstream.NewWriter()
	writeVLC(t, w, coeffTokenLen[0][:], coeffTokenBits[0][:], 5)
	if err := w.WriteBits(1, 0); err != nil {
		t.Fatal(err)
	}
	writeVLC(t, w, totalZerosLen[0][:], totalZerosBits[0][:], 0)

	var dst [16]int32
	nnz, err := DecodeBlock(newReader(t, w), &dst, Luma4x4, 0)
	if err != nil {
		t.Fatalf("DecodeBlock: %v", err)
	}
	if nnz != 1 {
		t.Fatalf("nnz = %d, want 1", nnz)
	}
	if dst[transform.Zigzag4x4[0]] != 1 {
		t.Fatalf("level at scan pos 0 = %d, want 1", dst[transform.Zigzag4x4[0]])
	}
	for i, v := range dst {
		if i != transform.Zigzag4x4[0] && v != 0 {
			t.Fatalf("dst[%d] = %d, want 0", i, v)
		}
	}
}

func TestDecodeBlock_SingleTrailingOneNegative(t *testing.T) {
	w := bitstream.NewWriter()
	writeVLC(t, w, coeffTokenLen[0][:], coeffTokenBits[0][:], 5) // total=1, TO=1
	if err := w.WriteBits(1, 1); err != nil {                    // sign bit 1 -> -1
		t.Fatal(err)
	}
	writeVLC(t, w, totalZerosLen[0][:], totalZerosBits[0][:], 0)

	var dst [16]int32
	if _, err := DecodeBlock(newReader(t, w), &dst, Luma4x4, 0); err != nil {
		t.Fatal(err)
	}
	if got := dst[0]; got != -1 {
		t.Fatalf("dst[0] = %d, want -1", got)
	}
}

func TestDecodeBlock_TwoCoeffsWithZeros(t *testing.T) {
	// totalCoeff=2、trailingOnes=2：table0 sym=10，码字 "001"；
	// 两个符号位 0,0 -> +1,+1；
	// total_zeros=14（行 1 列 14）码字 6 位 000000；
	// run_before=0 -> 行 6 的 "111"。
	w := bitstream.NewWriter()
	writeVLC(t, w, coeffTokenLen[0][:], coeffTokenBits[0][:], 10)
	if err := w.WriteBits(2, 0); err != nil {
		t.Fatal(err)
	}
	writeVLC(t, w, totalZerosLen[1][:], totalZerosBits[1][:], 14)
	writeVLC(t, w, runLen[6][:], runBits[6][:], 0)

	var dst [16]int32
	nnz, err := DecodeBlock(newReader(t, w), &dst, Luma4x4, 0)
	if err != nil {
		t.Fatalf("DecodeBlock: %v", err)
	}
	if nnz != 2 {
		t.Fatalf("nnz = %d, want 2", nnz)
	}
	// 扫描位置 15->raster 15，14->raster 14。
	if dst[transform.Zigzag4x4[15]] != 1 || dst[transform.Zigzag4x4[14]] != 1 {
		t.Fatalf("unexpected coefficients: %v", dst)
	}
}

func TestDecodeBlock_LumaAC(t *testing.T) {
	// I16x16 AC 块只有 15 个系数，扫描表从 Zigzag4x4[1] 开始。
	// totalCoeff=1,TO=1,total_zeros=14 -> 最后一个 AC 位置。
	w := bitstream.NewWriter()
	writeVLC(t, w, coeffTokenLen[0][:], coeffTokenBits[0][:], 5)
	if err := w.WriteBits(1, 0); err != nil {
		t.Fatal(err)
	}
	writeVLC(t, w, totalZerosLen[0][:], totalZerosBits[0][:], 14)

	var dst [16]int32
	nnz, err := DecodeBlock(newReader(t, w), &dst, LumaAC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if nnz != 1 {
		t.Fatalf("nnz = %d, want 1", nnz)
	}
	// AC 扫描位置 14 对应 Zigzag4x4[15]=15；DC 位置必须保持 0。
	if dst[15] != 1 || dst[0] != 0 {
		t.Fatalf("dst=%v, want level at raster 15 and DC 0", dst)
	}
}

func TestDecodeBlock_ChromaDC(t *testing.T) {
	// 色度 DC：totalCoeff=1,TO=1 -> chroma 表 sym=5，码字 "1"；
	// 符号位 0；total_zeros=3 -> 行 0 列 3 码字 "000"。
	w := bitstream.NewWriter()
	writeVLC(t, w, chromaDCCoeffTokenLen[:], chromaDCCoeffTokenBits[:], 5)
	if err := w.WriteBits(1, 0); err != nil {
		t.Fatal(err)
	}
	writeVLC(t, w, chromaDCTotalZerosLen[0][:], chromaDCTotalZerosBits[0][:], 3)

	var dst [16]int32
	nnz, err := DecodeBlock(newReader(t, w), &dst, ChromaDC, -1)
	if err != nil {
		t.Fatal(err)
	}
	if nnz != 1 || dst[3] != 1 {
		t.Fatalf("nnz=%d dst=%v, want nnz=1 dst[3]=1", nnz, dst)
	}
}

func TestDecodeBlock_LumaDCSpatialScan(t *testing.T) {
	// Spec: 表 9-5、9-7。01=一个 trailing one，0=正，011=一个零；
	// 系数位于扫描位置 1，即光栅坐标 (1,0)，不能转置为 (0,1)。
	r := bitstream.NewReader([]byte{0b01001100})
	var dst [16]int32
	n, err := DecodeBlock(r, &dst, LumaDC, 0)
	if err != nil || n != 1 || dst[1] != 1 || dst[4] != 0 {
		t.Fatalf("n=%d dst=%v err=%v", n, dst, err)
	}
}

func TestDecodeBlock_Truncated(t *testing.T) {
	// 只有 coeff_token 的前两位，后续 total_zeros 缺失。
	w := bitstream.NewWriter()
	if err := w.WriteBits(2, 0b01); err != nil {
		t.Fatal(err)
	}
	var dst [16]int32
	_, err := DecodeBlock(newReader(t, w), &dst, Luma4x4, 0)
	if !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("err = %v, want ErrInvalidData", err)
	}
}

func TestDecodeBlock_InvalidTableIndex(t *testing.T) {
	// nC 超出 16 时内部钳到 16，不应 panic。
	w := bitstream.NewWriter()
	writeVLC(t, w, coeffTokenLen[3][:], coeffTokenBits[3][:], 0)
	var dst [16]int32
	if _, err := DecodeBlock(newReader(t, w), &dst, Luma4x4, 999); err != nil {
		t.Fatalf("DecodeBlock: %v", err)
	}
}

func FuzzDecodeBlock(f *testing.F) {
	f.Add([]byte{0x01, 0x00}, uint8(Luma4x4), uint8(0))
	f.Add([]byte{0xFF, 0xFF}, uint8(ChromaDC), uint8(0))
	f.Fuzz(func(t *testing.T, data []byte, typ, nC uint8) {
		if typ > uint8(ChromaDC) {
			typ = uint8(Luma4x4)
		}
		var dst [16]int32
		r := bitstream.NewReader(data)
		// 不关心结果；只要求不 panic、错误可分类。
		_, err := DecodeBlock(r, &dst, BlockType(typ), int(nC))
		if err != nil && !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("unexpected error class: %v", err)
		}
	})
}
