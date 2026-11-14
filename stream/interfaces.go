package stream

import h264 "github.com/goavc/h264"

// FrameReader按显示顺序读取完整帧。正常结束返回io.EOF，不完整尾帧返回错误。
// 成功帧归调用方所有，用完Release；后续读取不得覆盖此前返回的帧。
// 实例非并发安全；取消与背压由后续pipeline层管理。
type FrameReader interface {
	ReadFrame() (*h264.Frame, error)
}

// FrameWriter同步消费一帧，返回后不得保留Frame或平面Data引用。
// Writer不拥有输入帧，调用方仍负责Release；错误时也不转移所有权。
type FrameWriter interface {
	WriteFrame(*h264.Frame) error
}

// AccessUnitReader读取一个完整AU及其时间戳，正常结束返回io.EOF。
// Packet.Data归调用方所有，后续读取不得覆盖它；封装格式由具体实现声明。
type AccessUnitReader interface {
	ReadAccessUnit() (h264.Packet, error)
}

// AccessUnitWriter同步写出一个完整AU，返回后不得保留Packet.Data引用。
// 时间戳是否可序列化由封装格式决定，不能假定裸Annex-B携带容器时间戳。
type AccessUnitWriter interface {
	WriteAccessUnit(h264.Packet) error
}
