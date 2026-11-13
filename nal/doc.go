// Package nal 实现 NAL Unit 的封装与拆封。
//
// 覆盖两种容器格式：
//   - Annex-B：起始码（00 00 01 / 00 00 00 01）+ NAL Unit；
//   - AVCC：大端长度前缀（1/2/3/4 字节可配）+ NAL Unit，外加 avcC 配置记录。
//
// 关键约束：
//   - 扫描必须支持任意分块输入：起始码、NAL header、payload 都可能被切在
//     两个 buffer 之间，不允许假设"一次 Read 就是一个完整 NAL"；
//   - Access Unit 的边界不能按"一个 NAL 一帧"判断，必须结合 AUD、
//     first_mb_in_slice、frame_num、pic_order_cnt_lsb 等字段。
package nal
