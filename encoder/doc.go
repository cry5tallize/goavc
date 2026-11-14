// Package encoder 实现编码流水线。
//
// 流水线顺序：预处理 → GOP/帧类型决策 → 运动估计 → 模式决策
// → 预测 → 残差 → 正变换 → 量化 → 熵编码 → NAL 写出
// → 本地重建 → 去块滤波 → 参考帧入库。
//
// 支持 8/10-bit 420、422、444；高位深公共输入和重建均为 LE，stride 以字节计。
// 语法 QPY 在 10-bit 时允许 -12..51，量化/重建显式转换为 QP'；
// CABAC 上下文仍用裁剪后的语法 QPY。422 使用八项 DC，444 三平面共享预测
// 模式并独立变换、矩阵和非零统计；WithSeparateColourPlanes 则写出三个独立
// colour_plane_id slice 集合，复用同一 I/P/B 内核。默认是真实残差压缩，
// WithPCM 才是显式全 PCM 通道。
// WithInterlaced选择真实的全场MBAFF宏块对，TFF/BFF由逐帧pic_timing声明；
// 不进行自适应帧/场RDO，PAFF仍不生成。宏块QP回调的y为2*场MB行+奇偶。
//
// 关键约束：
//   - 本包不得 import decoder 包（单向依赖，见 docs/architecture.md R1）；
//   - 本地重建必须复用解码器相同的内核，否则重建帧与解码输出会漂移；
//   - 码率控制独立成模块，不允许散落在模式决策里；
//   - 共同平面产物与FFmpeg/自研解码逐像素核对；独立平面用规范与单色投影验证。
package encoder
