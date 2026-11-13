// Package decoder 实现解码流水线。
//
// 流水线顺序：NAL → RBSP → SPS/PPS/SEI → Slice Header → 熵解码
// （CAVLC/CABAC）→ 反量化 → 逆变换 → 帧内/帧间预测 → 残差重建
// → 去块滤波 → DPB/POC 重排序 → 输出帧。
//
// 关键约束：
//   - 本包不得 import encoder 包（单向依赖，见 docs/architecture.md R1）；
//   - 所有重建必须复用 internal/ 下的共享内核，不允许另写一套反变换；
//   - 遇到未支持工具返回 ErrUnsupported 并进入等待 IDR 状态，
//     不得带着可能损坏的参考帧继续解码。
package decoder
