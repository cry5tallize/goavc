//go:build amd64 && !purego

#include "textflag.h"

// 每对行两次独立16-byte load；VINSERTI128只读第二行的16字节。
// 最后奇数行用VEX.128加载清零YMM高半部后做相同256位运算。
TEXT ·sad8x16AVX2(SB), NOSPLIT, $0-80
	MOVQ a_base+0(FP), SI
	MOVQ as+24(FP), R8
	MOVQ b_base+32(FP), DI
	MOVQ bs+56(FP), R9
	MOVQ height+64(FP), R11
	VPXOR Y0, Y0, Y0
sad8_avx_pair:
	CMPQ R11, $2
	JL sad8_avx_last
	VMOVDQU (SI), X1
	VINSERTI128 $1, (SI)(R8*1), Y1, Y1
	VMOVDQU (DI), X2
	VINSERTI128 $1, (DI)(R9*1), Y2, Y2
	VPSADBW Y2, Y1, Y1
	VPADDQ Y1, Y0, Y0
	LEAQ (SI)(R8*2), SI
	LEAQ (DI)(R9*2), DI
	SUBQ $2, R11
	JMP sad8_avx_pair
sad8_avx_last:
	TESTQ R11, R11
	JE sad8_avx_done
	VMOVDQU (SI), X1
	VMOVDQU (DI), X2
	VPSADBW Y2, Y1, Y1
	VPADDQ Y1, Y0, Y0
sad8_avx_done:
	VEXTRACTI128 $1, Y0, X1
	VPADDQ X1, X0, X0
	VMOVQ X0, AX
	VPSRLDQ $8, X0, X1
	VMOVQ X1, DX
	ADDQ DX, AX
	VZEROUPPER
	MOVQ AX, ret+72(FP)
	RET

// 先求unsigned绝对差，每对<=2046；按128位lane拆解后扩为64位，
// 所有跨行累加均在qword域，不依赖BMI/FMA或32位总和不会溢出的假设。
TEXT ·sad16x16AVX2(SB), NOSPLIT, $0-80
	MOVQ a_base+0(FP), SI
	MOVQ as+24(FP), R8
	MOVQ b_base+32(FP), DI
	MOVQ bs+56(FP), R9
	SHLQ $1, R8
	SHLQ $1, R9
	MOVQ height+64(FP), R11
	VPXOR Y0, Y0, Y0
	VPXOR Y5, Y5, Y5
	VPCMPEQW Y4, Y4, Y4
	VPSRLW $15, Y4, Y4
sad16_avx_row:
	VMOVDQU (SI), Y1
	VMOVDQU (DI), Y2
	VPSUBUSW Y2, Y1, Y3
	VPSUBUSW Y1, Y2, Y2
	VPOR Y2, Y3, Y1
	VPMADDWD Y4, Y1, Y1
	VPUNPCKLDQ Y5, Y1, Y2
	VPUNPCKHDQ Y5, Y1, Y1
	VPADDQ Y2, Y1, Y1
	VPADDQ Y1, Y0, Y0
	ADDQ R8, SI
	ADDQ R9, DI
	DECQ R11
	JNZ sad16_avx_row
	VEXTRACTI128 $1, Y0, X1
	VPADDQ X1, X0, X0
	VMOVQ X0, AX
	VPSRLDQ $8, X0, X1
	VMOVQ X1, DX
	ADDQ DX, AX
	VZEROUPPER
	MOVQ AX, ret+72(FP)
	RET
