//go:build amd64 && !purego

#include "textflag.h"

// CPUID 的四个结果为 uint32，返回区紧接两个 uint32 参数。
TEXT ·readCPUID(SB), NOSPLIT, $0-24
	MOVL leaf+0(FP), AX
	MOVL subleaf+4(FP), CX
	CPUID
	MOVL AX, a+8(FP)
	MOVL BX, b+12(FP)
	MOVL CX, c+16(FP)
	MOVL DX, d+20(FP)
	RET

// Go 包装确认 OSXSAVE 后才进入；这里只读取 XCR0。
TEXT ·readXCR0(SB), NOSPLIT, $0-8
	XORL CX, CX
	XGETBV
	SHLQ $32, DX
	ORQ DX, AX
	MOVQ AX, ret+0(FP)
	RET
