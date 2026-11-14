# M5c/M5d 本地进展记录

日期：2026-11-14；Go 1.27.4 windows/amd64；常规验证 `CGO_ENABLED=0`。
当前分支 `feat/m5c-main-high`。本文件记录部分证据，不代表 M5c、M5d 或 M5 总门禁关闭。

## High 720p/1080p长序列正确性

生产基线 `0934b08`（测量器为同期延迟输出版本，后以`cd9f13e`提交）。
FFmpeg testsrc2、300帧、30FPS、High/CABAC、QP26、GOP30/IDR60、B2、4 slice、
WithTransform8x8、WithWeightedPrediction、WithLookahead(2)。每次Receive均按PTS比较
本地重建与Go Decoder；Flush后全部300帧按显示顺序输出。FFmpeg 8.0以`-v warning
-xerror -fps_mode passthrough`独立解码，无warning/error，完整YUV哈希如下。

| 尺寸 | 输入SHA-256 | Annex-B SHA-256 | 重建=Go Decoder=FFmpeg SHA-256 | 字节 |
|---|---|---|---|---:|
| 1280×720 | `dc1c14f1d0063c2743152dd81eef293a2b668a9cf00c29817104cf07994614e9` | `b81e9e4099766899343aa89cacf7232ad6e8d1379dc66d5deca2cbd6b6a4c49e` | `30baf8136db34226f319c50e0c43e6c16d8abb8408df3f3e9fd64e88e322fa33` | 9,372,355 |
| 1920×1080 | `a252864539b1817e9a3886014a55ac085739abcf31baf49269616fe79bc65336` | `01927c849e9105065b6c65b021ea690f1472d11333275688052af8710018ac1a` | `36f3a5fe6c830f16c21471a551cc5936b7100944386f525187603374c8e50a4f` | 17,263,009 |

本地命令为`encodebench -profile high -bframes 2 -gop 30 -idr 60 -qp 26
-transform8 -weighted -lookahead 2 -slices 4 -frames 300 -verify -input <raw.yuv>
-output <stream.264> -recon <local.yuv>`，分别设置`-width/-height`。
与其他验证并行，时间数字不用于性能承诺。可复用门禁脚本为`scripts/verify_m5.ps1`。
该结果证明上述配置的正确性，不替代ABR/CBR/VBV长窗口或等QP/等码率质量门禁。

## 已验证

| 范围 | 证据 |
|---|---|
| CABAC Main/High I/P 编码 | `tests/codec/cabac_test.go` 覆盖 12 组配置、288 帧：QP 0/26/51、1/3 slice、宏块 QP 变化、帧号回绕与场景切换；FFmpeg、自研 Decoder、本地重建逐像素一致 |
| CABAC PCM | `TestEncoder_CABACPCM_RoundTrip`，输入与解码输出逐像素一致 |
| B与重排 | Main/High、B深度1/2/3/16、80帧/组共14组1120帧三方Oracle；B2组合ABR/CBR/VBV、High8×8与P权重。实际slice为非参考spatial B；B_Skip/B_Direct残差实际选中。规则PTS/Duration、DTS递增、强制IDR/GOP/尾部Flush、失败Send/延迟Receive重试、时间戳溢出和清理有测试；B+Lookahead闪光/切换边界另有Oracle |
| P 显式权重 | `tests/codec/weighted_prediction_test.go` Main/High 各4组共160帧三方 Oracle：三平面不同 weight/offset、负/零权重、裁剪、denom=7 隐含128、自动分析、3 slice、34×18 crop、非 IDR I/IDR/帧号回绕；另有实际加权 Skip、插值/裁剪顺序与失败事务单测 |
| High 8×8 与矩阵闭环 | `TestEncoder_Transform8x8Oracle` QP0/18/26/51×1/3slice×flat/custom共320帧三方Oracle；规范逆基定点投影有常量/脉冲/非DC解析基；I/P实际选中8×8；PPS矩阵扫描/delta与4×4 flat不变量有测试 |
| GOP / IDR | `TestEncoder_IndependentGOPAndSceneChangeIDR` 断言场景切换与 IDR 间隔；ffprobe 断言 GOPSize 产生非 IDR I picture，且剩余帧数正确 |
| ABR | `TestEncoder_ABRLongSequenceAccuracy`：32×32、120 帧，实测平均码率在目标 ±10% 内，FFmpeg 与本地重建逐像素一致 |
| CBR feedback/filler | `TestEncoder_CBRWindowRate`：两个30帧窗口±10%；`StaticFillerOracle`静态CBR/VBV各60帧，30000/1001 FPS码率≤1%，filler字节与资源门禁解析解，均有FFmpeg像素Oracle；未宣称HRD信令 |
| VBV | 32×32、60 帧连续成功序列逐帧 fullness 在配置范围内，FFmpeg 像素一致；上/下边界和失败事务另有测试 |
| 720p 性能 smoke | HW-M2 单次 ABR/CBR 数值与 CPU profile top 10 见 `docs/performance.md` §10；非统计稳健基准，不作性能承诺 |

## 未完成与门禁状态

- B非参考spatial-direct零MV双参考策略已接入；B temporal-direct、B运动搜索与B-pyramid未生成，当前不作完整B宏块模式承诺。
- P 单参考显式权重编码已验证；B 显式/隐式权重编码待 B slice 接入。
- High Intra8/P8×8、六个4×4列表/DC首项和8×8 intra/inter自定义矩阵已接入CABAC闭环；320帧组合和126帧4×4/DC+B三方Oracle已通过。
- CABAC 整帧率估算仍借用 CAVLC 候选代理；zero-word 已实施逐slice bin密度约束，计入PCM restart前的bin数，并在封装后进入AU资源门禁。解析阈值与5000个MPS密集流读回测试通过。
- WithLookahead(1..16)已实现有界深拷贝、未来窗口确认切换/抑制闪光、PTS/DTS、背压、失败事务与尾部Flush排空；与B调度组合避免跨场景选未来锚点。
- VBV大尺寸长序列/突变压力证据待完成；filler NAL与分数FPS余数已实现，HRD参数信令仍属后续扩展。
- 720p/1080p各300帧High/B2/8×8/P权重长Oracle已通过；QP18/26/34同参数x264、等码率报告、大尺寸码控窗口、正式基准仍待执行结果，不能因短序列或已启动后台任务自动算通过。

常规门禁（CGO=0）gofmt/vet/build/test、depscheck和manifest27条通过；非缓存`go test -count=1 ./...`也通过。`CGO_ENABLED=1 go test -race -timeout 30m ./...`全包通过（该次codec591.553秒；4×4/DC后续改动另跑相关包race）；Windows/Linux/macOS×amd64/arm64六目标CGO=0构建通过。High两尺寸各300帧Oracle已通过；同参数x264、大尺寸码控、最新相关包race仍以完成通知和记录为准。

4×4/DC改动后相关包`encoder/internal/transform/tests/codec/tools/encodebench`的race通过，
codec487.741秒；B调度fuzz10秒11次无失败，只是smoke，不替代长时覆盖。
最新测量器加入CPU profile、实际帧型、QP序列、每AU比特与30帧窗口码率、构建revision/
dirty状态和`DecoderVerified`标记；重建YUV按PTS顺序输出并检查短写。

因此 Roadmap M5 仍为 WIP；不创建 `m5c-gate`/`m5d-gate`/`v0.5.3`/`v0.6.0` 收尾提交。
