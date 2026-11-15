package simd

import "testing"

func TestGuardPage_Transforms(t *testing.T) {
	if !guardChild(t) {
		return
	}
	saved := kernels
	defer func() { kernels = saved }()
	for _, backend := range guardBackends() {
		kernels = backend.table
		t.Run(backend.name, func(t *testing.T) {
			source, _ := guardedRows(t, 64, 1, 4)
			dest, _ := guardedRows(t, 64, 1, 4)
			src8, dst8 := (*[64]int32)(guardInt32(source)), (*[64]int32)(guardInt32(dest))
			for i := range src8 {
				src8[i] = 17
			}
			Forward8x8(dst8, src8)
			if *dst8 != ([64]int32{1088}) {
				t.Fatalf("8×8常量正变换=%v", *dst8)
			}
			*src8 = [64]int32{1088}
			if !IDCT8x8(dst8, src8) {
				t.Fatal("8×8常量逆变换意外溢出")
			}
			for i, v := range dst8 {
				if v != 17 {
					t.Fatalf("8×8逆变换lane%d=%d", i, v)
				}
			}
			source4, _ := guardedRows(t, 16, 1, 4)
			dest4, _ := guardedRows(t, 16, 1, 4)
			src4, dst4 := (*[16]int32)(guardInt32(source4)), (*[16]int32)(guardInt32(dest4))
			for i := range src4 {
				src4[i] = 17
			}
			*dst4 = Forward4x4(*src4)
			if *dst4 != ([16]int32{272}) {
				t.Fatalf("4×4常量正变换=%v", *dst4)
			}
			*dst4 = ForwardLumaDC(*src4)
			if *dst4 != ([16]int32{136}) {
				t.Fatalf("亮度DC常量正变换=%v", *dst4)
			}
			*src4 = [16]int32{1088}
			IDCT4x4(dst4, src4)
			for i, v := range dst4 {
				if v != 17 {
					t.Fatalf("4×4逆变换lane%d=%d", i, v)
				}
			}
			for i := range src4 {
				src4[i] = 1023
			}
			if got := SATD4x4(*src4); got != 8184 {
				t.Fatalf("SATD常量=%d，期望8184", got)
			}
			if *src4 != ([16]int32{1023, 1023, 1023, 1023, 1023, 1023, 1023, 1023, 1023, 1023, 1023, 1023, 1023, 1023, 1023, 1023}) {
				t.Fatal("只读源数组被修改")
			}
		})
	}
}
