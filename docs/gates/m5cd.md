# M5c/M5d 本地进展记录

日期：2026-11-14；Go 1.27.4 windows/amd64；常规验证 `CGO_ENABLED=0`。
当前分支 `feat/m5c-main-high`。本文件记录部分证据，不代表 M5c、M5d 或 M5 总门禁关闭。

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
| CBR feedback | `TestEncoder_CBRWindowRate`：32×32、两个 30 帧窗口均在目标 ±10% 内；FFmpeg 与本地重建逐像素一致。无 filler NAL，不宣称恒定传输 |
| VBV | 32×32、60 帧连续成功序列逐帧 fullness 在配置范围内，FFmpeg 像素一致；上/下边界和失败事务另有测试 |
| 720p 性能 smoke | HW-M2 单次 ABR/CBR 数值与 CPU profile top 10 见 `docs/performance.md` §10；非统计稳健基准，不作性能承诺 |

## 未完成与门禁状态

- B非参考spatial-direct零MV双参考策略已接入；B temporal-direct、B运动搜索与B-pyramid未生成，当前不作完整B宏块模式承诺。
- P 单参考显式权重编码已验证；B 显式/隐式权重编码待 B slice 接入。
- High Intra8/P 8×8与自定义8×8矩阵已接入CABAC闭环；自定义4×4/DC矩阵编码仍未接入，现有路径保持flat=16。
- CABAC 整帧率估算仍借用 CAVLC 候选代理；zero-word 已实施逐slice bin密度约束，计入PCM restart前的bin数，并在封装后进入AU资源门禁。解析阈值与5000个MPS密集流读回测试通过。
- WithLookahead(1..16)已实现有界深拷贝、未来窗口确认切换/抑制闪光、PTS/DTS、背压、失败事务与尾部Flush排空；与B调度组合避免跨场景选未来锚点。
- VBV 长序列/溢出压力语料、HRD 参数与 filler NAL 未实现。
- M5 规定的 720p/1080p 各 300 帧完整参数门禁、QP 18/26/34 与 x264 对比、等码率 PSNR/SSIM 报告、全量 CPU profile、竞态检测与六目标交叉构建未因本次局部结果而自动通过。

最近一次常规本地门禁（CGO=0）：`gofmt -l .`、`go vet ./...`、`go build ./...`、`go test ./...`、`go run ./tools/depscheck`、manifest 校验及 `git diff --check` 全通过。`CGO_ENABLED=1 go test -race -timeout 20m ./...` 全包通过；Windows/Linux/macOS × amd64/arm64 六目标 CGO=0 交叉构建通过。完整 M5 长序列与 x264 质量门禁本次未执行。

因此 Roadmap M5 仍为 WIP；不创建 `m5c-gate`/`m5d-gate`/`v0.5.3`/`v0.6.0` 收尾提交。
