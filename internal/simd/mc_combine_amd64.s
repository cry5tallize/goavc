//go:build amd64 && !purego

#include "textflag.h"

// 分别执行相位组合及最终双向均值，两次PAVGB与两次标量舍入严格一致。
TEXT ·mcCombine8SSE2(SB), NOSPLIT, $0-113
	MOVQ dst_base+0(FP),DI
	MOVQ ds+24(FP),R8
	MOVQ a_base+32(FP),SI
	MOVQ as+56(FP),R9
	MOVQ b_base+64(FP),BX
	MOVQ bs+88(FP),R10
	MOVQ w+96(FP),R11
	MOVQ h+104(FP),R12
	MOVBQZX average+112(FP),R13
combine_row:
	XORQ CX,CX
	MOVQ R11,DX
combine_16:
	CMPQ DX,$16
	JLT combine_8
	MOVOU (SI)(CX*1),X0
	TESTQ BX,BX
	JEQ combine16_output
	MOVOU (BX)(CX*1),X1
	PAVGB X1,X0
combine16_output:
	TESTQ R13,R13
	JEQ combine16_store
	MOVOU (DI)(CX*1),X1
	PAVGB X1,X0
combine16_store:
	MOVOU X0,(DI)(CX*1)
	ADDQ $16,CX
	SUBQ $16,DX
	JMP combine_16
combine_8:
	CMPQ DX,$8
	JLT combine_4
	MOVQ (SI)(CX*1),X0
	TESTQ BX,BX
	JEQ combine8_output
	MOVQ (BX)(CX*1),X1
	PAVGB X1,X0
combine8_output:
	TESTQ R13,R13
	JEQ combine8_store
	MOVQ (DI)(CX*1),X1
	PAVGB X1,X0
combine8_store:
	MOVQ X0,(DI)(CX*1)
	ADDQ $8,CX
	SUBQ $8,DX
combine_4:
	CMPQ DX,$4
	JLT combine_tail
	MOVL (SI)(CX*1),X0
	TESTQ BX,BX
	JEQ combine4_output
	MOVL (BX)(CX*1),X1
	PAVGB X1,X0
combine4_output:
	TESTQ R13,R13
	JEQ combine4_store
	MOVL (DI)(CX*1),X1
	PAVGB X1,X0
combine4_store:
	MOVL X0,(DI)(CX*1)
	ADDQ $4,CX
	SUBQ $4,DX
combine_tail:
	TESTQ DX,DX
	JEQ combine_next
	MOVBQZX (SI)(CX*1),AX
	TESTQ BX,BX
	JEQ combine_tail_output
	MOVBQZX (BX)(CX*1),R14
	LEAQ 1(AX)(R14*1),AX
	SHRQ $1,AX
combine_tail_output:
	TESTQ R13,R13
	JEQ combine_tail_store
	MOVBQZX (DI)(CX*1),R14
	LEAQ 1(AX)(R14*1),AX
	SHRQ $1,AX
combine_tail_store:
	MOVB AX,(DI)(CX*1)
	INCQ CX
	DECQ DX
	JMP combine_tail
combine_next:
	ADDQ R8,DI
	ADDQ R9,SI
	TESTQ BX,BX
	JEQ combine_next_row
	ADDQ R10,BX
combine_next_row:
	DECQ R12
	JNE combine_row
	RET
