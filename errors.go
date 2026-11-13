package h264

import (
	"errors"
	"io"
)

// 编解码器与流式管道对外暴露的唯一错误集合。
//
// 约定：调用方一律使用 errors.Is 判断，因为内部实现会用 %w 包装上下文
// （例如 "plane 1 stride 8 below row size 16"）。直接比较 == 只对哨兵值本身成立。
var (
	// ErrAgain 表示"当前没有输出可拿"。
	//
	// 语义要点：它不是错误状态，而是流控信号。Send 成功消费输入之后，
	// 调用方应当继续调用 Receive，直到拿到帧或者拿到 ErrAgain；
	// Encoder 的 B 帧重排、Decoder 的 DPB 显示顺序重排都会产生这类空窗期。
	ErrAgain = errors.New("h264: no output available")

	// ErrEOF 表示码流结束且所有输出已排空。
	//
	// 它直接复用 io.EOF 而不是自定义错误，这样通用的 io 循环（io.Copy 风格、
	// 逐帧 for 循环）既可以判 errors.Is(err, h264.ErrEOF)，也可以判 io.EOF，
	// 不会因为用了库内别名而漏判。
	ErrEOF = io.EOF

	// ErrInvalidData 表示输入不是合法码流（语法错误、截断、越界）。
	//
	// 该错误是致命的：Decoder 必须丢弃当前 slice 状态并进入等待 IDR 的状态，
	// 不能带着可能已损坏的参考帧继续解码，否则会把错误扩散到后续所有帧。
	ErrInvalidData = errors.New("h264: invalid data")

	// ErrUnsupported 表示"码流本身合法，但本实现尚未支持"。
	//
	// 必须与 ErrInvalidData 严格区分：前者是兼容性边界（例如 M0 阶段遇到
	// CABAC 或 B Slice），后者是输入有问题。把不支持伪装成"能跑但结果错"
	// 是本项目明确禁止的行为。
	ErrUnsupported = errors.New("h264: unsupported feature")

	// ErrResourceLimit 表示超出配置的资源上限（分辨率、NAL 大小、DPB 数量等）。
	//
	// 这是抵御恶意码流的最后一道闸门：任何由码流字段推导出的分配大小，
	// 都必须先与上限比较，再决定是否分配。
	ErrResourceLimit = errors.New("h264: resource limit exceeded")
)

// IsFatal 判断 err 是否会使当前解码状态失效。
//
// 判定依据：语法错误、未支持特性、超限都需要丢弃当前图像和参考状态，
// 经 Reset 或后续 IDR 恢复后才能继续解码；
// 而 ErrAgain / io.EOF 属于正常流控，不是致命错误。
// 返回 false 不代表可以忽略 err，只代表管道状态仍然有效。
func IsFatal(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrInvalidData) ||
		errors.Is(err, ErrUnsupported) ||
		errors.Is(err, ErrResourceLimit)
}
