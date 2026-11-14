# testdata

## M7 高级语料套件

`advanced/`新增25份、76个实际输出帧，清单总计55条。每份均登记原始位深的输入/输出SHA-256、帧数和Oracle来源。
`oracle=ffmpeg`表示真实FFmpeg原生输出比较，`oracle=analytic`表示规范解析像素；空值保留历史FFmpeg约定。
不能因FFmpeg退出0就宣称通过：还要核对完整字节数、帧数与像素。`sliceCount`是同一图片单个冗余表示的最大slice数，B/C不是额外slice。

- 八份x264：422/444 × 8/10-bit × 逐行/MBAFF，各6帧、64×64、双slice、QP26、B2、ref3。422隔行TFF，444隔行BFF，低两位含真实信息；另有6帧High NAL-HRD CBR声明语料。
- 十六份规范向量：七种FMO、独立ASO、A/B/C混合非零残差、primary/secondary SP和SI正负系数、冗余表示。期望由PCM源采样或8.6手算DC基生成，不调用编解码器或内部变换计算期望。
- SP primary两向量还与FFmpeg像素相等；其余十四份明确使用解析解。FFmpeg对FMO拒绝、ASO/分区输出不完整、secondary/SI输出不同，记录于[完整证据](../docs/gates/m7_corpus.json)，不计作FFmpeg通过。

FFmpeg8的[切片头实现](https://ffmpeg.org/doxygen/8.0/h264__slice_8c_source.html#l01872)仅消费而不保存`sp_for_switch_flag/slice_qs_delta`；不能以它覆盖规范8.6.2切换过程。
规范生成器复现（先设置`CGO_ENABLED=0`）：

```text
go run ./tools/fixtures -kind advanced -output-dir <temporary-directory>
```

生成器同时输出临时原始YUV与metadata，入库只保存小码流；外部x264的精确滤镜、参数与观察见证据JSON。
真实HRD语料使用testsrc2/64×64/6FPS/6帧、High、bitrate=maxrate=64000、bufsize128000，
`nal-hrd=cbr:force-cfr=1:threads=1:scenecut=0:aud=1:bframes=2:ref=2`；trace_headers与共享语法核对参数，
原始输出必须使用`-fps_mode passthrough`，不能依赖裸流猜测帧率复制帧。

执行`go test ./decoder -run 'TestCorpus_(AdvancedFFmpegPixelExact|ManifestOutputDigests)'`。
前者显式区分外部与解析解，后者不依赖FFmpeg且对所有Conformant条目核对原生像素和已登记帧数。


## M7 High10

`high10/`新增三份32×32/48×32、共18帧的真实10-bit420码流，包含CAVLC I/P、
CABAC I/P/B、双slice、8×8/JVT矩阵和低QP。三份均已与独立FFmpeg逐采样一致，
状态为`conformant`，码流与完整原始LE输出哈希登记在manifest；仅证明列明的工具组合。

生成输入为5 FPS、目标尺寸的10-bit滤镜：

```text
nullsrc=size=<size>:rate=5,format=yuv420p10le,geq=lum='mod(X*13+Y*7+N*19,1024)':cb='mod(251+X*3+N*7,1024)':cr='mod(769+Y*5+N*11,1024)'
```

使用FFmpeg 8.0/libx264、`-preset medium -profile:v high10`，固定`scenecut=0:threads=1:aud=1:keyint=30`。
001为32×32/6帧/QP26、`cabac=0:bframes=0:ref=1:weightp=0:8x8dct=0`；
002为48×32/8帧/QP26、`cabac=1:bframes=2:ref=3:weightp=2:slices=2:8x8dct=1:cqm=jvt`；
003为32×32/4帧/QP1、`cabac=1:bframes=0:ref=1:weightp=0:8x8dct=1`。
Oracle命令为`ffmpeg -v error -i <file> -map 0:v:0 -c:v rawvideo -pix_fmt yuv420p10le -f hash -hash sha256 -`，
哈希覆盖按显示顺序串联的完整原始采样字节。输入低两位有真实信息，禁止先降为8-bit再比较。

## M3 High 与不支持边界

七份 High 样本覆盖默认 CABAC（96×64/12 帧、176×144/12 帧、320×240/10 帧、
64×48/8 帧）、JVT 矩阵 CABAC（160×96/10 帧）、JVT+CAVLC（128×96/10 帧）、
JVT+双 slice（192×128/12 帧）。都使用 testsrc2 rate=5、x264 medium/High/yuv420p，
固定 threads=1、aud=1；矩阵样本额外设 cqm=jvt，CAVLC 设 cabac=0，多 slice 设 slices=2。
High 共 74 帧，输入与 FFmpeg 原始 YUV SHA-256 均登记为 conformant。

合法隔行 Main 样本为 64×64/4 帧，medium/Main/yuv420p、flags=+ildct+ilme、
x264-params=interlaced=1:tff=1:threads=1:aud=1。M7已接入MBAFF并将该条迁为conformant，
四帧与FFmpeg逐像素及固定YUV哈希一致。PAFF序列仍显式拒绝，恢复契约使用独立构造的PAFF边界。
本节记录原M3语料；当前总量和新增高级证据见上面的M7套件。

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
