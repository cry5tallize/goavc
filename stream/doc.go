// Package stream 提供文件与流式 I/O 适配层。
//
// 提供YUV原始帧读写、Annex-B/AVCC增量读写、Access Unit组装，以及同步
// 有界pipeline、背压、上下文取消与自动Flush/EOF。
//
// 关键约束：
//   - 支持任意大小的输入分块，NAL 起始码/header/payload 可以跨缓冲区边界；
//   - 输入缓冲必须有上限，不允许无限缓存（实时流场景下等于内存泄漏）；
//   - 不实现 MP4 demux/mux：这里只处理 Annex-B 与长度前缀码流，
//     MP4/RTP/WebRTC 属于独立适配层。
//
// EncodeStream/DecodeStream不创建后台worker，也不关闭调用方I/O。可能永久
// 阻塞的端点须实现Context*接口或由所有者在取消时中断，参见docs/stream.md。
package stream
