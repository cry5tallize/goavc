//go:build amd64 && !purego

#include "textflag.h"

// copy16AVX2 与 SSE2 路径保持相同的精确16字节访问范围。
TEXT ·copy16AVX2(SB), NOSPLIT, $0-0
	MOVQ dst_base+0(FP), DI
	MOVQ ds+24(FP), R8
	MOVQ ref_base+32(FP), SI
	MOVQ rs+56(FP), R9
	MOVQ height+64(FP), R10
copy16_avx2_loop:
	TESTQ R10, R10
	JE copy16_avx2_done
	VMOVDQU (SI), X0
	VMOVDQU X0, (DI)
	ADDQ R8, DI
	ADDQ R9, SI
	DECQ R10
	JMP copy16_avx2_loop
copy16_avx2_done:
	VZEROUPPER
	RET
