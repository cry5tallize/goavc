//go:build amd64 && !purego

#include "textflag.h"

// copy16SSE2 只处理宽度16的整像素亮度块；每行读取/写入恰好16字节。
TEXT ·copy16SSE2(SB), NOSPLIT, $0-0
	MOVQ dst_base+0(FP), DI
	MOVQ ds+24(FP), R8
	MOVQ ref_base+32(FP), SI
	MOVQ rs+56(FP), R9
	MOVQ height+64(FP), R10
copy16_sse2_loop:
	TESTQ R10, R10
	JE copy16_sse2_done
	MOVOU (SI), X0
	MOVOU X0, (DI)
	ADDQ R8, DI
	ADDQ R9, SI
	DECQ R10
	JMP copy16_sse2_loop
copy16_sse2_done:
	RET
