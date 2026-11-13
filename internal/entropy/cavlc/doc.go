// Package cavlc 实现上下文自适应变长编码（CAVLC）。
//
// 覆盖 coeff_token、trailing ones、total zeros、run before、
// level prefix/suffix 与 nC 上下文推导，编码与解码双向实现。
//
// 关键约束：
//   - 查找表由生成器产出（tables_gen.go），禁止手工编辑生成文件；
//   - 解码侧必须对每个码字做长度上限保护，并在读取失败时返回错误，
//     不允许返回"零系数"继续解析——那会把坏码流伪装成全零块；
//   - 编码侧必须能处理极端分布：全零块、长游程、需要转义的大 level。
package cavlc
