//go:build amd64 && !purego

#include "textflag.h"

// 所有行精确访问width；尾部8/4/1字节分解，不读padding。
TEXT ·averageBlock8SSE2(SB), NOSPLIT, $0-104
	MOVQ dst_base+0(FP), DI
	MOVQ ds+24(FP), R8
	MOVQ a_base+32(FP), SI
	MOVQ b_base+56(FP), BX
	MOVQ ss+80(FP), R9
	MOVQ w+88(FP), R10
	MOVQ h+96(FP), R11
avg8_row:
	XORQ CX,CX
	MOVQ R10,DX
avg8_vec:
	CMPQ DX,$16
	JLT avg8_tail8
	MOVOU (SI)(CX*1),X0
	MOVOU (BX)(CX*1),X1
	PAVGB X1,X0
	MOVOU X0,(DI)(CX*1)
	ADDQ $16,CX
	SUBQ $16,DX
	JMP avg8_vec
avg8_tail8:
	CMPQ DX,$8
	JLT avg8_tail4
	MOVQ (SI)(CX*1),X0
	MOVQ (BX)(CX*1),X1
	PAVGB X1,X0
	MOVQ X0,(DI)(CX*1)
	ADDQ $8,CX
	SUBQ $8,DX
avg8_tail4:
	CMPQ DX,$4
	JLT avg8_scalar
	MOVL (SI)(CX*1),X0
	MOVL (BX)(CX*1),X1
	PAVGB X1,X0
	MOVL X0,(DI)(CX*1)
	ADDQ $4,CX
	SUBQ $4,DX
avg8_scalar:
	TESTQ DX,DX
	JEQ avg8_next
	MOVBQZX (SI)(CX*1),AX
	MOVBQZX (BX)(CX*1),R12
	LEAQ 1(AX)(R12*1),AX
	SHRQ $1,AX
	MOVB AX,(DI)(CX*1)
	INCQ CX
	DECQ DX
	JMP avg8_scalar
avg8_next:
	ADDQ R8,DI
	ADDQ R9,SI
	ADDQ R9,BX
	DECQ R11
	JNE avg8_row
	RET

// PAVGW对完整无符号16-bit域实现(a+b+1)>>1，无中间截断。
TEXT ·averageBlock16SSE2(SB), NOSPLIT, $0-104
	MOVQ dst_base+0(FP), DI
	MOVQ ds+24(FP), R8
	MOVQ a_base+32(FP), SI
	MOVQ b_base+56(FP), BX
	MOVQ ss+80(FP), R9
	SHLQ $1,R8
	SHLQ $1,R9
	MOVQ w+88(FP), R10
	MOVQ h+96(FP), R11
avg16_row:
	XORQ CX,CX
	MOVQ R10,DX
avg16_vec:
	CMPQ DX,$8
	JLT avg16_tail4
	MOVOU (SI)(CX*2),X0
	MOVOU (BX)(CX*2),X1
	PAVGW X1,X0
	MOVOU X0,(DI)(CX*2)
	ADDQ $8,CX
	SUBQ $8,DX
	JMP avg16_vec
avg16_tail4:
	CMPQ DX,$4
	JLT avg16_scalar
	MOVQ (SI)(CX*2),X0
	MOVQ (BX)(CX*2),X1
	PAVGW X1,X0
	MOVQ X0,(DI)(CX*2)
	ADDQ $4,CX
	SUBQ $4,DX
avg16_scalar:
	TESTQ DX,DX
	JEQ avg16_next
	MOVWQZX (SI)(CX*2),AX
	MOVWQZX (BX)(CX*2),R12
	LEAQ 1(AX)(R12*1),AX
	SHRQ $1,AX
	MOVW AX,(DI)(CX*2)
	INCQ CX
	DECQ DX
	JMP avg16_scalar
avg16_next:
	ADDQ R8,DI
	ADDQ R9,SI
	ADDQ R9,BX
	DECQ R11
	JNE avg16_row
	RET
