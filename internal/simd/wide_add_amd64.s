//go:build amd64 && !purego

#include "textflag.h"

// 两个后端均只读a、b各32字节，写out的32字节；不要求对齐。
TEXT ·wideAdd4SSE2(SB), NOSPLIT, $0-96
	LEAQ a+0(FP), SI
	LEAQ b+32(FP), DI
	LEAQ out+64(FP), BX
	MOVOU (SI), X0
	MOVOU 16(SI), X1
	MOVOU (DI), X2
	MOVOU 16(DI), X3
	PADDQ X2, X0
	PADDQ X3, X1
	MOVOU X0, (BX)
	MOVOU X1, 16(BX)
	RET

TEXT ·wideAdd4AVX2(SB), NOSPLIT, $0-96
	VMOVDQU a+0(FP), Y0
	VMOVDQU b+32(FP), Y1
	VPADDQ Y1, Y0, Y0
	VMOVDQU Y0, out+64(FP)
	VZEROUPPER
	RET
