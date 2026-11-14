# CABAC 数值与生成表

规范依据：[ITU-T H.264 (08/2024)](https://www.itu.int/rec/T-REC-H.264-202408-I/en)：
9.3.1、9.3.3，初始化数值见表 9-12 至 9-24，算术区间与转移数值见表 9-44/9-45。

`context_init.csv` 按规范 ctxIdx=0..459 存储 I、P/B init_idc=0/1/2 的 (m,n)，
只包含标准规定的数值。编号 276 对应专门的 terminate 流程，普通 Bin 不使用它。
460 之后是 M7 的 4:4:4 扩展上下文，当前不纳入 Main/High/High422 引擎。

`go generate ./internal/entropy/cabac` 使用本仓库的 Go 生成器读取本地 CSV，
重建 `tables_gen.go`；预先设置 `CGO_ENABLED=0`。不手改生成物，不在生成时
访问网络、调用外部编解码工具或读取本机调试目录。

初始化测试逐一检查全部上下文、QP=0..51、I/PB0/PB1/PB2。算术单元测试
使用规范区间推导的固定序列，覆盖 MPS、LPS、状态0翻转、bypass、terminate、
截断后的粘性错误。真实像素证据由 decoder 的 FFmpeg Oracle 提供。

4:2:2 DC 的规范依据为 9.3.3.1.3（表 9-42、公式 9-16/9-18）：
`Residual` 沿用 `ChromaDC` 类别，使用八项 `transform.DC2x4Scan`；
`coded_block_flag` 仍由调用方在 `97+condA+2*condB` 编解码，零块跳过残差。
七个显著性/末位位置的上下文增量为 `{0,0,1,1,2,2,2}`，第八项隐含显著；
幅值大于一的后续 bin 使用 `5+min(3,numDecodAbsLevelGt1)`，不跨入色度 AC。
新增测试使用独立列出的规范 bin/ctxIdx 轨迹与 CBF 四种邻域条件；
CAVLC 的八 DC 则使用表 9-5 的 nC=-2 列及表 9-9(b)，没有改动生成输出。
