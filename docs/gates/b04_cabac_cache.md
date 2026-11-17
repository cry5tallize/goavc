# B04：读位缓存与固定残差扫描

父版本`31d0308`，环境HW-M2、Go1.27.4 windows/amd64、CGO0、GOAMD64v1、
GOMAXPROCS16、codec Threads1。完整五样本、输入SHA及补测见[b04_samples.json](b04_samples.json)。

ReadBit直接消费Reader已有reservoir，空缓存继续原ReadBits冷路径；CABAC每次读取
立即更新逻辑位数，没有私有预读游标。残差保留唯一算术流程，ResidualScan拥有扫描
副本并在构造时校验。decoder只按固定表身份接入4/15/16项；未知/自定义、8项422 DC
与64项8×8仍走通用校验。原通用API的非法scan清零前缀不变。

| 测量 | before→after ns/op中位 | 结论 |
|---|---:|---|
| 完整15项残差 | 512.9→492.6 | -4.0% |
| 完整16项残差 | 528.6→490.7 | -7.2% |
| 完整64项残差 | 1051→1040 | 持平 |
| 直接AU IDR | 34928512→34576587 | -1.0%，持平 |
| 直接AU 32帧I/P/B首轮 | 833243800→867562700 | +4.1%，保留 |
| I/P/B唯一配对补测 | 881802600→882011900 | +0.02%，持平 |

完整残差基准每op重建Reader，两侧均64B/1alloc；不能报告为0alloc。
另用复用Reader/Decoder的同版本辅助基准（200ms×5、0B/0alloc），
4/15/16项通用→prepared为284.5→241.7、463.7→366.8、537.2→516ns；
8项397.1→388.1收益未确认，64项921.3→1158变慢，因此不接入这两个默认形状。
原含Reader分配的辅助数据单列保存，不混算。短端点没有可重复回退，不宣称吞吐改善。
正式端点每对独立进程、交替顺序，IDR500ms/IPB1x，含实例完整生命周期，不含I/O/组AU。

逐状态旧引擎trace比较bin/context/range/offset/lastBit/终止/错误/消费位数与后续Peek，
混合Read/Peek/Skip/Align、PCM restart及残差十四类别/帧场/逐字节截断通过。
CGO0完整gofmt/vet/build/test/deps（36包）/manifest（55条）、两个直接AU FFmpeg像素
和显式加权编码三方Oracle通过；算术/残差fuzz各5秒parallel2，295027/303863次exec，
仅为烟测。purego/386/race/六目标的新一轮集成证据由B05收尾单列，不借用A轮旧证据。
