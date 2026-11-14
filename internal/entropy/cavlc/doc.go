// Package cavlc 实现上下文自适应变长编码（CAVLC）。
//
// 覆盖 coeff_token、trailing ones、total zeros、run before、
// level prefix/suffix 与 nC 上下文推导，编码与解码双向实现。
//
// 4:2:2 色度 DC 使用 ChromaDC422（隐含 nC=-2），输入输出前八项为
// 2×4 光栅顺序，由 transform.DC2x4Scan 扫描；420 的 ChromaDC 不变。
//
// 关键约束：
//   - 规范 VLC 数值集中在 tables.go，直接查找表由 buildVLC 校验前缀并构建；
//   - 解码侧必须对每个码字做长度上限保护，并在读取失败时返回错误，
//     不允许返回"零系数"继续解析——那会把坏码流伪装成全零块；
//   - 编码侧必须能处理极端分布：全零块、长游程、需要转义的大 level。
package cavlc
