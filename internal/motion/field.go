package motion

// FieldStructure 指明 MV 的坐标单位及当前宏块所属的场。
// FrameMotion 的纵向单位是 1/4 帧亮度样点；两个场模式的纵向单位
// 都是 1/4 场亮度样点。Top/Bottom 是场奇偶，不是解码顺序或 POC 顺序。
// PAFF 使用 field_pic_flag/bottom_field_flag；MBAFF 场宏块使用
// mb_field_decoding_flag 及宏块地址的奇偶（偶地址顶场，奇地址底场）。
type FieldStructure uint8

const (
	FrameMotion FieldStructure = iota
	TopFieldMotion
	BottomFieldMotion
)

// FieldNeighbor 是一个已由调用者定位的 A、B、C 或 D 邻分区，限同一参考列表。
// Ref 使用该邻宏块原生参考列表的 ref_idx；负数表示帧内或未使用此列表。
// Available 表示规范的邻分区可用性，必须已经考虑 slice、解码顺序及
// 6.4.12 的 MBAFF 地址/分区推导；不能用 Ref >= 0 代替 Available。
// 尤其可用的帧内 C 不得触发 C 到 D 的回退。
type FieldNeighbor struct {
	MV        MV
	Ref       int8
	Structure FieldStructure
	Available bool
}

// FieldNeighbors 按当前分区的局部样点坐标选择：A=(-1,0)，B=(0,-1)，
// C=(分区宽度,-1)，D=(-1,-1)。MBAFF 的坐标到宏块/分区地址转换由
// frameState 负责，不能直接在帧式 Grid 上对场宏块使用 by-1。
type FieldNeighbors struct {
	A FieldNeighbor
	B FieldNeighbor
	C FieldNeighbor
	D FieldNeighbor
}

// FieldPartition 选择 8.4.1.3 的分区定向预测规则。分区尺寸以当前宏块
// 自身的帧/场样点计；场 16×8 不得因其覆盖 16 帧行而当成 16×16。
// 16×16、8×8 及所有子宏块分区使用 FieldMedian。
type FieldPartition uint8

const (
	FieldMedian FieldPartition = iota
	Field16x8Top
	Field16x8Bottom
	Field8x16Left
	Field8x16Right
)

// NormalizeFieldNeighbor 实现 8.4.1.3.2 的邻块 MV/ref_idx 归一化，
// 返回值全部采用 current 的单位与参考索引空间，保留原始可用性。
// 调用者应传入合法结构及参考索引（帧 0..31，场 0..63，未使用为 -1）。
//
// 场当前块/帧邻块：mvY = mvY / 2，ref_idx = 2 * ref_idx；
// 帧当前块/场邻块：mvY = 2 * mvY，ref_idx = ref_idx / 2。
// 此处规范的 / 向零截断，因此 -7 / 2 = -3、-1 / 2 = 0；不能用
// 算术右移代替 MV 除法。负 ref_idx 在缩放之前归为 -1，不能乘成 -2。
//
// 两个场邻块之间即使奇偶不同，也不改 MV 或 ref_idx：索引是各自场列表
// 的相对索引，而非绝对顶/底场编号。亮度 MVP 不添加场奇偶位移；场参考
// 选择与运动补偿中的样点定位是另一过程。PAFF 同一场图像内的邻块同场，
// 不应把另一幅场图像的块作为空间邻块传入。
func NormalizeFieldNeighbor(current FieldStructure, n FieldNeighbor) FieldNeighbor {
	if !n.Available || n.Ref < 0 {
		return FieldNeighbor{Ref: -1, Structure: current, Available: n.Available}
	}
	if current != FrameMotion && n.Structure == FrameMotion {
		n.MV.Y /= 2
		n.Ref *= 2
	} else if current == FrameMotion && n.Structure != FrameMotion {
		n.MV.Y *= 2
		n.Ref /= 2
	}
	n.Structure = current
	return n
}

// MBAFFFieldReference 把 8.2.4.2.5 生成的 MBAFF 场参考列表索引拆成
// 原帧参考列表索引与绝对参考场奇偶。每个参考帧先列与当前宏块同奇偶的场，
// 再列另一场；所以底场的偶数索引引用底场，而不是总是引用顶场。
// 返回的帧索引仍须在当前 slice 重排后的原帧列表中查找，不能作为 DPB 下标。
// 输入帧模式、未使用列表或超出场索引范围时 ok=false。
// PAFF 场图像的列表可能独立重排，不能用这个 MBAFF 专用拆分替代 PAFF 列表。
func MBAFFFieldReference(current FieldStructure, ref int8) (frameRef int8, field FieldStructure, ok bool) {
	if (current != TopFieldMotion && current != BottomFieldMotion) || ref < 0 || ref > 63 {
		return -1, FrameMotion, false
	}
	field = current
	if ref&1 != 0 {
		if current == TopFieldMotion {
			field = BottomFieldMotion
		} else {
			field = TopFieldMotion
		}
	}
	return ref / 2, field, true
}

// PredictField 对已经定位的帧/场邻分区执行 8.4.1.3 预测。
// ref 是当前分区原生列表中的合法索引；返回 MV 是当前分区帧/场坐标单位，
// 尚未加 mvd，也未做运动补偿边界裁剪。对 list0/list1 分别调用即可。
// 归一化发生在参考匹配和分区优先规则之前；C 不可用才取 D。
func PredictField(current FieldStructure, ref int8, partition FieldPartition, neighbors FieldNeighbors) MV {
	return predictField(current, ref, partition, neighbors, false)
}

// PredictFieldSkip 实现 P_Skip 的场/帧邻块规则（8.4.1.1）。参考索引恒为
// 当前原生列表的 0；先归一化，再判断 A/B 不可用或 ref=0 的零 MV 捷径。
// B_Skip 的 direct 运动推导不是本函数的 P_Skip 过程。
func PredictFieldSkip(current FieldStructure, neighbors FieldNeighbors) MV {
	return predictField(current, 0, FieldMedian, neighbors, true)
}

func predictField(current FieldStructure, ref int8, partition FieldPartition, neighbors FieldNeighbors, skip bool) MV {
	// 小型栈上邻块视图只桥接已有 Predict/定向预测/Skip，不复制中值与
	// 参考匹配算法，也不分配整幅 Grid。这里的坐标不是实际图像坐标。
	var vectors [12]MV
	var refs [12]int8
	var valid [12]bool
	g := Grid{MBWidth: 2, BlockW: 6, BlockH: 2, MV: vectors[:], Ref: refs[:], valid: valid[:]}
	partW := 4
	if partition == Field8x16Left || partition == Field8x16Right {
		partW = 2
	}
	putFieldNeighbor(&g, 6, current, neighbors.A)
	putFieldNeighbor(&g, 1, current, neighbors.B)
	putFieldNeighbor(&g, 1+partW, current, neighbors.C)
	putFieldNeighbor(&g, 0, current, neighbors.D)
	if skip {
		return PredictSkip(&g, 1, 1, ref)
	}
	switch partition {
	case Field16x8Top:
		return Predict16x8(&g, 1, 1, false, ref)
	case Field16x8Bottom:
		return Predict16x8(&g, 1, 1, true, ref)
	case Field8x16Left:
		return Predict8x16(&g, 1, 1, false, ref)
	case Field8x16Right:
		return Predict8x16(&g, 1, 1, true, ref)
	default:
		return Predict(&g, 1, 1, partW, ref)
	}
}

func putFieldNeighbor(g *Grid, index int, current FieldStructure, n FieldNeighbor) {
	n = NormalizeFieldNeighbor(current, n)
	g.MV[index] = n.MV
	g.Ref[index] = n.Ref
	g.valid[index] = n.Available
}
