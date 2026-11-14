package cavlc

import (
	"errors"
	"strings"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
)

// reader422Bits 在前方补齐字节而非尾部补零，确保截断测试只有指定的有效位。
func reader422Bits(t *testing.T, text string) *bitstream.Reader {
	t.Helper()
	w := bitstream.NewWriter()
	padding := (8 - len(text)%8) % 8
	w.WriteBits(uint(padding), 0)
	for _, ch := range text {
		if ch != '0' && ch != '1' {
			t.Fatal(text)
		}
		w.WriteBits(1, uint64(ch-'0'))
	}
	r := bitstream.NewReader(w.Bytes())
	r.ReadBits(uint(padding))
	return r
}

func TestChromaDC422_NormativeCodewords(t *testing.T) {
	// ITU-T H.264 (11/2007) 表9-5，nC=-2；空项在规范中无定义。
	tokens := [36]string{
		"1", "", "", "",
		"0001111", "01", "", "",
		"0001110", "0001101", "001", "",
		"000000111", "0001100", "0001011", "00001",
		"000000110", "000000101", "0001010", "000001",
		"0000000111", "0000000110", "000000100", "0001001",
		"00000000111", "00000000110", "0000000101", "0001000",
		"000000000111", "000000000110", "00000000101", "0000000100",
		"0000000000111", "000000000101", "000000000100", "00000000100",
	}
	for symbol, code := range tokens {
		if code == "" {
			if chromaDC422CoeffTokenLen[symbol] != 0 {
				t.Fatalf("undefined token %d has a code", symbol)
			}
			continue
		}
		r := reader422Bits(t, code)
		got, err := chromaDC422Token.decode(r)
		if err != nil || int(got) != symbol || r.BitsRemaining() != 0 {
			t.Fatalf("token %s: symbol=%d want=%d remaining=%d: %v", code, got, symbol, r.BitsRemaining(), err)
		}
		if int(chromaDC422CoeffTokenLen[symbol]) != len(code) {
			t.Fatal(symbol, code)
		}
	}
	// 表9-9(b) 转置成行=tzVlcIndex，独立写出全部合法 total_zeros 码字。
	zeros := [7][]string{
		{"1", "010", "011", "0010", "0011", "0001", "00001", "00000"},
		{"000", "01", "001", "100", "101", "110", "111"},
		{"000", "001", "01", "10", "110", "111"},
		{"110", "00", "01", "10", "111"},
		{"00", "01", "10", "11"},
		{"00", "01", "1"},
		{"0", "1"},
	}
	for row, codes := range zeros {
		for value, code := range codes {
			r := reader422Bits(t, code)
			got, err := chromaDC422Zeros[row].decode(r)
			if err != nil || int(got) != value || r.BitsRemaining() != 0 {
				t.Fatalf("totalCoeff=%d code=%s: got=%d want=%d: %v", row+1, code, got, value, err)
			}
			if int(chromaDC422TotalZerosLen[row][value]) != len(code) {
				t.Fatal(row, value, code)
			}
		}
	}
}

func TestChromaDC422_IndependentBlockBits(t *testing.T) {
	for _, tc := range []struct {
		src  [16]int32
		bits string
		nz   int
	}{
		{[16]int32{}, "1", 0},
		{[16]int32{1}, "0101", 1},
		{[16]int32{-1}, "0111", 1},
		{[16]int32{2}, "000111111", 1},
		{[16]int32{-2}, "0001111011", 1},
		// 唯一非零在扫描末位：token 01、负号1、total_zeros=7 的 00000。
		{[16]int32{7: -1}, "01100000", 1},
		// 扫描第二项是光栅2；total_zeros=1 的码字为010。
		{[16]int32{2: 1}, "010010", 1},
		// 八个+1：token(8,3)、三符号、首level=1、四个suffixLength=1的10。
		{[16]int32{1, 1, 1, 1, 1, 1, 1, 1}, "00000000100" + "000" + "1" + "10101010", 8},
	} {
		w := bitstream.NewWriter()
		count, err := EncodeBlock(w, &tc.src, ChromaDC422, -2)
		if err != nil || count != tc.nz || w.BitsWritten() != uint64(len(tc.bits)) {
			t.Fatal(tc, count, w.BitsWritten(), err)
		}
		raw := bitstream.NewReader(w.Bytes())
		var actual strings.Builder
		for range w.BitsWritten() {
			bit, _ := raw.ReadBits(1)
			actual.WriteByte('0' + byte(bit))
		}
		if actual.String() != tc.bits {
			t.Fatalf("%v: bits %s want %s", tc.src, actual.String(), tc.bits)
		}
		var dst [16]int32
		r := reader422Bits(t, tc.bits)
		count, err = DecodeBlock(r, &dst, ChromaDC422, -2)
		if err != nil || count != tc.nz || dst != tc.src || r.BitsRemaining() != 0 {
			t.Fatal(tc, dst, count, err)
		}
	}
}

func TestChromaDC422_EscapesAndTruncation(t *testing.T) {
	for _, value := range []int32{1 << 24, -1 << 24} {
		src := [16]int32{7: value}
		w := bitstream.NewWriter()
		if _, err := EncodeBlock(w, &src, ChromaDC422, -2); err != nil {
			t.Fatal(err)
		}
		var dst [16]int32
		if _, err := DecodeBlock(bitstream.NewReader(w.Bytes()), &dst, ChromaDC422, -2); err != nil || dst != src {
			t.Fatal(dst, src, err)
		}
	}
	for _, value := range []int32{1<<31 - 1, -1 << 31} {
		src := [16]int32{value}
		w := bitstream.NewWriter()
		w.WriteBits(3, 5)
		if _, err := EncodeBlock(w, &src, ChromaDC422, -2); !errors.Is(err, h264.ErrInvalidData) || w.BitsWritten() != 3 {
			t.Fatal(value, w.BitsWritten(), err)
		}
	}
	fixture := "0001111011"
	for length := range len(fixture) {
		var dst [16]int32
		if _, err := DecodeBlock(reader422Bits(t, fixture[:length]), &dst, ChromaDC422, -2); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("accepted truncated %q: %v", fixture[:length], err)
		}
	}
	// token(1,0) 后出现 prefix=29，必须在读入逃逸幅值前拒绝。
	var dst [16]int32
	if _, err := DecodeBlock(reader422Bits(t, "0001111"+strings.Repeat("0", 29)+"1"), &dst, ChromaDC422, -2); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
}
