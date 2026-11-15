package cabac

import (
	"bytes"
	"errors"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
)

func TestResidual444_IndependentLevelSaturationTrace(t *testing.T) {
	// 扫描次序的8个level为{2,-1,3,1,-2,1,2,-3}。
	// 逆序幅值的上下文增量固定列出，末项使用+9而不是422专用的+8。
	trace := [][2]int{
		{1, 1}, {5, 1}, {5, 0}, {-1, 1},
		{0, 1}, {6, 0}, {-1, 0},
		{0, 0}, {-1, 0},
		{0, 1}, {7, 0}, {-1, 1},
		{0, 0}, {-1, 0},
		{0, 1}, {8, 1}, {8, 0}, {-1, 0},
		{0, 0}, {-1, 1},
		{0, 1}, {9, 0}, {-1, 0},
	}
	for mode := range 2 {
		for cat := 6; cat < 14; cat++ {
			ctx, _ := InitContexts444(true, 0, 19)
			initial := ctx
			bases := normative444Bases[mode][cat]
			w := bitstream.NewWriter()
			e, _ := NewEncoder(w)
			for pos := range 8 {
				sig, last := pos, pos
				if cat == 9 || cat == 13 {
					sig, last = normative444Sig[mode][pos], normative444Last[pos]
				}
				e.Bin(&ctx[bases[0]+sig], 1)
				e.Bin(&ctx[bases[1]+last], uint8(pos/7))
			}
			for _, event := range trace {
				if event[0] == -1 {
					e.Bypass(uint8(event[1]))
				} else {
					e.Bin(&ctx[bases[2]+event[0]], uint8(event[1]))
				}
			}
			e.Terminate(1)
			data := w.Bytes()
			wantCtx := ctx
			scan := normative444Scan(cat, mode)
			var src, dst [64]int32
			for pos, value := range []int32{2, -1, 3, 1, -2, 1, 2, -3} {
				src[scan[pos]] = value
			}
			ctx = initial
			d, err := New(bitstream.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			n, err := d.Residual444(&ctx, cat, dst[:], scan, mode != 0)
			if err != nil || n != 8 || dst != src || ctx != wantCtx || d.Terminate() != 1 || d.Err() != nil {
				t.Fatal(cat, mode, n, dst, src, err, d.Err())
			}
			ctx = initial
			w = bitstream.NewWriter()
			e, _ = NewEncoder(w)
			n, err = e.Residual444(&ctx, cat, src[:], scan, mode != 0)
			e.Terminate(1)
			if err != nil || n != 8 || ctx != wantCtx || !bytes.Equal(data, w.Bytes()) || e.Err() != nil {
				t.Fatal(cat, mode, n, err, e.Err())
			}
		}
	}
}

func TestResidual444_IndependentEscapeBinsAndTruncation(t *testing.T) {
	for mode := range 2 {
		for cat := range 14 {
			for _, vector := range []struct {
				level int32
				bins  []uint8
			}{
				{15, []uint8{0}},
				{-31, []uint8{1, 1, 1, 1, 0, 0, 0, 0, 1}},
			} {
				ctx, _ := InitContexts444(false, 1, 0)
				initial := ctx
				bases := normative444Bases[mode][cat]
				w := bitstream.NewWriter()
				e, _ := NewEncoder(w)
				e.Bin(&ctx[bases[0]], 1)
				e.Bin(&ctx[bases[1]], 1)
				e.Bin(&ctx[bases[2]+1], 1)
				for range 13 {
					e.Bin(&ctx[bases[2]+5], 1)
				}
				for _, bin := range vector.bins {
					e.Bypass(bin)
				}
				if vector.level < 0 {
					e.Bypass(1)
				} else {
					e.Bypass(0)
				}
				e.Terminate(1)
				data, wantCtx := w.Bytes(), ctx
				scan := normative444Scan(cat, mode)
				var src [64]int32
				src[scan[0]] = vector.level
				ctx = initial
				encoded := bitstream.NewWriter()
				e, _ = NewEncoder(encoded)
				if _, err := e.Residual444(&ctx, cat, src[:], scan, mode != 0); err != nil {
					t.Fatal(err)
				}
				e.Terminate(1)
				if ctx != wantCtx || !bytes.Equal(encoded.Bytes(), data) || e.Err() != nil {
					t.Fatal(cat, mode, vector.level, e.Err())
				}
				for cut := range len(data) + 1 {
					ctx = initial
					d, err := New(bitstream.NewReader(data[:cut]))
					if err != nil {
						if cut == len(data) {
							t.Fatal(err)
						}
						continue
					}
					var dst [64]int32
					n, err := d.Residual444(&ctx, cat, dst[:], scan, mode != 0)
					term := d.Terminate()
					if cut == len(data) {
						if err != nil || n != 1 || dst != src || ctx != wantCtx || term != 1 || d.Err() != nil {
							t.Fatal(cat, mode, vector.level, n, dst, err, d.Err())
						}
					} else if err == nil && term == 1 && d.Err() == nil {
						t.Fatal("接受截断向量", cat, mode, cut, len(data))
					}
				}
			}
		}
	}
}

func TestResidual444_RejectsInvalidDomainsAndCapacity(t *testing.T) {
	var ctx [ContextCount444]Context
	var old [ContextCount]Context
	var src [64]int32
	src[0] = 1
	d, _ := New(bitstream.NewReader([]byte{0, 0, 0}))
	e, _ := NewEncoder(bitstream.NewWriter())
	valid := normative444Scan(Cb4x4, 0)
	for _, field := range []bool{false, true} {
		if _, err := d.Residual444(nil, Cb4x4, src[:], valid, field); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal(err)
		}
		if _, err := e.Residual444(nil, Cb4x4, src[:], valid, field); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal(err)
		}
		for _, cat := range []int{-1, 14, 1 << 30} {
			if _, err := d.Residual444(&ctx, cat, src[:], valid, field); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatal(cat, err)
			}
			if _, err := e.Residual444(&ctx, cat, src[:], valid, field); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatal(cat, err)
			}
		}
		for _, scan := range [][]int{nil, {0}, {0, 1, 2, 3}, {0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, -1}, {0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 64}, {0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 0}} {
			if _, err := d.Residual444(&ctx, Cb4x4, src[:], scan, field); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatal(scan, err)
			}
			if _, err := e.Residual444(&ctx, Cb4x4, src[:], scan, field); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatal(scan, err)
			}
		}
		for capacity := range 16 {
			if _, err := d.Residual444(&ctx, Cb4x4, src[:capacity], valid, field); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatal(capacity, err)
			}
			if _, err := e.Residual444(&ctx, Cb4x4, src[:capacity], valid, field); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatal(capacity, err)
			}
		}
	}
	for cat := -1; cat <= 14; cat++ {
		if cat >= 0 && cat <= 5 {
			continue
		}
		for _, decode := range []func() (int, error){
			func() (int, error) { return d.Residual(&old, cat, src[:], valid) },
			func() (int, error) { return d.ResidualField(&old, cat, src[:], valid) },
			func() (int, error) { return e.Residual(&old, cat, src[:], valid) },
			func() (int, error) { return e.ResidualField(&old, cat, src[:], valid) },
		} {
			if _, err := decode(); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatal(cat, err)
			}
		}
	}
	for _, call := range []func() (int, error){
		func() (int, error) { return d.Residual(nil, 2, src[:], valid) },
		func() (int, error) { return d.ResidualField(nil, 2, src[:], valid) },
		func() (int, error) { return e.Residual(nil, 2, src[:], valid) },
		func() (int, error) { return e.ResidualField(nil, 2, src[:], valid) },
	} {
		if _, err := call(); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal(err)
		}
	}
	for _, value := range []int32{0, 1<<24 + 1, -1<<24 - 1, 1<<31 - 1, -1 << 31} {
		src = [64]int32{value}
		if _, err := e.Residual444(&ctx, Cb4x4, src[:], valid, false); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal(value, err)
		}
	}
}

func TestResidualField_PreservesLegacyModelDomain(t *testing.T) {
	for cat := range 6 {
		scan := normative444Scan(cat, 1)
		for position := range scan {
			data, want := normative444Fixture(t, cat, 1, position)
			ctx, _ := InitContexts(false, 2, 37)
			d, err := New(bitstream.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			var src, dst [64]int32
			src[scan[position]] = -2
			n, err := d.ResidualField(&ctx, cat, dst[:], scan)
			if err != nil || n != 1 || dst != src || d.Terminate() != 1 || d.Err() != nil {
				t.Fatal(cat, position, n, err, d.Err())
			}
			for i, c := range ctx {
				if c != want[i] {
					t.Fatal(cat, position, i, c, want[i])
				}
			}
			ctx, _ = InitContexts(false, 2, 37)
			w := bitstream.NewWriter()
			e, _ := NewEncoder(w)
			n, err = e.ResidualField(&ctx, cat, src[:], scan)
			e.Terminate(1)
			if err != nil || n != 1 || !bytes.Equal(w.Bytes(), data) || e.Err() != nil {
				t.Fatal(cat, position, n, err, e.Err())
			}
		}
	}
}

func TestResidual444_RejectsOverlongEscapePrefix(t *testing.T) {
	for mode := range 2 {
		for cat := range 14 {
			ctx, _ := InitContexts444(true, 0, 51)
			initial := ctx
			bases := normative444Bases[mode][cat]
			w := bitstream.NewWriter()
			e, _ := NewEncoder(w)
			e.Bin(&ctx[bases[0]], 1)
			e.Bin(&ctx[bases[1]], 1)
			e.Bin(&ctx[bases[2]+1], 1)
			for range 13 {
				e.Bin(&ctx[bases[2]+5], 1)
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
			var dst [64]int32
			if _, err := d.Residual444(&initial, cat, dst[:], normative444Scan(cat, mode), mode != 0); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatal(cat, mode, err)
			}
		}
	}
}

func TestResidualField_PreservesEightCoefficientChromaDC(t *testing.T) {
	scan := []int{0, 2, 1, 4, 6, 3, 5, 7}
	ctx, _ := InitContexts(true, 0, 26)
	initial := ctx
	w := bitstream.NewWriter()
	e, _ := NewEncoder(w)
	// 场编码422DC的sig/last基址为321/382，增量仍为min(pos/2,2)。
	for _, inc := range []int{0, 0, 1, 1, 2, 2, 2} {
		e.Bin(&ctx[321+inc], 1)
		e.Bin(&ctx[382+inc], 0)
	}
	// 八项幅值均为2，第四个之后后缀饱和于265，不跨入ChromaAC。
	for i, inc := range []int{5, 6, 7, 8, 8, 8, 8, 8} {
		first := 257
		if i == 0 {
			first = 258
		}
		e.Bin(&ctx[first], 1)
		e.Bin(&ctx[257+inc], 0)
		e.Bypass(0)
	}
	e.Terminate(1)
	data, wantCtx := w.Bytes(), ctx
	d, err := New(bitstream.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	ctx = initial
	var dst [8]int32
	src := [8]int32{2, 2, 2, 2, 2, 2, 2, 2}
	n, err := d.ResidualField(&ctx, ChromaDC, dst[:], scan)
	if err != nil || n != 8 || dst != src || ctx != wantCtx || d.Terminate() != 1 || d.Err() != nil {
		t.Fatal(n, dst, err, d.Err())
	}
	ctx = initial
	w = bitstream.NewWriter()
	e, _ = NewEncoder(w)
	n, err = e.ResidualField(&ctx, ChromaDC, src[:], scan)
	e.Terminate(1)
	if err != nil || n != 8 || ctx != wantCtx || !bytes.Equal(w.Bytes(), data) || e.Err() != nil {
		t.Fatal(n, err, e.Err())
	}
}
