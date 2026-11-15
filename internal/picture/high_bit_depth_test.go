package picture

import (
	"errors"
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

func TestNew_High10Storage(t *testing.T) {
	pic, err := New(3, 2, h264.YUV420P10LE)
	if err != nil {
		t.Fatal(err)
	}
	defer pic.Release()
	if err := pic.Validate(); err != nil {
		t.Fatal(err)
	}
	for i, dims := range [][2]int{{48, 32}, {24, 16}, {24, 16}} {
		pl := &pic.Planes[i]
		if pl.Width != dims[0] || pl.Height != dims[1] || pl.BitDepth != 10 {
			t.Fatalf("plane %d: %dx%d depth%d", i, pl.Width, pl.Height, pl.BitDepth)
		}
		if len(pl.Data) != 0 || len(pl.Data16) != pl.Stride*(pl.Height+2*pl.Border) || pl.Stride*2%64 != 0 {
			t.Fatalf("plane %d: bytes=%d samples=%d stride=%d", i, len(pl.Data), len(pl.Data16), pl.Stride)
		}
	}
}

func TestPlane_High10OddStrideBorder(t *testing.T) {
	// 5×3、奇数stride11；每行最后两项是对齐填充，不属于边界区。
	pl := Plane{Data16: make([]uint16, 77), Stride: 11, Width: 5, Height: 3, Border: 2, BitDepth: 10}
	for i := range pl.Data16 {
		pl.Data16[i] = 65535
	}
	want := [3][5]uint16{{0, 1, 256, 1022, 1023}, {1023, 1022, 512, 1, 0}, {12, 24, 256, 768, 1000}}
	for y := range 3 {
		for x := range 5 {
			pl.SetSample(x, y, want[y][x])
		}
	}
	// Row16必须与存储共享，而不是截断到byte或返回临时副本。
	pl.Row16(1)[2] = 777
	want[1][2] = 777
	pl.FillBorder()
	for y := -2; y < 5; y++ {
		for x := -2; x < 7; x++ {
			expect := want[max(0, min(2, y))][max(0, min(4, x))]
			if got := pl.Sample(x, y); got != expect {
				t.Fatalf("Sample(%d,%d)=%d want %d", x, y, got, expect)
			}
		}
		for _, x := range []int{9, 10} {
			if got := pl.Data16[(y+2)*11+x]; got != 65535 {
				t.Fatalf("padding overwritten at row %d: %d", y, got)
			}
		}
	}
}

func TestAddResidual4x4_High10ClipAndStride(t *testing.T) {
	pic, err := New(1, 1, h264.YUV420P10LE)
	if err != nil {
		t.Fatal(err)
	}
	defer pic.Release()
	pl := &pic.Planes[0]
	residual := [16]int32{-1 << 31, -513, -512, -511, -1, 0, 1, 255, 256, 510, 511, 512, 513, 1023, 1 << 30, 1<<31 - 1}
	want := [16]uint16{0, 0, 0, 1, 511, 512, 513, 767, 768, 1022, 1023, 1023, 1023, 1023, 1023, 1023}
	for i := range 16 {
		pl.SetSample(3+i%4, 5+i/4, 512)
	}
	pl.SetSample(2, 5, 901)
	AddResidual4x4(pl, 3, 5, &residual)
	for i, expect := range want {
		if got := pl.Sample(3+i%4, 5+i/4); got != expect {
			t.Fatalf("sample %d=%d want %d", i, got, expect)
		}
	}
	if got := pl.Sample(2, 5); got != 901 {
		t.Fatalf("neighbor overwritten: %d", got)
	}
}

func TestPool_High10AndEightBitRemainIsolated(t *testing.T) {
	pool := NewPool(1)
	for round := range 2 {
		for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE} {
			pic, err := pool.Get(1, 1, format)
			if err != nil {
				t.Fatal(err)
			}
			want := uint16(201)
			if format == h264.YUV420P10LE {
				want = 1001
			}
			if pic.Format != format || pic.Planes[0].BitDepth != format.BitDepth() {
				t.Fatalf("pool returned %s for %s", pic.Format, format)
			}
			if round == 0 {
				pic.Planes[0].SetSample(0, 0, want)
			}
			if got := pic.Planes[0].Sample(0, 0); got != want {
				t.Fatalf("%s round%d=%d want %d", format, round, got, want)
			}
			if err := pic.Validate(); err != nil {
				t.Fatal(err)
			}
			pic.Release()
			pic.Release()
		}
	}
}

func TestPictureValidate_High10RejectsInvalidStorage(t *testing.T) {
	for _, mutate := range []func(*Plane){
		func(pl *Plane) { pl.Data16 = pl.Data16[:len(pl.Data16)-1] },
		func(pl *Plane) { pl.Data = []byte{0} },
		func(pl *Plane) { pl.BitDepth = 8 },
		func(pl *Plane) { pl.Stride = int(^uint(0) >> 1) },
		func(pl *Plane) { pl.Border = int(^uint(0) >> 1) },
	} {
		pic, err := New(1, 1, h264.YUV420P10LE)
		if err != nil {
			t.Fatal(err)
		}
		mutate(&pic.Planes[0])
		if err := pic.Validate(); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("Validate=%v want ErrInvalidData", err)
		}
		pic.Release()
	}
}
