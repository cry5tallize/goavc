# M5a：单参考 Inter 本地门禁

验证日期：2026-11-14，Go 1.27.4 windows/amd64、FFmpeg/ffprobe 8.0、HW-M2。
生产代码基线：`57d53b3b67effb72ef416e5a0af5e95d6b76af31`；补充参考 fuzz 与
帧类型测试提交：`3ef6e4a`。这是 M5a 子阶段证据，不是 M5 整体退出记录。

## 范围与证据

| 要求 | 验证 |
|---|---|
| 标量 SAD、full/diamond、边界与自适应范围 | `internal/motion/search/search_test.go`：非对齐 stride、奇数块、极值、已知平移、full 与 diamond 定向差距；不宣称菱形全局最优 |
| 整/半/四像素运动补偿 | `TestSearch_TranslationAndSubpixel`；搜索与重建调用 decoder 使用的同一份 MC；没有重复六抽头或色度插值实现 |
| P16×16 / 16×8 / 8×16 / 8×8 | `TestEncoder_InterPartitionCoverageAndSkip` 构造四种不同移动分区，检查最终实际选择 |
| MVP / MVD / ref_idx | 共用 `motion.Grid` 与规范 P 预测；单参考 ref_idx=0 按语法省略；逐分区预测、写差值并更新可用性 |
| P_Skip / 早期终止 | 规范 Skip MV 的三平面量化残差均零时提前选择，写 mb_skip_run；静态重建输入断言全部 Skip 且 AU 小于 100 字节 |
| 连续 P、残差与滤波参考 | 9 组 QP/slice ×40 帧共 360 帧，含 0/26/51、slice=1/3/5、frame_num 16 回绕、场景变化、显式 KeyFrame；本地重建=自研 Decoder=FFmpeg |
| 实际帧类型 | `TestEncoder_InterPictureTypes` 由独立 ffprobe 检查 I/P/P/I/P 和关键帧标记；序列测试另检查 NAL 类型及 PTS/DTS |
| 720p/1080p 长序列 | 各 300 帧 testsrc2、30 FPS、QP26、GOP30、四 slice；本地/自解码逐帧相等，FFmpeg 全序列 SHA-256 相等，stderr 无 warning/error，Flush→EOF |
| 初始化与资源边界 | unsupported profile/格式/码控/线程在 New 拒绝；LevelAuto 超出最坏预算时检查实际 AU，超限不发布输出、不更新参考或帧号 |
| 基准 | `BenchmarkEncoderP_720p_Generic`、`tools/encodebench -gop 30`，见 performance.md §8；记录改动前后输入、时间、码率与 PSNR |

## 长门禁复现

```powershell
$env:CGO_ENABLED = '0'
$env:H264_LONG_TEST = '1'
go test -run '^TestEncoder_InterLongOracle$' -count=1 -v -timeout 30m ./tests/codec
```

常规测试明确跳过长门禁；本记录来自实际显式执行，不能用 skip 替代通过。
输入由 FFmpeg 现场生成，逐帧读取，不保留 1 GiB 原始输入；输出 Oracle 同样流式计算哈希。

| 尺寸/帧数 | 输入 SHA-256 | Annex-B SHA-256 | 本地重建与 FFmpeg YUV SHA-256 | 字节数 |
|---|---|---|---|---|
| 1280×720 /300 | `dc1c14f1d0063c2743152dd81eef293a2b668a9cf00c29817104cf07994614e9` | `8a546c833fb1757797694608b89d7afa54339fe4913bf5a27f88098087190546` | `8cf3634de4b5ab8c816de5f20b08b2ec19baf78cd572e371099d6fb0221f2e91` | 7,095,020 |
| 1920×1080 /300 | `a252864539b1817e9a3886014a55ac085739abcf31baf49269616fe79bc65336` | `d6e3d6ac65f6c64858cfb0e055359e22c43e1c03cef066717e0bec4927a18d15` | `a0593c53771710ea6b1ad95c54b1cdce9cda8808fe7389f3debe6962948a25e6` | 15,785,935 |

这两次长测试的编码均值为 9.0270 / 4.3240 FPS，同时运行了其他验证；数字只供诊断，
不能与独占环境 benchmark 做等价性能比较。码率按 10 秒分别为 5,676.016 / 12,628.748 kbit/s。

## 通用检查

- 普通 `CGO_ENABLED=0` 的 gofmt、vet、build、`go test -count=1 ./...`、depscheck（29 包）、
  manifest（27 条）、diff 检查全部通过。M3 的 217 帧语料哈希回归仍通过。
- `CGO_ENABLED=1 go test -race -count=1 ./...` 全包通过，使用开发期 GCC 工具链，运行时没有 C 导入。
- 旧帧内 fuzz 5 秒：38,685 次通过；新增三帧参考/宏块 QP fuzz 10 秒：8,775 次通过。
- Windows/Linux/macOS × amd64/arm64 六种 CGO=0 build 通过；只证明编译。
- 本地标签 `v0.5.1`；远端 CI、其余目标原生测试未执行。

## 当前缺口

M5b 的 SATD/RD、Intra/Inter 全比较与自适应 QP 尚未完成；M5c 的 CABAC/Main/High/B
编码和 M5d 的 ABR/CBR/VBV/独立 GOP/IDR 间隔尚未完成。等 QP 1.5 dB 的 x264 质量门禁、
等码率报告及 CPU profile 仍待执行。不能据此宣称 M5 或完整 Baseline 编码支持已完成。

默认 GOPSize=30 现在激活 I/P，GOPSize=1 保留 M4 全帧内；当前要求 GOPSize=IDRInterval。
WithPCM 仍为全 IDR 诊断通道。LevelAuto 支持实际 AU 预算，显式 Level 和 PCM 仍较保守。
