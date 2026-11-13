// Package dpb 实现解码图像缓冲（Decoded Picture Buffer）。
//
// 职责：参考帧管理、POC 计算与重排序、参考列表构造与重排序、
// 滑动窗口与 MMCO 标记、延迟输出。
//
// 关键约束：
//   - DPB 不是"只存上一帧"：Main/High 的多参考、长期参考、B 帧重排
//     都依赖完整的列表语义；
//   - 任何标记操作必须先检查 picture_id 合法性，码流里的
//     difference_of_pic_nums_minus1 完全可以指向不存在的帧；
//   - 输出顺序由 POC 决定，与解码顺序无关；Flush 时必须把剩余帧按
//     POC 顺序全部吐出，最后返回 io.EOF。
package dpb
