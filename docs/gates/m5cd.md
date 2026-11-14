# M5c/M5d 本地进展记录

日期：2026-11-14；Go 1.27.4 windows/amd64；常规验证 `CGO_ENABLED=0`。
当前分支 `feat/m5c-main-high`。本文件记录部分证据，不代表 M5c、M5d 或 M5 总门禁关闭。

## 已验证

| 范围 | 证据 |
|---|---|
| CABAC Main/High I/P 编码 | `tests/codec/cabac_test.go` 覆盖 12 组配置、288 帧：QP 0/26/51、1/3 slice、宏块 QP 变化、帧号回绕与场景切换；FFmpeg、自研 Decoder、本地重建逐像素一致 |
| CABAC PCM | `TestEncoder_CABACPCM_RoundTrip`，输入与解码输出逐像素一致 |
| 8×8 正向变换/量化内核 | `internal/transform/eight_test.go` 常量 DC 与单位脉冲解析向量、QP 0..51 flat/scaling parity、逆量化符号和溢出边界；尚未接入宏块/码流 |
| GOP / IDR | `TestEncoder_IndependentGOPAndSceneChangeIDR` 断言场景切换与 IDR 间隔；ffprobe 断言 GOPSize 产生非 IDR I picture，且剩余帧数正确 |
| ABR | `TestEncoder_ABRLongSequenceAccuracy`：32×32、120 帧，实测平均码率在目标 ±10% 内，FFmpeg 与本地重建逐像素一致 |
| CBR feedback | `TestEncoder_CBRWindowRate`：32×32、两个 30 帧窗口均在目标 ±10% 内；FFmpeg 与本地重建逐像素一致。无 filler NAL，不宣称恒定传输 |
| VBV | 32×32、60 帧连续成功序列逐帧 fullness 在配置范围内，FFmpeg 像素一致；上/下边界和失败事务另有测试 |
| 720p 性能 smoke | HW-M2 单次 ABR/CBR 数值与 CPU profile top 10 见 `docs/performance.md` §10；非统计稳健基准，不作性能承诺 |

## 未完成与门禁状态

- B slice 编码、B Direct、参考列表、IBBP 放置、DTS/POC 重排均未实现；`MaxBFrames>0` 在 New 阶段明确返回 `ErrUnsupported`。
- Weighted Prediction 编码未实现。
- 8×8 正变换及缩放量化尚未接入 Intra8/P 宏块决策、残差扫描和码流；High 编码仍只覆盖 CABAC 4×4 I/P。
- CABAC 整帧率估算仍借用 CAVLC 候选代理，通用 CABAC zero-word 最坏封装预算未完成。
- Lookahead 未实现；场景检测为当前帧相对参考帧的全平面平均绝对差阈值。
- VBV 长序列/溢出压力语料、HRD 参数与 filler NAL 未实现。
- M5 规定的 720p/1080p 各 300 帧完整参数门禁、QP 18/26/34 与 x264 对比、等码率 PSNR/SSIM 报告、全量 CPU profile、竞态检测与六目标交叉构建未因本次局部结果而自动通过。

最近一次常规本地门禁（CGO=0）：`gofmt -l .`、`go vet ./...`、`go build ./...`、`go test ./...`、`go run ./tools/depscheck`、manifest 校验及 `git diff --check` 全通过。`CGO_ENABLED=1 go test -race -timeout 20m ./...` 全包通过；Windows/Linux/macOS × amd64/arm64 六目标 CGO=0 交叉构建通过。完整 M5 长序列与 x264 质量门禁本次未执行。

因此 Roadmap M5 仍为 WIP；不创建 `m5c-gate`/`m5d-gate`/`v0.5.3`/`v0.6.0` 收尾提交。
