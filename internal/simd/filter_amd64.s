//go:build amd64 && !purego

#include "textflag.h"

// 仅访问固定值参数 a、b 各16字节及返回值16字节。
TEXT ·filterDeltaSSE2(SB), NOSPLIT, $0-48
	LEAQ a+0(FP), SI
	LEAQ b+16(FP), DI
	LEAQ out+32(FP), BX
	MOVOU (SI), X0
	MOVOU (DI), X1
	PADDL X1, X0
	MOVOU X0, (BX)
	RET

TEXT ·filterDeltaAVX2(SB), NOSPLIT, $0-48
	LEAQ a+0(FP), SI
	VMOVDQU (SI), Y0
	VEXTRACTI128 $1, Y0, X1
	VPADDD X1, X0, X0
	LEAQ out+32(FP), DI
	VMOVDQU X0, (DI)
	VZEROUPPER
	RET
