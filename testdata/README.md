# testdata

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
