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

## 6. 编码基准

| 分辨率 | Profile | 码控 | QP/码率 | FPS | PSNR-Y | 码率实测 | commit |
|---|---|---|---|---|---|---|---|
| _待 M4_ | | | | | | | |

## 7. 内核对比

| Kernel | Generic | SSE2 | AVX2 | NEON | 说明 |
|---|---|---|---|---|---|
| _待 M8_ | | | | | |

对比原则：同一输入、同一输出约束、同一编译参数；**速度与画质必须同时报告**，
不允许只挑对自己有利的一项。
