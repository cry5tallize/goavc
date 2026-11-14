# M5c/M5d 本地验收与历史记录

日期：2026-11-14；Go 1.27.4 windows/amd64；常规验证 `CGO_ENABLED=0`。
当前M5c/d本地门禁证据已全部满足：六项固定QP质量和六项码控长测各300帧，
共3600帧三方Oracle；原1.5dB阈值全部通过，ABR偏差均<1%，CBR窗口达标、VBV不越界。
最终范围与工程检查分别见[M5c](./m5c.md)、[M5d](./m5d.md)，数值/哈希见
[完整记录](./m5cd_results.json)和[性能报告§14](../performance.md)。
完整Profile仍Partial，等码率效率和标量吞吐仍需改进；仓库未配置远端，远端CI未执行。

以下内容保留收尾前的历史快照。其中“待完成”“未通过”“不创建标签”均是当时结论，
不覆盖上述最新验收；历史失败项没有删除，也没有用旧构建冒充新量化/码控证据。

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
- P 单参考显式权重编码已验证；B slice 已接入，但B显式/隐式权重编码尚未生成。
- High Intra8/P8×8、六个4×4列表/DC首项和8×8 intra/inter自定义矩阵已接入CABAC闭环；320帧组合和126帧4×4/DC+B三方Oracle已通过。
- CABAC 整帧率估算仍借用 CAVLC 候选代理；zero-word 已实施逐slice bin密度约束，计入PCM restart前的bin数，并在封装后进入AU资源门禁。解析阈值与5000个MPS密集流读回测试通过。
- WithLookahead(1..16)已实现有界深拷贝、未来窗口确认切换/抑制闪光、PTS/DTS、背压、失败事务与尾部Flush排空；与B调度组合避免跨场景选未来锚点。
- VBV大尺寸长序列/突变压力证据待完成；filler NAL与分数FPS余数已实现，HRD参数信令仍属后续扩展。
- 720p/1080p各300帧High/B2/8×8/P权重长Oracle已通过；历史基线QP18/26/34六组同参数x264与等码率报告已齐，五过一失败；后续量化改动的新质量矩阵与大尺寸码控证据需单独验收。

720p同参数质量矩阵已完成，见`performance.md`§12：QP18/34绝对Y差0.983346/
0.878151dB通过，QP26差1.542604dB未通过（Go更高PSNR但协议为绝对差）；
不降低画质或放宽阈值。实测等码率匹配误差均<1%，Go Y-PSNR低于x264约
7.09..8.79dB，完整公开。1080p历史基线矩阵现已完成：QP18/26/34绝对Y差
1.078893/1.4999924216020588/1.021472dB均通过；按未舍入值判定QP26。
输入/码流/重建哈希与完整300帧长度已现场核对，详见`performance.md`§12。
1080p等码率补测匹配误差−0.460001%/+0.807906%/+0.843907%，Go Y-PSNR
低于x264约4.66..5.74dB。六组总体仍因720p QP26失败而未通过。
本任务运行旧脚本，正常退出并不表示质量通过；产物也不包含后续预测量化改动，
不能用作当前HEAD的新质量验收。大尺寸码控另按其任务证据核验。

常规门禁（CGO=0）gofmt/vet/build/test、depscheck和manifest27条通过；非缓存`go test -count=1 ./...`也通过。`CGO_ENABLED=1 go test -race -timeout 30m ./...`全包通过，该次codec500.406秒，包含4×4/DC矩阵、B队列、filler、延迟测量及其回归；部分未改包命中Go测试缓存，不描述为全部非缓存执行。该次启动于新增High剪枝对照测试之前，不将其算作该新增测试或后续量化提交的race证据。Windows/Linux/macOS×amd64/arm64六目标CGO=0构建通过。High两尺寸各300帧Oracle已通过；历史质量结果现已补齐，后续量化版本质量和大尺寸码控按独立记录核验。

4×4/DC改动后相关包`encoder/internal/transform/tests/codec/tools/encodebench`的race通过，
codec487.741秒；B调度fuzz先跑10秒11次，后续30秒4700次、发现16个新覆盖输入，均无失败；仍只是smoke，不替代长时覆盖。
最新测量器加入CPU profile、实际帧型、QP序列、每AU比特与30帧窗口码率、构建revision/
dirty状态和`DecoderVerified`标记；重建YUV按PTS顺序输出并检查短写。

该历史快照当时结论为Roadmap M5仍WIP，不创建`v0.5.3`/`v0.6.0`收尾提交；
后续完成情况以本页顶部和独立M5c/d门禁页为准。

## 重新核验记录（2026-11-14 21:35 +08:00）

核验基线为`fcfa674`，初始工作区干净。本轮没有更改编码实现或放宽门禁。

- 当前HEAD重新执行CGO=0的gofmt/vet/build、非缓存全包测试（codec39.484秒）、
  depscheck29包、manifest27条及diff检查，全部通过。
- 单独补跑此前完整race记录未覆盖的新增`TestModeDecision_HighToolsMatchUnpruned`：
  `CGO_ENABLED=1 go test -race -run '^TestModeDecision_HighToolsMatchUnpruned$' -count=1 ./encoder`
  通过（3.940秒）。这不描述为本轮重新执行了完整race。
- 重新读取两份High长Oracle报告，并对原始输入、Annex-B、重建和FFmpeg原始输出
  现场重算SHA-256，全部与上表/报告相等。帧数均为300，重建长度分别为
  414,720,000与933,120,000字节。这是既有配置和生产基线的产物复核，未重新编码长序列。
- 原始`m5-formal-quality`产物确认720p QP18/26/34结果与§12一致，QP26为明确失败。
  1080p QP18另有已完成报告：Go Y=56.454788、x264 Y=55.375895，绝对差1.078893dB，
  该单项通过；当前1080p QP26仍在运行，不能将空JSON报告算作完成。
- `m5-formal-rate`中720p ABR已有300帧报告：目标2,000,000bit/s，实测1,997,916bit/s，
  偏差−0.1042%；码流和重建文件哈希与报告一致。后台测量器build info为
  `f060c4270540801d6deba1835289dae83001be50`且`vcs.modified=true`，报告没有
  `FrameBits`与`DecoderVerified`字段，独立FFmpeg原始输出也未在该结果目录出现。
  因此这里只记录初步码率结果，不升级为当前HEAD的大尺寸完整码控门禁通过。
  同一任务的720p CBR仍在运行；VBV及1080p完整码控结果未齐。
- 代码确认B策略为双锚点、非参考、零MV spatial-direct/skip；不将未实现的
  temporal-direct、B运动搜索或B-pyramid误写为已有能力。M5c要求的一种可用B策略已有证据。

后台目录为`%LOCALAPPDATA%/Temp/opencode/m5-formal-quality`与`m5-formal-rate`。
最终应使用当前测量器统一核验/补跑，保留现有长任务结果和失败项；后台进程存在、文件存在
或旧报告产生均不等于当前HEAD验收完成。仍无`v0.5.3`与`v0.6.0`标签。

## 最终核验（2026-11-14）

- CQP长测clean构建`eec92c7`，RC长测clean构建`fbcaad2`；后续RC改动不进入CQP提前返回路径，最终版本另有30帧逐比特等价及Go Decoder检查。
- 六项绝对Y差为720p的0.916513/1.400772/0.048145和1080p的1.037103/1.411639/0.112997dB，均≤原1.5dB；全部实测匹配码率误差≤1%，PSNR/SSIM及效率差距完整公开。
- ABR实测2.0095072/3.9692312Mbps，偏差+0.475360%/−0.769220%；CBR/VBV平均恰为2/4Mbps，十个30帧窗口均达目标。VBV逐AU独立重算，与全部300步轨迹相等且不越界。
- 全部12份输入、码流、重建与独立FFmpeg文件重新计算SHA-256；ffprobe确认每组完整300帧、High、正确尺寸、IDR5/P100/B195及关键帧位置。h264probe另确认每图4slice、B深度2、IDR60与完整POC显示映射。
- 最新常规门禁通过：非缓存全包test（codec30.680秒）、depscheck30包、manifest27条；编码实现完整非缓存race（codec681.840秒）与随后新增h264probe包的独立race（1.243秒）通过；最新六目标CGO=0构建通过。
- 120帧High/B2/VBV突变/闪光三方压力通过；52起点×53边界的QP夹紧、失败事务和Reset测试通过。B CQP fuzz60秒3015次无失败，保持smoke范围，不冒充RC fuzz。
- 停止的早期码控任务、旧版历史报告与未完成`.pending`均未纳入验收；本轮启动的最终质量/码控任务已结束。既有外部任务与其产物保持原样。
