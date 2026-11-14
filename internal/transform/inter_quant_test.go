package transform

import (
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
)

func TestInterQuantization_AnalyticDeadZone(t *testing.T) {
	// QP0、flat：4×4 DC步长约2.5，Hadamard步长约5，8×8 DC步长5。
	// 0.8个步长在帧内1/3舍入时为1，在帧间1/6舍入时为0；
	// 超过一个步长的系数则仍为1。负系数必须与正系数对称。
	for _, sign := range []int32{-1, 1} {
		var src4, intra4, inter4 [16]int32
		src4[0], src4[2] = 2*sign, 3*sign
		if err := Quant4x4WithScaling(&intra4, &src4, 0, nil); err != nil {
			t.Fatal(err)
		}
		if err := QuantInter4x4WithScaling(&inter4, &src4, 0, nil); err != nil {
			t.Fatal(err)
		}
		if intra4[0] != sign || inter4[0] != 0 || inter4[2] != sign {
			t.Fatalf("4×4死区不符解析边界：intra=%v inter=%v", intra4, inter4)
		}
		srcDC := []int32{4 * sign, 6 * sign}
		intraDC, interDC := make([]int32, 2), make([]int32, 2)
		if err := QuantDCWithScaling(intraDC, srcDC, 0, 16); err != nil {
			t.Fatal(err)
		}
		if err := QuantInterDCWithScaling(interDC, srcDC, 0, 16); err != nil {
			t.Fatal(err)
		}
		if intraDC[0] != sign || interDC[0] != 0 || interDC[1] != sign {
			t.Fatalf("DC死区不符解析边界：intra=%v inter=%v", intraDC, interDC)
		}
		var src8, intra8, inter8 [64]int32
		src8[0] = 4 * sign
		if err := Quant8x8WithScaling(&intra8, &src8, 0, nil); err != nil {
			t.Fatal(err)
		}
		if err := QuantInter8x8WithScaling(&inter8, &src8, 0, nil); err != nil {
			t.Fatal(err)
		}
		if intra8[0] != sign || inter8[0] != 0 {
			t.Fatalf("8×8死区不符解析边界：intra=%v inter=%v", intra8, inter8)
		}
		src8[0] = 6 * sign
		if err := QuantInter8x8WithScaling(&inter8, &src8, 0, nil); err != nil || inter8[0] != sign {
			t.Fatalf("8×8非零系数被错误舍弃：%v %v", inter8[0], err)
		}
	}
}

func TestInterQuantization_ScalingAndInvalidQP(t *testing.T) {
	var src, dst [16]int32
	var matrix [16]uint8
	for i := range matrix {
		matrix[i] = 32
	}
	// 矩阵加倍后，原来两倍的系数落到相同死区内。
	src[0] = 4
	if err := QuantInter4x4WithScaling(&dst, &src, 0, &matrix); err != nil || dst[0] != 0 {
		t.Fatalf("自定义矩阵死区不符：%v %v", dst, err)
	}
	for _, qp := range []int{-1, 64} {
		if err := QuantInter4x4WithScaling(&dst, &src, qp, nil); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal("4×4未拒绝非法QP", err)
		}
		if err := QuantInterDCWithScaling(dst[:], src[:], qp, 16); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal("DC未拒绝非法QP", err)
		}
		var src8, dst8 [64]int32
		if err := QuantInter8x8WithScaling(&dst8, &src8, qp, nil); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal("8×8未拒绝非法QP", err)
		}
	}
}
