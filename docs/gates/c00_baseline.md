# C00：B05 后解码基线与成本账本

生产基线 `9f493b3`，HW-M2 Ryzen 7 5700X、Windows amd64、Go 1.27.4、
CGO=0、GOAMD64=v1、GOMAXPROCS=16、decoder Threads=1。
旧 B05 交错样本与本次独立基线分列；C01 及以后比较各自的父版本。

## 固定输入与测量入口

- 原 1080p Baseline/High 各 1/4 slice 和 B05 的 High10 各 1/4 slice，均为原 300 帧文件；
  沿用原独立 FFmpeg 像素摘要，不重新生成原主矩阵。
- 新增三份 **60 帧合成留出样本**：静态桌面代理（色条网格）、10-bit 高纹理（固定 seed 噪声）、
  低码率重复运动帧（5→30fps），覆盖 8/10-bit 和单/四 slice。名称不是已测宏块分布；
  不以重复帧推断实测 skip 比例。这些本地性能输入不进入静态语料清单。
- `scripts/prepare_c00_inputs.ps1` 使用 FFmpeg 8.0/libx264 生成码流和独立原生 YUV 摘要，
  并检查参考总字节数。`scripts/measure_c00.ps1` 复用 Sequence：每格 Auto/purego、
  同进程 `1x/count5`，保留每样本耗时/分配及对应输入身份，每次均核对完整像素和帧数；
  文件 I/O、组 AU 和像素校验不计时。与独立进程交错配对的数据不可混合做前后结论。

```powershell
./scripts/prepare_c00_inputs.ps1
./scripts/measure_c00.ps1 -Out tmp/c00-frozen -ExtraInputs tmp/c00-inputs/inputs.json
```

运行时从仓库根目录出发，输出目录必须是新目录。冻结二进制保存在 `tmp/c00-frozen/`；
测量脚本输出历史主输入和留出输入的原始日志与 `results.json`。

## 实测基线

2026-11-18 的 18 格、90 个正式样本共核对 19,800 帧，全部通过。每格以五次耗时
中位数计算 FPS；完整耗时、B/op、allocs/op、生成参数与输入/像素身份见
[c00_baseline_samples.json](c00_baseline_samples.json)。

| 输入 | 帧/op | Auto FPS | purego FPS |
|---|---:|---:|---:|
| Baseline 1 slice | 300 | 67.48 | 58.21 |
| Baseline 4 slice | 300 | 62.28 | 55.27 |
| High 1 slice | 300 | 30.33 | 28.24 |
| High 4 slice | 300 | 30.71 | 24.59 |
| High10 1 slice | 300 | 23.03 | 20.88 |
| High10 4 slice | 300 | 24.67 | 22.85 |
| 静态网格 | 60 | 44.63 | 38.66 |
| 固定种子高纹理 | 60 | 5.17 | 5.27 |
| 低码率重复运动帧 | 60 | 50.37 | 42.01 |

高纹理显著慢于主输入，不能用主矩阵的 FPS 外推；Auto/purego 在此格约 2% 差异，
未做配对，不据此判断回退。同一生产源码的新基线亦不同于 B05 的历史独立进程结果，
所以正式比较继续要求冻结二进制交错配对。

## 成本账本

复用同一源码的[规划 CPU 采样](c00_planning_profile.json)，下列为解码调用子图的 CPU 占比；
不是完整端点壁钟，也不能相加有重叠的累计节点。50% 局部耗时下降列只是算术情景，
不是承诺或可累加的整链收益。

| 后续 | 目标负载 | 成本证据 | 若该部分减半，子图 CPU 耗时下降约 |
|---|---|---:|---:|
| C01 | 局部亮度边缓冲 | A07 新建局部数组 1 alloc/op；实际图像平面已在堆上 | 不预计端点收益 |
| C02 | High10 输出 | toFrame 9.13% 累计；包含分配、打包等 | 4.6%（整个子树减半情景） |
| C03 | High8 B 帧 | decodeBMacroblock 自身 11.55%，不是累计 48.27% | 5.8%（仅自身减半） |
| C04 | High8 混合流 | bSAtBlocks 累计 15.47%，包括运动比较 | 7.7%（整个子树减半情景） |
| C05 | 多参考混合流 | Grid.Get 累计 8.20%，不能全归因身份表 | 4.1%（整个读取子树减半情景） |
| C06 | High10 残差 | decodeResidual 累计 26.02%，与 Bin 重叠 | 13.0%（整个子树减半情景） |
| C07 | 8-bit IDR | 亮度 SSE2 自身 17.46%，混合流小得多 | 8.7%（仅 IDR） |
| C08 | 常驻实例 | High8 Grid.Reset 自身 3.70%；另看 resident 分配/GC | Reset 项约 1.9%，不能代替留存验证 |
| C09/C10 | 多 slice/最终应用 | 尚无实测并行效率或独立训练与留出结果 | 暂不外推 |

原输入中已有固定合成运动；自然实拍留出、硬件 cache-miss、宏块模式/残差/patch
频率尚没有实测。C03 用专用测试构建先取分布再决定哪个模式值得专化；
C05/C08 用分配与生命周期证据决定是否改变布局或回收，不能用 CPU 函数名代替证据。

常驻实例入口 `BenchmarkDecoder_Resident_ResetCycle` 复用一个 decoder，
每轮完整 Send/Receive/Flush/Reset 并核对帧数，记录 GC 次数和暂停；
它与冷端点分列。可附加 `-cpuprofile`、`-memprofile` 诊断，
使用 pprof 的 `alloc_space` 和 `alloc_objects` 分别查看字节和对象归因。

| 常驻 High 单 slice（两轮诊断） | 每轮 300 帧 |
|---|---:|
| B/op / allocs/op | 1,269,758,416 / 15,690 |
| GC/op / GC 暂停 ns/op | 32.50 / 495,400 |

采样二进制在独立 `9f493b3` 工作树上只叠加 resident 基准，启用 CPU/heap profile，
不把其计时用于吞吐比较。采样分配字节中 `motion.NewGrid` 自身 49.09%，
`newFrameStateForPicture` 自身 39.19%，后者累计 88.80%（包含前者，不相加）。
对象数的 `NewGrid` 为 6.40% 自身，不能用其字节占比推断对象数。
memprofile 还包括计时外的组 AU/采样器工作，不用于直接估算 live heap；
原日志与 `cpu/mem.pprof` 在 `tmp/c-plan/resident*`，关键计数随正式样本归档。
