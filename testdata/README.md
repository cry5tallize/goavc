# testdata

## M3 B 帧、加权与显示重排

三份 `main_*_cabac_default_008/009/010` 分别为 96×64/16 帧、176×144/14 帧、
320×240/12 帧：x264 medium/Main/8-bit 4:2:0 默认编码工具，仅固定 threads=1、
aud=1 和 testsrc2 rate=5，覆盖 B-pyramid、多参考、CABAC 与隐式权重。
`bframes/main_128x96_yuv420p_cavlc_temporal_001` 为 16 帧，额外设 cabac=0、
direct=temporal、b-adapt=0，验证另一熵模式及 temporal direct。

`main_160x96_yuv420p_cabac_weighted_011` 为 12 帧 testsrc2 + fade=out:st=0:d=2，
Main medium、bframes=0，测试确认权重并非全为恒等值。三帧合成显式 B 权重样本
由 `go run ./tools/fixtures -kind weighted-b` 重建，输入顺序 I0/I4/B2，三平面
权重和偏移各不相同；其解析解首样本和 FFmpeg 输出同时核验。
原有 `main_64x48_yuv420p_cabac_ipb_001` 已从 unsupported 更新为 conformant。

## M3 多参考与 MMCO 定向语料

`main_128x96_yuv420p_cabac_multiref_005` 为 12 帧、ref=4 的 Main CABAC I/P；
`baseline_96x64_yuv420p_cavlc_multiref_006` 为 10 帧、ref=3 的 CAVLC I/P。
使用上述尺寸与帧数、x264 medium/QP=26、bframes=0、weightp=0、scenecut=0、
threads=1、aud=1、keyint=30 生成，并以 FFmpeg 的完整 rawvideo 输出哈希固定。

`main_16x16_yuv420p_cavlc_mmco_006` 与 `poc1_007` 由本仓库的规范小向量生成器
产生，均为 6 帧，覆盖 MMCO 1..6、长期参考选择、列表重排和 MMCO5 后恢复。
可用 `go run ./tools/fixtures` 重建 type 0 样本；type 1 使用
`go run ./tools/fixtures -poc-type 1 -output testdata/main/main_16x16_yuv420p_cavlc_poc1_007.264`。
预先设置 CGO_ENABLED=0；向量输出必须由独立 FFmpeg 再次逐像素核验后才能更新 manifest。

## M3 首批 CABAC 语料

新增三份 Main 语料：64×48 全 I（3 帧）、176×144 I/P（5 帧，init_idc=1）、
320×240 双 slice I/P（4 帧，init_idc=2）。三者均为单参考、无 B 帧、无加权预测，
状态为 `conformant`，最初证明 M3 的 I/P 子集；后续 B 与重排验证见上节。

生成环境 FFmpeg 8.0 + libx264，通用命令如下；替换 size/count/idc/slices/keyint：

```text
ffmpeg -v error -f lavfi -i testsrc2=size=<size>:rate=5 -frames:v <count> -c:v libx264 -preset medium -profile:v main -qp 26 -x264-params cabac=1:cabac-idc=<idc>:bframes=0:ref=1:weightp=0:keyint=<keyint>:scenecut=0:threads=1:aud=1:slices=<slices> -f h264 <output.264>
```

全 I 使用 keyint=1、idc=0、slices=1；176×144 使用 keyint=30、slices=1；
320×240 使用 keyint=30、slices=2。执行 `go test ./decoder -run TestCABAC -v`
可验证真实像素、PCM 后的算术引擎重启及截断后的 IDR 恢复。固定 Oracle 哈希
由 `TestCorpus_ManifestOutputDigests` 在没有 FFmpeg 的环境中继续核验。

## M2 语料与 Oracle

`manifest.json` 登记五份 Baseline I/P 样本，共 21 帧；多 slice 样本每帧两个
slice，并在中途再次出现 IDR。全部通过 FFmpeg 8.0 的 Y/U/V 逐像素对比，
`expectedYUVSha256` 是 FFmpeg 输出的完整原始 YUV 序列哈希，不需要入库大体积 YUV。
该阶段原有 Main/CABAC/B 样本尚未支持；M3 已补齐并更新其验证状态。

前四份 Baseline 文件来自接手时工作区中的 x264 语料，原始生成命令未保留；
其内容由 manifest 输入哈希固定。1080p 样本新增于 M2，生成命令为：

```text
ffmpeg -v error -f lavfi -i testsrc2=size=1920x1080:rate=30 -frames:v 3 -c:v libx264 -preset veryfast -profile:v baseline -qp 26 -pix_fmt yuv420p -x264-params ref=1:bframes=0:weightp=0:scenecut=0:threads=1:aud=1:keyint=30 -f h264 testdata/baseline/baseline_1920x1080_yuv420p_cavlc_ip_005.264
```

Oracle 复现：`ffmpeg -v error -i <input.264> -f rawvideo -pix_fmt yuv420p <output.yuv>`，
对输出计算 SHA-256。实时对比执行 `go test ./decoder -run TestCorpus_BaselinePixelExact -v`；
固定哈希与不支持样本执行 `go test ./decoder -run TestCorpus_ManifestOutputDigests -v`。
运行 Go 命令前设置 `CGO_ENABLED=0`。这些哈希来自独立 FFmpeg，不能用待验证的
解码器输出来重写。

测试语料目录。文件本身的纳入与否由体积决定，但**每一条语料都必须在
`manifest.json` 中登记**，否则无法参与 CI 校验。

## 目录约定

```text
testdata/
├── manifest.json     语料清单（唯一入口）
├── baseline/         Baseline profile 码流（CAVLC，I/P）
├── main/             Main profile（CABAC，含 B 帧）
├── high/             High profile（8×8 变换、ScalingMatrix）
├── bframes/          B 帧重排与 POC 专项
├── malformed/        损坏 / 截断 / 恶意构造样本（fuzz 种子来源）
└── ref/              参考解码输出（raw YUV），体积大时改为按需生成
```

## 命名规则

```text
{profile}_{WxH}_{pixfmt}_{entropy}_{frametypes}_{tag}_{seq}.264
```

示例：

```text
baseline_352x288_yuv420p_cavlc_ip_001.264
high_1280x720_yuv420p_cabac_ibbp_014.264
main_640x480_yuv420p_cabac_mmco_003.264
```

`manifest.json` 中的 `name` 必须等于文件名去掉扩展名。
参考输出使用同名 `.yuv`；损坏样本放在 `malformed/{target}_{case}_{seq}.264`。

## 语料来源

| 来源 | 说明 |
|---|---|
| `x264` | 用固定参数生成的互操作样本（最常用的回归语料） |
| `ffmpeg-fate` | FFmpeg FATE 测试集中的 H.264 样本 |
| `jm` | JM 参考软件生成的规范边界样本（POC type 1、MMCO 等） |
| `synthetic` | 程序化生成的图案（用于精确验证单个预测模式） |
| `corrupt` | 人工损坏样本，只用于健壮性测试 |

## 为什么大文件不入库

参考 YUV 与长序列码流可以让仓库膨胀到 GB 级。约定：

- 小于 2 MiB 的码流直接入库；
- 更大的语料在 manifest 中用 `url` + `sha256` 声明，由 CI 按需拉取并校验哈希；
- 参考 YUV 优先由 FFmpeg 在测试时现场生成（Oracle 模式），只有需要
  "冻结历史输出"用于回归时才入库。

## 状态字段

| status | 含义 | CI 行为 |
|---|---|---|
| `conformant` | 已通过逐像素对比 | 必须跑 Oracle，任何差异即失败 |
| `unsupported` | 已知不支持 | 必须断言返回 `ErrUnsupported`，且 `notes` 说明缺口 |
| `wip` | 已入库未验证 | 只跑基础健壮性（不 panic、不越界） |
