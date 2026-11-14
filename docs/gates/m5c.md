# M5c：Main/High编码工具本地门禁

日期：2026-11-14；HW-M2、Go1.27.4 windows/amd64、FFmpeg/ffprobe8.0。
常规验证CGO=0；race单独CGO=1。实现基线与全部数值见
[机器核验记录](./m5cd_results.json)及[性能报告§14](../performance.md)。

## 范围与退出证据

| 要求 | 实现与可执行证据 |
|---|---|
| CABAC算术编码 | 与Decoder共享context表；64状态、bypass、terminate、PCM restart；规范区间解析向量及混合残差测试 |
| CABAC宏块与slice | Main/High I4/I16/PCM、P四分区/skip及B_Direct/B_Skip，QP/CBP/MVD/CBF、多slice；I/P288帧及B1120帧三方Oracle回归 |
| zero-word | 按slice计bin密度，合并PCM restart前计数，EBSP终端03处理和AU资源预算；阈值解析与5000个密集MPS流读回 |
| 可用B策略与重排 | 非参考spatial-direct零MV双锚点预测、4×4残差和skip；L0过去/L1未来，POC0与递增DTS；深度1/2/3/16、强制IDR/GOP/Flush/失败重试及有界所有权测试 |
| High8×8与ScalingMatrix | 规范逆基定点正向分析、I8/P8×8真实候选、共享反量化/逆变换/去块；4×4六列表/DC首项与8×8 intra/inter矩阵，320+126帧三方Oracle |
| explicit加权预测 | 单参考P固定或自动三平面权重；先MC后加权、clip、负/零权重、denom7和skip；Main/High160帧三方Oracle |
| Main/High参数集 | 实际SPS/PPS、CABAC/8×8/矩阵/权重信令和裁剪；h264probe与独立ffprobe确认当前长码流High、显示尺寸、每图4slice及POC显示位置 |
| RD候选与状态 | 保留1/3与1/6量化候选，以三平面SSE+lambda×CAVLC率代理选择；独立未剪枝搜索及B候选最低J测试，完整恢复像素/NZ/MV/模式 |
| 长序列 | 720p/1080p×QP18/26/34共1800帧，Go Decoder、FFmpeg、本地重建逐像素一致；实际IDR5/P100/B195/组，关键帧0/60/120/180/240 |
| 质量报告 | 六组同QP绝对Y差均≤原1.5dB；六组实测等码率误差均≤1%，PSNR/SSIM和效率差距全部公开，不用历史失败版本冒充当前结果 |
| 性能证据 | CABAC/B基准、两轮完整前后样本与300帧CPU profile见performance.md§11/13/14；当前标量不作实时承诺 |
| 显式边界 | 不支持的Profile/格式/并行度及8×8/矩阵选项在New拒绝；Configuration、ScalingSignalAndValidation、失败事务测试通过 |

CQP长测构建为clean的`eec92c7`，后续`919c3c5`/`fbcaad2`只调整RC分支。
最终生产基线`fbcaad2`另跑30帧CQP，码流
`732faa57d1a52684cb97d8ea1c48fc74124ed82f33618132776ee3d4b1b7e4cd`、
重建`2706ce814aac8bd2fdcebf97902473d87811daa3746d23d828fb580b60d4bdc1`
与同量化实现的前构建相等；CQP提前返回路径未改。历史§12的720p QP26失败保留，
新实现按RD候选选择通过，未改变QP、参考参数或1.5dB阈值。

## 工程验证

- 最新gofmt无输出、vet/build、非缓存全包test通过，codec30.680秒；depscheck30包、manifest27条通过。
- 编码实现完整非缓存`CGO_ENABLED=1 GOMAXPROCS=2 go test -race -count=1 -timeout 45m ./...`通过，codec681.840秒。随后仅新增h264probe诊断包，该包独立补跑race1.243秒；不将前次完整race冒充新增包的证据。
- Windows/Linux/macOS×amd64/arm64六目标CGO=0构建通过；不把交叉编译当成原生或SIMD验证。
- B CQP调度fuzz60秒3015次、7个新增覆盖输入无失败，仅作为smoke；不据此宣称完整RC fuzz覆盖。
- 所有长测的长度、输入/码流/重建/FFmpeg哈希、实际帧型、QP和包时间戳已重新核对；无未完成项冒充通过。
- `1f00d90`的h264probe对12份长码流全部断言通过，逐图POC对应显示位置唯一完整；工具仅解析头部，不替代上述像素验证。

## 支持边界

已验收的是8-bit420、渐进、偶数显示尺寸、串行及上述明确生成策略。
B运动搜索、temporal-direct、B-pyramid和B权重未生成，P仅单参考，CABAC RD仍用
CAVLC率代理；High10/422/444和字段编码留到M7，SIMD/并行留到M8。
完整Profile状态保持Partial，不将本地工具闭环表述为全Profile认证。
仓库未配置远端，远端CI未执行；本文件仅关闭本地子阶段门禁。
