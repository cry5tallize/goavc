//go:build amd64 && !purego

#include "textflag.h"

// 每个YMM寄存器并行计算两个独立的4采样行蝶形。
// 只读取输入64字节，写出128字节的和差；均不要求对齐。
TEXT ·forward4RowsAVX2(SB), NOSPLIT, $0-192
	LEAQ src+0(FP), SI
	VMOVDQU (SI), Y0
	VMOVDQU 32(SI), Y1
	VPSHUFD $0x1b, Y0, Y2
	VPADDD Y2, Y0, Y3
	VPSUBD Y2, Y0, Y0
	VPSHUFD $0x1b, Y1, Y2
	VPADDD Y2, Y1, Y5
	VPSUBD Y2, Y1, Y1
	LEAQ pairs+64(FP), DI
	VMOVDQU Y3, (DI)
	VMOVDQU Y0, 32(DI)
	VMOVDQU Y5, 64(DI)
	VMOVDQU Y1, 96(DI)
	VZEROUPPER
	RET
