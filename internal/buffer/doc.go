// Package buffer 提供帧、图像与临时缓冲的池化管理。
//
// 目标：长时间流式处理时内存曲线平稳，热路径零额外分配。
//
// 关键约束：
//   - 池化对象必须显式归还（Frame.Release），且归还后不得再有引用；
//   - 池不限制总量，但受 DecoderConfig 的上限约束，超限返回 ErrResourceLimit；
//   - 临时缓冲按 slice 粒度复用，不跨 goroutine 共享；
//   - 不允许把池化缓冲交给库外长期持有：需要长期持有时调用 Clone。
package buffer
