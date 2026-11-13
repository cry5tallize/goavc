# 性能基准

> 本文件只记录**可复现**的数字。任何一条结果缺少硬件、commit、参数或输入哈希，
> 都视为无效记录，不得用于对外宣称。

## 1. 基准硬件登记

每次跑基准前先填这张表（同一份结果只在同一硬件行下可比）。

| 编号 | CPU | 核心/线程 | 内存 | OS | Go | 备注 |
|---|---|---|---|---|---|---|
| HW-1 | _待填_ | _待填_ | _待填_ | _待填_ | _待填_ | 主力开发机 |
| HW-M2 | AMD Ryzen 7 5700X | 8 核 / 16 线程 | OS 可见 31.89 GiB | Windows 11 IoT Enterprise Insider 10.0.26200 amd64 | Go 1.27.4 | 2026-11-14 本地标量基线 |

## 2. 测量协议

每条结果必须附以下字段，缺一不可：

```text
硬件编号   HW-1
commit     <40 位 hash>
Go 版本    go1.27.4 <os/arch>（后续使用最新 1.27 补丁版）
构建参数   CGO_ENABLED=0，默认 GOFLAGS
输入语料   <manifest name>  sha256=<...>
分辨率     1920x1080
Profile    high
熵编码     cabac
参数       QP=26 或 bitrate=4M，GOP=30，B=2
线程数     1 / N
SIMD       generic | sse2 | avx2 | neon
命令       go test -run '^$' -bench <name> -benchmem -count 5
```

报告指标：

| 指标 | 说明 |
|---|---|
| FPS | 端到端吞吐，取 5 次 `-count` 的中位数 |
| 平均帧延迟 | 单帧 `Send+Receive` 的耗时均值 |
| P95 帧延迟 | 抖动指标，实时场景更关键 |
| CPU 使用率 | 单线程/多线程分别记录 |
| 峰值内存 | `runtime.MemStats.HeapSys` 峰值 |
| alloc/op、B/op | 热路径分配预算 |
| 码率 | kbps，ABR/CBR 需同时记录目标值与实测值 |
| PSNR / SSIM | 与原始 YUV 对比，Y/U/V 分平面报告 |

## 3. 项目目标（暂定，非承诺）

| 场景 | 初始目标 | 状态 |
|---|---|---|
| 1080p 8-bit YUV420 解码 | M8 优化后争取 30 FPS，尚未承诺 | M2 实测中位数 21.54 FPS，见 §5 |
| 720p 8-bit YUV420 编码 | 30 FPS | 未测量 |
| 1080p 8-bit YUV420 编码 | v1.x 优化目标 | 未测量 |
| 连续流式处理 | 内存无持续增长 | 未测量 |
| 核心 kernel | 零堆分配 | 未测量 |

> 目标在 M2（首次有解码闭环）与 M5（首次有完整编码器）结束后各复审一次：
> 要么改成"实测 + 承诺"，要么下调并说明原因。不允许长期挂着未验证的目标。

## 4. M0 基线

M0 阶段只有基础设施级别的基准（`benchmarks/frame_test.go`），用于建立
"帧校验/深拷贝/释放"的成本基线，作为 M8 缓冲池优化的对照。

复现命令：

```bash
CGO_ENABLED=0 go test -run '^$' -bench . -benchmem -count 5 ./benchmarks
```

| 基准 | 结果（HW-1） | 状态 |
|---|---|---|
| `BenchmarkFrameValidate/1920x1080` | _待测_ | 未测量 |
| `BenchmarkFrameClone/1920x1080` | _待测_ | 未测量 |
| `BenchmarkFrameRelease` | _待测_ | 未测量 |

> M0 公共类型基准尚未登记；本次 M2 只测量以下真实解码基线。

## 5. 解码基准

| 分辨率 | Profile | 熵编码 | SIMD | FPS | P95 延迟 | alloc/op | commit |
|---|---|---|---|---|---|---|---|
| 1920×1080 | Baseline | CAVLC | generic | 21.54 | 未测 | 51,470 / 三帧序列 | `542b9db` |

### M2 首次记录（2026-11-14）

- 硬件：HW-M2；完整代码 commit：`542b9dbc48ffe7e6fa6453818aa60c2015c48779`。
- Go 1.27.4 windows/amd64；`CGO_ENABLED=0`、默认 GOFLAGS、GOMAXPROCS=16；解码实例内部为单线程，无 SIMD。
- 输入：`baseline_1920x1080_yuv420p_cavlc_ip_005`；SHA-256：`14d2afa7048b4ca21e5f44c3206a55b9b195fc11cea3bff65e1c93104e23cc5a`。
- 编码参数：x264 veryfast、QP=26、GOP=30、B=0、ref=1、weightp=0、scenecut=0、threads=1、AUD=1；生成命令见 `testdata/README.md`。
- 一次 op 包含 New、三帧 I/P 解码、Flush、Receive/Release 和 Close；文件读取不计时。该数据是短序列端到端基线，不能代表长期稳定流或所有画面复杂度。
- 命令：`go test ./decoder -run '^$' -bench BenchmarkDecoder_1080p_Generic -benchmem -count 5`（预先设置 `CGO_ENABLED=0`）。

| 次数 | ns/op（三帧） | FPS | B/op | allocs/op |
|---|---:|---:|---:|---:|
| 1 | 134,537,088 | 22.30 | 24,140,805 | 51,471 |
| 2 | 140,015,860 | 21.43 | 24,140,163 | 51,470 |
| 3 | 125,734,062 | 23.86 | 24,140,152 | 51,470 |
| 4 | 140,379,062 | 21.37 | 24,140,152 | 51,470 |
| 5 | 139,291,025 | 21.54 | 24,140,166 | 51,470 |

中位数 139,291,025 ns/序列，约 46.43 ms/帧、21.54 FPS；每帧平均约
8.05 MB 分配、17,157 次分配。P95、峰值堆内存、CPU 利用率与长时流尚未测量。
M8 首先针对每帧上下文和帧内预测临时分配、逐样本亚像素 MC 建立 profile，
在保持 Oracle 一致性的前提下复审 30 FPS 目标。

## 6. M4 帧内编码基线

2026-11-14，HW-M2，代码/工具 commit：`aff921bca98ba77db7bd36f3937f3e15992460c8`。
Go 1.27.4 windows/amd64、CGO_ENABLED=0、默认 GOFLAGS/GOMAXPROCS=16，实例内部
单线程，generic 标量；无 SIMD。IDR 每帧、B=0、QP=26、CQP、单 slice、LevelAuto。
Level 根据最坏 PCM/EBSP 预算选择，当前 720p@30 与 1080p@15 都为 Level 6.2；
不能据此声称 1080p@30 已支持。GOPSize 不参与 M4 决策。

质量语料为 `internal/testutil/pattern.Frame(1280,720,index)` 的 index=0..5，
共六帧噪声、二维斜坡、水平/垂直斜坡、对角和分块图案，按 30 FPS 计算码率。
它是合成基线，不代表自然视频或 M5 质量目标。完整输入 SHA-256：
`dd292d5f102f2956bd27e2b5a1f9ea627a09342fa27ac0d729bb3d71221a589d`。

```powershell
$env:CGO_ENABLED = '0'
go run ./tools/encodebench -width 1280 -height 720 -frames 6 -qp 26
go run ./tools/encodebench -width 1280 -height 720 -frames 6 -qp 26 -pcm
go test -run '^$' -bench . -benchmem -benchtime=300ms -count=5 ./encoder
```

六帧混合图案实测（计时仅 SendFrame+ReceivePacket，不计图案生成、重建快照、质量统计与 I/O）：

| 通道 | 字节数 | 码率 kbit/s | PSNR Y/U/V dB | FPS | 平均 ms/帧 | P95 ms/帧 | HeapSys 采样最大字节 |
|---|---|---|---|---|---|---|---|
| I4/I16 + CAVLC | 1,222,486 | 48,899.44 | 41.1507 / 41.0844 / 41.0850 | 13.7144 | 72.9161 | 117.6486 | 20,709,376 |
| 强制 I_PCM | 8,337,847 | 333,513.88 | +Inf / +Inf / +Inf | 90.1674 | 11.0905 | 13.1590 | 37,519,360 |

两份输出都经 FFmpeg 独立解码（`-v warning -xerror -probesize 32M`），无 warning/error，
YUV 哈希与本地重建严格相等。PCM 文件超过默认 5 MiB 探测预算，所以显式增大探测范围。

| 通道 | Annex-B SHA-256 | 重建/FFmpeg YUV SHA-256 |
|---|---|---|
| Intra | `e8312c684fb42cc709799b49bd7bde51237d8d09caeca8ff383444b8aad1ca56` | `78f1750bcdf855196db0b39036ebec8ef8896fffb239b1d06a7cc96369527472` |
| PCM | `937129138d3dd92c1a491795b404212d7407391c7ac4c02a29431e91904ce687` | `dd292d5f102f2956bd27e2b5a1f9ea627a09342fa27ac0d729bb3d71221a589d` |

Go benchmark 使用固定 index=1 的二维斜坡单帧，包含输入边缘复制、编码和取包，
不包含重建快照和图案生成。以下为五次测量中位数；同一行的 B/op、alloc/op 也各取中位数。

| 基准 | ns/op | FPS | B/op | alloc/op | 输入单帧 SHA-256 |
|---|---|---|---|---|---|
| Encoder_720p_Generic（配置 30 FPS） | 70,918,920 | 14.1006 | 6,994,448 | 121,058 | `b7fc8e5048a11a8a56e17f10cdc1073c840b8e1850e79f0fce9200b23ca22590` |
| Encoder_1080p_Generic（配置 15 FPS） | 156,516,100 | 6.3891 | 15,195,604 | 273,079 | `dd6adffe8c40e27ea0e7279247643839368ba853543d57b699bf23a96e5b9cfc` |
| EncoderPCM_720p_Generic（配置 30 FPS） | 10,000,464 | 99.9954 | 13,876,536 | 55 | 同 720p 单帧 |

五次耗时原始数据（ns/op）：

```text
720p Intra: 70918920 73671260 68483800 71173340 68412520
1080p Intra: 150019100 159657300 151388150 156516100 170226650
720p PCM: 9969374 9990270 10054416 10000464 10241385
```

HeapSys 是工具进程每帧后的堆保留量采样，包含保留码流和质量统计，不等同 RSS 或精确峰值。
本次未测 CPU 利用率、SSIM、自然视频、x264 等 QP/码率差异或长期内存稳定性。
M4 没有优化前后加速声明：这是首次标量编码基线；30 FPS 目标仍未达到，不能算性能门禁通过。
M5 引入帧间压缩，M8 再处理邻域分配与热路径优化。

## 7. 内核对比

| Kernel | Generic | SSE2 | AVX2 | NEON | 说明 |
|---|---|---|---|---|---|
| _待 M8_ | | | | | |

对比原则：同一输入、同一输出约束、同一编译参数；**速度与画质必须同时报告**，
不允许只挑对自己有利的一项。

## 8. M5a 单参考 P 编码基线

2026-11-14，HW-M2；Go 1.27.4 windows/amd64、CGO=0、默认 GOFLAGS/GOMAXPROCS=16，
编码实例单线程 generic。固定 720p 图案 index=1，输入 SHA-256
`b7fc8e5048a11a8a56e17f10cdc1073c840b8e1850e79f0fce9200b23ca22590`，
Baseline/CAVLC、QP26、GOP600、B0、单参考、单 slice、搜索半径16。
基准先编码一张参考图，再计时首个 P 帧；不含图案生成/重建快照。

```powershell
$env:CGO_ENABLED = '0'
go test -run '^$' -bench '^BenchmarkEncoderP_' -benchmem -benchtime=1x -count=5 ./encoder
go run ./tools/encodebench -width 1280 -height 720 -frames 6 -qp 26 -gop 30
```

| 策略 | 代码基线 | ns/op 中位数 | FPS | B/op 中位数 | alloc/op |
|---|---|---|---|---|---|
| 每个候选分区都做亚像素细化 | `930e2c86d6d6a81cc857e582d1a7d54d17566c95` | 1,954,924,200（三次） | 0.5115 | 4,449,792 | 5,842 |
| 整像素分区选择后细化 + 早期 Skip | `57d53b3b67effb72ef416e5a0af5e95d6b76af31` | 425,999,900（五次） | 2.3474 | 4,449,520 | 5,814 |

原始 ns/op：改前 `1826893300 1957166300 1954924200`；改后
`380557500 383879900 470494200 432055400 425999900`。
旧版本使用相同基准函数在同一环境测量。改动会改变分区和码流，是编码启发式调整，
不能声称输出逐比特等价的纯性能优化。双方仍须分别满足独立解码正确性。

同时公开同输入六类图案（index=0..5，完整输入哈希同 §6）的质量与码率变化：

| 策略 | 字节 | kbit/s（30 FPS） | PSNR Y/U/V dB |
|---|---|---|---|
| 改前 | 2,908,024 | 116,320.96 | 37.9979 / 38.0516 / 38.0549 |
| 改后 | 2,904,875 | 116,195.00 | 37.9934 / 38.0544 / 38.0600 |

| 策略 | 码流 SHA-256 | 本地重建 SHA-256 |
|---|---|---|
| 改前 | `be062734d5d5aac112b460ff2b195705748755fa13be2f5b131c3057069f126e` | `8a1eaddd0a0f42133a99361b576fa8427d5048f8048e970da95019ad2ff0bc15` |
| 改后 | `7f5085686ab6d5e8c0880d7ebd815ed157beacd8224eeec35c0fbef5924d69bd` | `0fcc14eef129473dd6f382fce62159f7e6e402f7cadeaa743103486b7d5e6316` |

合成序列每帧切换图案，当前 P-only 分析尚无 Intra/Inter RD 决策，因此它的码率与
画质都劣于 §6 全帧内基线。这是明确已知缺口，后续 M5b 处理，不能只报告搜索耗时改善。
改前六帧质量测量与其他验证并行，耗时不作独占基准；质量/码率/哈希不受调度影响。

真实 testsrc2 的各 300 帧 720p/1080p、30FPS、QP26、GOP30、四 slice 输出码率为
5,676.016 / 12,628.748 kbit/s；均通过三方像素/哈希验证，输入和输出哈希见
[M5a 门禁](gates/m5a.md)。这些长测试同时运行其他验证，不用其 FPS 声明加速。
CPU profile、x264 等 QP/等码率质量门禁、SSIM、ABR/CBR/VBV 待 M5 后续阶段。
