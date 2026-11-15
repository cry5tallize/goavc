package transform

import (
	"errors"
	h264 "github.com/cry5tallize/goavc"
	"testing"
)

func TestForwardScaling_FlatParityAndAnalyticScale(t *testing.T) {
	var flat, half [16]uint8
	var src, a, b [16]int32
	for i := range flat {
		flat[i] = 16
		half[i] = 32
		src[i] = int32(i*251 - 1700)
	}
	for qp := 0; qp <= 51; qp++ {
		if err := Quant4x4(&a, &src, qp); err != nil {
			t.Fatal(err)
		}
		if err := Quant4x4WithScaling(&b, &src, qp, &flat); err != nil {
			t.Fatal(err)
		}
		if a != b {
			t.Fatal("flat量化行为改变", qp)
		}
		if err := QuantDC(a[:], src[:], qp); err != nil {
			t.Fatal(err)
		}
		if err := QuantDCWithScaling(b[:], src[:], qp, 16); err != nil {
			t.Fatal(err)
		}
		if a != b {
			t.Fatal("flat DC量化行为改变", qp)
		}
	}
	src = [16]int32{640}
	Quant4x4WithScaling(&a, &src, 0, &half)
	if a[0] != 128 {
		t.Fatalf("scale32 DC量化%d，期望128", a[0])
	}
	flat[15] = 0
	b = [16]int32{17}
	before := b
	if err := Quant4x4WithScaling(&b, &src, 26, &flat); !errors.Is(err, h264.ErrInvalidData) || b != before {
		t.Fatal("非法矩阵未事务拒绝", err)
	}
}
