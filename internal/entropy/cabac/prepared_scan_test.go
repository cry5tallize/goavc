package cabac

import (
	"fmt"
	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/internal/transform"
	"testing"
)

func TestPreparedScan_GenericStateAndTruncationParity(t *testing.T) {
	for _, c := range []struct {
		cat  int
		scan []int
	}{{LumaAC, transform.Zigzag4x4[1:]}, {Luma4x4, transform.Zigzag4x4[:]}, {Luma8x8, transform.Zigzag8x8[:]}, {ChromaDC, transform.DC2x2Scan[:]}, {ChromaDC, transform.DC2x4Scan[:]}} {
		data, initial := residualPreparedFixture(t, c.cat, c.scan)
		prepared, err := PrepareResidualScan(c.scan)
		if err != nil {
			t.Fatal(err)
		}
		for cut := 0; cut <= len(data); cut++ {
			for _, field := range []bool{false, true} {
				ra, rb := bitstream.NewReader(data[:cut]), bitstream.NewReader(data[:cut])
				a, ea := New(ra)
				b, eb := New(rb)
				if fmt.Sprint(ea) != fmt.Sprint(eb) {
					t.Fatal("New错误不同")
				}
				if ea != nil {
					continue
				}
				ca, cb := initial, initial
				var out, want [64]int32
				for i := range out {
					out[i], want[i] = 77, 77
				}
				na, ea := a.ResidualPrepared(&ca, c.cat, out[:], &prepared, field)
				nb, eb := b.residual(cb[:], c.cat, want[:], c.scan, field)
				if na != nb || fmt.Sprint(ea) != fmt.Sprint(eb) || out != want || ca != cb || a.rangeValue != b.rangeValue || a.offset != b.offset || a.lastBit != b.lastBit || ra.BitsRead() != rb.BitsRead() {
					t.Fatalf("cat%d cut%d field%t状态/错误前缀改变", c.cat, cut, field)
				}
			}
		}
	}
	for _, scan := range [][]int{nil, {0, 0}, {-1}, {64}} {
		if _, err := PrepareResidualScan(scan); err == nil {
			t.Fatal("接受非法prepared scan")
		}
	}
}

func TestPreparedScan444_AllCategoriesAndField(t *testing.T) {
	for cat := LumaDC; cat <= Cr8x8; cat++ {
		for _, field := range []bool{false, true} {
			scan := transform.Zigzag4x4[:]
			if field {
				scan = transform.FieldScan4x4[:]
			}
			if cat == LumaAC || cat == ChromaAC || cat == CbAC || cat == CrAC {
				scan = scan[1:]
			}
			if cat == Luma8x8 || cat == Cb8x8 || cat == Cr8x8 {
				scan = transform.Zigzag8x8[:]
				if field {
					scan = transform.FieldScan8x8[:]
				}
			}
			if cat == ChromaDC {
				scan = transform.DC2x2Scan[:]
			}
			var src [64]int32
			src[scan[0]], src[scan[len(scan)-1]] = 3, -17
			var ctx [ContextCount444]Context
			encoded := ctx
			w := bitstream.NewWriter()
			e, err := NewEncoder(w)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = e.Residual444(&encoded, cat, src[:], scan, field); err != nil {
				t.Fatal(err)
			}
			e.Terminate(1)
			prepared, err := PrepareResidualScan(scan)
			if err != nil {
				t.Fatal(err)
			}
			data := w.Bytes()
			for cut := 0; cut <= len(data); cut++ {
				ra, rb := bitstream.NewReader(data[:cut]), bitstream.NewReader(data[:cut])
				a, ea := New(ra)
				b, eb := New(rb)
				if fmt.Sprint(ea) != fmt.Sprint(eb) {
					t.Fatal("初始化错误不符")
				}
				if ea != nil {
					continue
				}
				ca, cb := ctx, ctx
				var out, want [64]int32
				na, ea := a.ResidualPrepared444(&ca, cat, out[:], &prepared, field)
				nb, eb := b.Residual444(&cb, cat, want[:], scan, field)
				if na != nb || fmt.Sprint(ea) != fmt.Sprint(eb) || out != want || ca != cb || a.offset != b.offset || a.rangeValue != b.rangeValue || a.lastBit != b.lastBit || ra.BitsRead() != rb.BitsRead() {
					t.Fatalf("cat%d field%t cut%d不符", cat, field, cut)
				}
			}
		}
	}
}

func residualPreparedFixture(t testing.TB, cat int, scan []int) ([]byte, [ContextCount]Context) {
	t.Helper()
	var src [64]int32
	src[scan[0]], src[scan[len(scan)/2]], src[scan[len(scan)-1]] = 1, -2, 17
	initial, _ := InitContexts(true, 0, 26)
	contexts := initial
	w := bitstream.NewWriter()
	e, err := NewEncoder(w)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Residual(&contexts, cat, src[:], scan); err != nil {
		t.Fatal(err)
	}
	e.Terminate(1)
	if e.Err() != nil {
		t.Fatal(e.Err())
	}
	return w.Bytes(), initial
}

func BenchmarkCABACResidual_Prepared_Auto(b *testing.B) {
	for _, c := range []struct {
		cat  int
		scan []int
	}{{LumaAC, transform.Zigzag4x4[1:]}, {Luma4x4, transform.Zigzag4x4[:]}, {Luma8x8, transform.Zigzag8x8[:]}} {
		b.Run(fmt.Sprintf("size%d", len(c.scan)), func(b *testing.B) {
			data, initial := residualPreparedFixture(b, c.cat, c.scan)
			prepared, err := PrepareResidualScan(c.scan)
			if err != nil {
				b.Fatal(err)
			}
			reader := bitstream.NewReader(data)
			engine, err := New(reader)
			if err != nil {
				b.Fatal(err)
			}
			initialReader, initialEngine := *reader, *engine
			b.ReportAllocs()
			for b.Loop() {
				r := initialReader
				d := initialEngine
				d.r = &r
				ctx := initial
				var out [64]int32
				if n, err := d.ResidualPrepared(&ctx, c.cat, out[:], &prepared, false); err != nil || n != 3 {
					b.Fatal(n, err)
				}
			}
		})
	}
}

func BenchmarkCABACScan_Compare(b *testing.B) {
	for _, c := range []struct {
		cat  int
		scan []int
	}{{ChromaDC, transform.DC2x2Scan[:]}, {ChromaDC, transform.DC2x4Scan[:]}, {LumaAC, transform.Zigzag4x4[1:]}, {Luma4x4, transform.Zigzag4x4[:]}, {Luma8x8, transform.Zigzag8x8[:]}} {
		for _, preparedMode := range []bool{false, true} {
			b.Run(fmt.Sprintf("size%d/prepared%t", len(c.scan), preparedMode), func(b *testing.B) {
				data, initial := residualPreparedFixture(b, c.cat, c.scan)
				prepared, err := PrepareResidualScan(c.scan)
				if err != nil {
					b.Fatal(err)
				}
				r := bitstream.NewReader(data)
				d, err := New(r)
				if err != nil {
					b.Fatal(err)
				}
				ir, id := *r, *d
				b.ReportAllocs()
				rr, dd := ir, id
				ctx := initial
				var out [64]int32
				for b.Loop() {
					rr = ir
					dd = id
					dd.r = &rr
					ctx = initial
					var n int
					var err error
					if preparedMode {
						n, err = dd.ResidualPrepared(&ctx, c.cat, out[:], &prepared, false)
					} else {
						n, err = dd.Residual(&ctx, c.cat, out[:], c.scan)
					}
					if err != nil || n != 3 {
						b.Fatal(n, err)
					}
				}
			})
		}
	}
}
