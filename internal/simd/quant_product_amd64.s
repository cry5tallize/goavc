//go:build amd64 && !purego

#include "textflag.h"

// PMULULQ取每个64位lane的低32位，结果为完整64位积；无饱和或舍入。
TEXT ·quantMultiplySSE2(SB), NOSPLIT, $0-96
	LEAQ a+0(FP), SI
	LEAQ b+32(FP), DI
	LEAQ out+64(FP), BX
	MOVOU (SI), X0
	MOVOU 16(SI), X1
	MOVOU (DI), X2
	MOVOU 16(DI), X3
 PMULULQ X2, X0
 PMULULQ X3, X1
	MOVOU X0, (BX)
	MOVOU X1, 16(BX)
 RET

TEXT ·quantMultiplyAVX2(SB), NOSPLIT, $0-96
 VMOVDQU a+0(FP), Y0
 VMOVDQU b+32(FP), Y1
 VPMULUDQ Y1, Y0, Y0
 VMOVDQU Y0, out+64(FP)
 VZEROUPPER
 RET
