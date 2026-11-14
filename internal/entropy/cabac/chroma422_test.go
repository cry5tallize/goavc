package cabac

import (
	"bytes"
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/transform"
)

// chroma422SyntaxFixture 只按规范的独立 bin/ctxIdx 轨迹编码，不调用 Residual，
// 防止编解码器使用同一错误位置表时自往返掩盖问题。
func chroma422SyntaxFixture(t *testing.T, cond int) ([]byte, [ContextCount]Context) {
	t.Helper()
	ctx, err := InitContexts(true, 0, 26)
	if err != nil {
		t.Fatal(err)
	}
	w := bitstream.NewWriter()
	e, err := NewEncoder(w)
	if err != nil {
		t.Fatal(err)
	}
	// ctxBlockCat=3 的 coded_block_flag 基址仍为97，cond=condA+2*condB。
	e.Bin(&ctx[97+cond], 1)
	// 规范9-16：min(levelListIdx/NumC8x8,2)，NumC8x8=2。
	for _, inc := range []int{0, 0, 1, 1, 2, 2, 2} {
		e.Bin(&ctx[149+inc], 1)
		e.Bin(&ctx[210+inc], 0)
	}
	// 扫描位置0..7的level为 {2,-1,3,1,-2,1,2,-3}；逆序读幅值。
	// 每项列出首bin、TU后缀、符号；最后一个>1仍饱和在ctx265而非266。
	e.Bin(&ctx[258], 1)
	e.Bin(&ctx[262], 1)
	e.Bin(&ctx[262], 0)
	e.Bypass(1)
	e.Bin(&ctx[257], 1)
	e.Bin(&ctx[263], 0)
	e.Bypass(0)
	e.Bin(&ctx[257], 0)
	e.Bypass(0)
	e.Bin(&ctx[257], 1)
	e.Bin(&ctx[264], 0)
	e.Bypass(1)
	e.Bin(&ctx[257], 0)
	e.Bypass(0)
	e.Bin(&ctx[257], 1)
	e.Bin(&ctx[265], 1)
	e.Bin(&ctx[265], 0)
	e.Bypass(0)
	e.Bin(&ctx[257], 0)
	e.Bypass(1)
	e.Bin(&ctx[257], 1)
	e.Bin(&ctx[265], 0)
	e.Bypass(0)
	e.Terminate(1)
	if e.Err() != nil {
		t.Fatal(e.Err())
	}
	return w.Bytes(), ctx
}

func TestChromaDC422_IndependentCABACSyntax(t *testing.T) {
	// 将独立扫描列表显式放到规范空间位置，避免借用被测扫描表生成期望。
	src := [8]int32{2, 3, -1, 1, 1, 2, -2, -3}
	for cond := range 4 {
		fixture, wantCtx := chroma422SyntaxFixture(t, cond)
		ctx, _ := InitContexts(true, 0, 26)
		d, err := New(bitstream.NewReader(fixture))
		if err != nil {
			t.Fatal(err)
		}
		if d.Bin(&ctx[97+cond]) != 1 {
			t.Fatal("422 coded_block_flag lost")
		}
		var dst [8]int32
		count, err := d.Residual(&ctx, ChromaDC, dst[:], transform.DC2x4Scan[:])
		if err != nil || count != 8 || dst != src || ctx != wantCtx || d.Terminate() != 1 || d.Err() != nil {
			t.Fatal(cond, count, dst, src, err, d.Err())
		}
		ctx, _ = InitContexts(true, 0, 26)
		w := bitstream.NewWriter()
		e, _ := NewEncoder(w)
		e.Bin(&ctx[97+cond], 1)
		count, err = e.Residual(&ctx, ChromaDC, src[:], transform.DC2x4Scan[:])
		e.Terminate(1)
		if err != nil || count != 8 || ctx != wantCtx || !bytes.Equal(w.Bytes(), fixture) || e.Err() != nil {
			t.Fatal(cond, count, w.Bytes(), fixture, err, e.Err())
		}
	}
}

func TestChromaDC422_CABACPositionAndBounds(t *testing.T) {
	for pos, inc := range []int{0, 0, 1, 1, 2, 2, 2} {
		sig, last := residualPositionContext(ChromaDC, 8, pos)
		if sig != inc || last != inc {
			t.Fatal(pos, sig, last, inc)
		}
	}
	for greater, inc := range []int{5, 6, 7, 8, 8, 8, 8, 8} {
		if got := residualGreaterContext(ChromaDC, 8, greater); got != inc {
			t.Fatal(greater, got, inc)
		}
	}
	for _, size := range []int{4, 8} {
		if !residualSizeValid(ChromaDC, size) {
			t.Fatal(size)
		}
	}
	for _, size := range []int{0, 1, 7, 9, 16, 64} {
		if residualSizeValid(ChromaDC, size) {
			t.Fatal(size)
		}
	}
	for pos := range 8 {
		for _, value := range []int32{1, -1, 1 << 24, -1 << 24} {
			var src, dst [8]int32
			src[transform.DC2x4Scan[pos]] = value
			ctx, _ := InitContexts(false, 2, 51)
			decoded := ctx
			w := bitstream.NewWriter()
			e, _ := NewEncoder(w)
			count, err := e.Residual(&ctx, ChromaDC, src[:], transform.DC2x4Scan[:])
			if err != nil || count != 1 {
				t.Fatal(pos, value, count, err)
			}
			e.Terminate(1)
			d, err := New(bitstream.NewReader(w.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			count, err = d.Residual(&decoded, ChromaDC, dst[:], transform.DC2x4Scan[:])
			if err != nil || count != 1 || dst != src || decoded != ctx || d.Terminate() != 1 || d.Err() != nil {
				t.Fatal(pos, value, count, dst, src, err, d.Err())
			}
		}
	}
	for _, value := range []int32{1<<24 + 1, -1<<24 - 1, 1<<31 - 1, -1 << 31} {
		ctx, _ := InitContexts(true, 0, 26)
		w := bitstream.NewWriter()
		e, _ := NewEncoder(w)
		src := [8]int32{value}
		if _, err := e.Residual(&ctx, ChromaDC, src[:], transform.DC2x4Scan[:]); !errors.Is(err, h264.ErrInvalidData) || e.BinCount() != 0 {
			t.Fatal(value, e.BinCount(), err)
		}
	}
}

func TestChromaDC422_CABACTruncationAndEscapeRejection(t *testing.T) {
	fixture, _ := chroma422SyntaxFixture(t, 0)
	for cut := range len(fixture) {
		ctx, _ := InitContexts(true, 0, 26)
		d, err := New(bitstream.NewReader(fixture[:cut]))
		if err != nil {
			continue
		}
		d.Bin(&ctx[97])
		var dst [8]int32
		_, err = d.Residual(&ctx, ChromaDC, dst[:], transform.DC2x4Scan[:])
		if err == nil && d.Terminate() == 1 && d.Err() == nil {
			t.Fatalf("accepted truncated CABAC at byte %d/%d", cut, len(fixture))
		}
	}
	ctx, _ := InitContexts(true, 0, 26)
	decoded := ctx
	w := bitstream.NewWriter()
	e, _ := NewEncoder(w)
	// 七个sig=0，最后一项隐含非零；幅值逃逸前缀超过实现的23位界限。
	for _, inc := range []int{0, 0, 1, 1, 2, 2, 2} {
		e.Bin(&ctx[149+inc], 0)
	}
	e.Bin(&ctx[258], 1)
	for range 13 {
		e.Bin(&ctx[262], 1)
	}
	for range 24 {
		e.Bypass(1)
	}
	e.Bypass(0)
	e.Terminate(1)
	d, err := New(bitstream.NewReader(w.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	var dst [8]int32
	if _, err := d.Residual(&decoded, ChromaDC, dst[:], transform.DC2x4Scan[:]); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
}
