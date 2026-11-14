# CABAC 数值与生成表

规范依据：[ITU-T H.264 (08/2024)](https://www.itu.int/rec/T-REC-H.264-202408-S/en)：
9.3.1、9.3.3，初始化数值见表 9-12 至 9-33，算术区间与转移见表 9-44/9-45。
444 扩展数值直接依据 [ITU-T H.264 (2007)/Amd.2](https://www.itu.int/rec/dologin_pub.asp?id=T-REC-H.264-200704-T!Amd2!PDF-E&lang=e&type=items)
表 9-24a..9-24i（合订本对应表 9-25..9-33），不取自外部编解码实现。

`context_init.csv` 存储 ctxIdx=0..459 的 I、P/B init_idc=0/1/2 的 (m,n)，
`context_alias.csv` 按上述规范中相同数值的区间声明 ctxIdx=460..1023；生成器
逐项展开为完整的 `[4][1024][2]int8` 表，并检查区间连续、引用已定义与边界。
编号 276 对应专门的 terminate 流程，普通 Bin 不使用它。
旧 `ContextCount=460`、`InitContexts` 仍只初始化460项；新增
`ContextCount444=1024`、`InitContexts444(intra bool,idc uint32,qp int)` 返回1024项。

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

444 残差使用 `Decoder.Residual444` / `Encoder.Residual444`，参数依次为
`ctx *[ContextCount444]Context, cat int, dst/src []int32, scan []int, field bool`，
返回 `(int,error)`。旧 `Residual` 仅接受类别0..5和460模型域；
`ResidualField` 参数同旧接口，使用场上下文。所有入口借用数组切片，共用一份
残差算术，不分配、不复制上下文；nil模型指针返回 `ErrInvalidData`。

表9-42的444类别为 `CbDC=6,CbAC=7,Cb4x4=8,Cb8x8=9`、
`CrDC=10,CrAC=11,Cr4x4=12,Cr8x8=13`。
`CodedBlockFlagBase(cat int)(int,error)` 返回14类CBF基址：
`85,89,93,97,101,1012,460,464,468,1016,472,476,480,1020`。
调用方另加邻域增量0..3；非444的8×8仍按语法省略CBF。
帧场sig/last/level基址依据表9-39/9-40，8×8的场显著性增量依据表9-43，
末位增量帧场相同。`field` 只选择模型，`scan` 仍须由调用方提供正确帧场扫描。

新增独立测试包含规范固定码字 `D180`、14类逐位置帧场bin/ctxIdx轨迹、
幅值饱和与UEG0逃逸轨迹、截断/非法类别/容量/nil指针，以及1024项M/N、
四初始化模式、QP=0..51和两侧裁剪边界；不以残差自往返替代规范证据。

