// Package stream 提供文件与流式 I/O 适配层。
//
// 规划范围：YUV 原始帧读写、Annex-B / AVCC 增量读写、Access Unit 组装、
// 有界缓冲、背压、上下文取消、Flush/EOF 语义。
//
// 关键约束：
//   - 支持任意大小的输入分块，NAL 起始码/header/payload 可以跨缓冲区边界；
//   - 输入缓冲必须有上限，不允许无限缓存（实时流场景下等于内存泄漏）；
//   - 不实现 MP4 demux/mux：这里只处理 Annex-B 与长度前缀码流，
//     MP4/RTP/WebRTC 属于独立适配层。
//
// 当前已实现四个流接口、8-bit YUV读写、有界Annex-B/AVCC NAL Reader及AU Reader；
// pipeline、取消、背压和自动Flush尚未接入，不能据本包存在宣称M6完整门禁通过。
package stream
