package syntax

import (
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/nal"
)

// DataPartitionInput 是一个已去除防竞争字节、不含NAL头的分区RBSP。
// BitLength 显式指定有效比特数，完整NAL应传 uint64(len(RBSP))*8；
// 允许最后一个字节不满8位，超出BitLength的比特不会成为合法语法。
// 解析器只读RBSP，不复制、拼接或改写其内容，也不验证后续数据和尾位。
type DataPartitionInput struct {
	RBSP      []byte
	BitLength uint64
	Header    nal.Header
}

// DataPartitionContext 保存与一个分区A关联的B/C前缀所需的最小上下文。
// Header 必须是对应分区A的NAL头；其余标志从该A使用的SPS/PPS取得。
// ParseDataPartitionA 直接生成此快照，因此参数集之后被替换不影响关联。
// 手工构造时，未出现的ColourPlaneID和RedundantPicCnt必须为0。
type DataPartitionContext struct {
	Header                     nal.Header
	SliceID                    uint32
	SeparateColourPlaneFlag    bool
	ColourPlaneID              uint8
	RedundantPicCntPresentFlag bool
	RedundantPicCnt            uint32
}

// DataPartitionA 包含完整slice_header、关联上下文和分区A数据的起始位。
// SliceHeader.HeaderBits仅统计slice_header；DataBitOffset还包含slice_id。
// 两个位偏移都相对于输入RBSP的第0位，绝不是字节偏移。
type DataPartitionA struct {
	SliceHeader   *SliceHeader
	Context       DataPartitionContext
	DataBitOffset uint64
}

// DataPartitionBC 是B/C的实际前缀语法值；Type区分类型3和4。
// 字段的存在性由调用时的DataPartitionContext决定，缺省值为0。
// DataBitOffset之后的残差仍在原始RBSP中，本接口不负责残差重组或解码。
type DataPartitionBC struct {
	Type            nal.UnitType
	SliceID         uint32
	ColourPlaneID   uint8
	RedundantPicCnt uint32
	DataBitOffset   uint64
}

// ParseDataPartitionA 解析类型2的slice_header和紧随其后的slice_id。
// A永远不是IDR；nal_ref_idc原样传给ParseSliceHeaderEx以决定参考图像标记。
// slice_id按图像宏块数（MBAFF按宏块对数）校验，CABAC不能用于数据分区。
// 本接口不检查跨NAL的顺序、slice_id唯一性或B/C是否应当存在；这些需要
// 调用方解析分区A数据后决定，不应仅凭I/P/B切片类型猜测。
func ParseDataPartitionA(in DataPartitionInput, sps *SPS, ppsByID map[uint32]*PPS) (*DataPartitionA, error) {
	if err := validatePartitionHeader(in.Header, nal.TypeSlicePartitionA); err != nil {
		return nil, err
	}
	if _, _, _, err := fmoDimensions(sps); err != nil {
		return nil, err
	}
	rbsp, err := partitionInputBytes(in)
	if err != nil {
		return nil, err
	}
	h, err := ParseSliceHeaderEx(rbsp, sps, ppsByID, false, in.Header.RefIDC)
	if err != nil {
		return nil, err
	}
	if h.HeaderBits > in.BitLength {
		return nil, fmt.Errorf("syntax: partition A slice_header truncated at bit %d: %w", in.BitLength, h264.ErrInvalidData)
	}
	pps := ppsByID[h.PicParameterSetID]
	if err := validateProfileSlice(sps, pps, h, true); err != nil {
		return nil, err
	}
	if pps.EntropyCodingModeFlag {
		return nil, fmt.Errorf("syntax: CABAC is not allowed for data partitions: %w", h264.ErrInvalidData)
	}
	r := bitstream.NewReader(rbsp)
	if err := r.SkipBits(uint(h.HeaderBits)); err != nil {
		return nil, err
	}
	id, err := readPartitionUE(r, in.BitLength, "slice_id")
	if err != nil {
		return nil, err
	}
	// PicSizeInMbs = 宽*图像高；场图像高减半，MBAFF的ID按宏块对计数。
	width := uint64(sps.PicWidthInMbsMinus1) + 1
	height := uint64(sps.PicHeightInMapUnitsMinus1) + 1
	if !sps.FrameMbsOnlyFlag && !h.FieldPicFlag && !sps.MbAdaptiveFrameFieldFlag {
		height *= 2
	}
	if width > ^uint64(0)/height || uint64(id) >= width*height {
		return nil, fmt.Errorf("syntax: partition A slice_id %d outside picture range: %w", id, h264.ErrInvalidData)
	}
	return &DataPartitionA{
		SliceHeader: h,
		Context: DataPartitionContext{
			Header: in.Header, SliceID: id,
			SeparateColourPlaneFlag:    sps.SeparateColourPlaneFlag,
			ColourPlaneID:              h.ColourPlaneID,
			RedundantPicCntPresentFlag: pps.RedundantPicCntPresentFlag,
			RedundantPicCnt:            h.RedundantPicCnt,
		},
		DataBitOffset: r.BitsRead(),
	}, nil
}

// ParseDataPartitionBC 按对应A的SPS/PPS上下文解析类型3或4的前缀。
// 校验slice_id、colour_plane_id和redundant_pic_cnt与A一致，且NAL头的
// 参考/非参考属性一致。非零nal_ref_idc是传输优先级，B/C可与A取不同非零值。
// 语法错误、截断、错误NAL类型及不一致上下文均包装ErrInvalidData。
func ParseDataPartitionBC(in DataPartitionInput, ctx DataPartitionContext) (*DataPartitionBC, error) {
	if in.Header.Type != nal.TypeSlicePartitionB && in.Header.Type != nal.TypeSlicePartitionC {
		return nil, fmt.Errorf("syntax: expected partition B/C NAL type 3/4, got %d: %w", in.Header.Type, h264.ErrInvalidData)
	}
	if err := validatePartitionHeader(in.Header, in.Header.Type); err != nil {
		return nil, err
	}
	if err := validatePartitionHeader(ctx.Header, nal.TypeSlicePartitionA); err != nil {
		return nil, fmt.Errorf("syntax: invalid partition A context: %w", err)
	}
	if (in.Header.RefIDC == 0) != (ctx.Header.RefIDC == 0) {
		return nil, fmt.Errorf("syntax: partition B/C reference status differs from A: %w", h264.ErrInvalidData)
	}
	if ctx.ColourPlaneID > 2 || (!ctx.SeparateColourPlaneFlag && ctx.ColourPlaneID != 0) ||
		ctx.RedundantPicCnt > 127 || (!ctx.RedundantPicCntPresentFlag && ctx.RedundantPicCnt != 0) {
		return nil, fmt.Errorf("syntax: invalid partition A conditional context: %w", h264.ErrInvalidData)
	}
	rbsp, err := partitionInputBytes(in)
	if err != nil {
		return nil, err
	}
	r := bitstream.NewReader(rbsp)
	h := &DataPartitionBC{Type: in.Header.Type}
	if h.SliceID, err = readPartitionUE(r, in.BitLength, "slice_id"); err != nil {
		return nil, err
	}
	if h.SliceID != ctx.SliceID {
		return nil, fmt.Errorf("syntax: partition B/C slice_id %d differs from A %d: %w", h.SliceID, ctx.SliceID, h264.ErrInvalidData)
	}
	if ctx.SeparateColourPlaneFlag {
		if in.BitLength-r.BitsRead() < 2 {
			return nil, fmt.Errorf("syntax: colour_plane_id truncated at bit %d: %w", in.BitLength, h264.ErrInvalidData)
		}
		plane, err := readBits(r, 2, "colour_plane_id")
		if err != nil {
			return nil, err
		}
		h.ColourPlaneID = uint8(plane)
		if h.ColourPlaneID > 2 || h.ColourPlaneID != ctx.ColourPlaneID {
			return nil, fmt.Errorf("syntax: partition B/C colour_plane_id %d invalid or differs from A %d: %w", h.ColourPlaneID, ctx.ColourPlaneID, h264.ErrInvalidData)
		}
	}
	if ctx.RedundantPicCntPresentFlag {
		if h.RedundantPicCnt, err = readPartitionUE(r, in.BitLength, "redundant_pic_cnt"); err != nil {
			return nil, err
		}
		if h.RedundantPicCnt > 127 || h.RedundantPicCnt != ctx.RedundantPicCnt {
			return nil, fmt.Errorf("syntax: partition B/C redundant_pic_cnt %d invalid or differs from A %d: %w", h.RedundantPicCnt, ctx.RedundantPicCnt, h264.ErrInvalidData)
		}
	}
	h.DataBitOffset = r.BitsRead()
	return h, nil
}

func validatePartitionHeader(h nal.Header, typ nal.UnitType) error {
	if h.ForbiddenZeroBit || h.RefIDC > 3 || h.Type != typ {
		return fmt.Errorf("syntax: invalid data partition NAL header (type=%d ref_idc=%d forbidden=%t), expected type %d: %w",
			h.Type, h.RefIDC, h.ForbiddenZeroBit, typ, h264.ErrInvalidData)
	}
	return nil
}

func partitionInputBytes(in DataPartitionInput) ([]byte, error) {
	if in.BitLength > uint64(len(in.RBSP))*8 {
		return nil, fmt.Errorf("syntax: partition bit length %d exceeds RBSP size: %w", in.BitLength, h264.ErrInvalidData)
	}
	return in.RBSP[:(in.BitLength+7)/8], nil
}

// readPartitionUE 在检查语义值之前拒绝落入最后一个字节无效区域的码字。
func readPartitionUE(r *bitstream.Reader, bitLength uint64, name string) (uint32, error) {
	value, err := readUE(r, name)
	if err != nil {
		return 0, err
	}
	if r.BitsRead() > bitLength {
		return 0, fmt.Errorf("syntax: %s truncated at bit %d: %w", name, bitLength, h264.ErrInvalidData)
	}
	return value, nil
}
