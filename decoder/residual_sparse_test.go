package decoder

import (
	"errors"
	"slices"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/internal/picture"
	"github.com/cry5tallize/goavc/internal/transform"
)

func TestResidual4x4_SparseParity(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE} {
		for _, qp := range []int{0, 26, 51} {
			for _, dc := range []int32{-2147483648, -8192, -33, -32, -1, 0, 1, 31, 32, 8192, 2147483647} {
				for _, ac := range []bool{false, true} {
					for _, ready := range []bool{false, true} {
						a, b := deblockTestState(t, 1, 1, format, true), deblockTestState(t, 1, 1, format, true)
						var levels, coeff, residual [16]int32
						levels[0] = dc
						if ac {
							levels[5] = -17
						}
						var readyPtr *[16]int32
						var wantErr error
						if ready {
							coeff = levels
							readyPtr = &levels
						} else {
							q, _ := transform.ScaleQP(qp, format.BitDepth())
							wantErr = transform.Dequant4x4(&coeff, &levels, q)
						}
						gotErr := addResidual4x4(&a.pic.Planes[0], 3, 5, &levels, qp, readyPtr, nil)
						if (gotErr == nil) != (wantErr == nil) || wantErr != nil && !errors.Is(gotErr, h264.ErrInvalidData) {
							t.Fatalf("反量化错误语义变化：%v/%v", gotErr, wantErr)
						}
						if wantErr != nil {
							continue
						}
						transform.IDCT4x4(&residual, &coeff)
						picture.AddResidual4x4(&b.pic.Planes[0], 3, 5, &residual)
						if !slices.Equal(a.pic.Planes[0].Data, b.pic.Planes[0].Data) || !slices.Equal(a.pic.Planes[0].Data16, b.pic.Planes[0].Data16) {
							t.Fatalf("稀疏重建变化：format=%s qp=%d dc=%d ac=%v ready=%v", format, qp, dc, ac, ready)
						}
					}
				}
			}
		}
	}
}

func TestResidual4x4_ZeroStillValidates(t *testing.T) {
	fs := deblockTestState(t, 1, 1, h264.YUV420P, true)
	var levels [16]int32
	var matrix [16]uint8
	for _, c := range []struct {
		qp     int
		matrix *[16]uint8
	}{{-1, nil}, {52, nil}, {26, &matrix}} {
		if err := addResidual4x4(&fs.pic.Planes[0], 0, 0, &levels, c.qp, nil, c.matrix); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("全零块绕过校验：%v", err)
		}
	}
}
