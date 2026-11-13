// Package decoder 实现解码流水线。
//
// 流水线顺序：NAL → RBSP → SPS/PPS/SEI → Slice Header → 熵解码
// （CAVLC 或 CABAC）→ 反量化 → 逆变换 → 帧内/帧间预测 → 残差重建
// → 去块滤波 → 单参考 DPB/POC → 按 SPS 裁剪输出帧。
// M3 已接入 CABAC/CAVLC 的多参考 I/P、MMCO/长期参考和 POC type 0/1/2。
// B 帧、双向/direct、加权与显示重排序已接入；High 8×8/ScalingMatrix
// 继续在 M3 扩展，当前显式拒绝。
// 输入为完整 Annex-B 访问单元；增量字节流/AVCC 适配属于 M6。
//
// 关键约束：
//   - 本包不得 import encoder 包（单向依赖，见 docs/architecture.md R1）；
//   - 所有重建必须复用 internal/ 下的共享内核，不允许另写一套反变换；
//   - 遇到未支持工具返回 ErrUnsupported 并进入等待 IDR 状态，
//     不得带着可能损坏的参考帧继续解码。
package decoder
