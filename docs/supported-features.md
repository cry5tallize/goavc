# 支持矩阵

> **M5 当前进展：** M5a/b/c/d本地门禁均已通过，M5c/d标签为v0.5.3/v0.6.0。
> 两尺寸×QP18/26/34六项质量、ABR/CBR/VBV六项码控，每项300帧三方Oracle通过。
> 六项绝对Y差均≤原1.5dB，ABR偏差均<1%，CBR窗口和VBV逐AU边界已核验。
> 具体版本、完整数值和已知边界见 gates/m5c.md、gates/m5d.md；远端CI未执行。
> 这是限定工具和配置的验收，完整Profile仍为Partial，等码率效率与标量吞吐仍待优化。

> **M4 进展：** `encoder.New` 默认执行串行 Baseline CQP 的 I4/I16/色度压缩，
> 每帧独立 IDR；支持 I_PCM 选项、1..64 slice、偶数显示尺寸裁剪和宏块 QP 覆盖。
> 两条路径及混合 PCM 已通过 FFmpeg/自研解码/本地重建逐像素验证。
> 本地门禁及性能记录已完成，证据见 gates/m4.md；远端 CI 尚待执行。
> M5c 已接入Main/High CABAC I/P及非参考spatial B_Direct/B_Skip和重排。M5d已接入ABR/CBR/VBV，
> CBR/VBV码量不足时输出filler NAL；HRD信令仍属后续扩展。LevelAuto实施AU预算门禁，
> 1080p@30已通过完整长矩阵三方像素验证。M5a本地门禁见gates/m5a.md，M5c/d见对应门禁页。

> 状态图例：`TODO` 未开始 · `WIP` 进行中 · `Partial` 部分可用（有已知不支持路径）·
> `Conformant` 通过规定验证 · `Unsupported` 明确拒绝并返回 `ErrUnsupported`
>
> 规则：**只有通过 Oracle 或规范解析解测试的模块才能标 `Conformant`**；
> 仅靠自研编解码器互相验证的，最高只能标 `Partial`。
>
> **Profile 边界：** Baseline/Main PCM 仅1..255，CAVLC level_prefix≤15。
> 极低QP的内容若无合法候选则 ErrUnsupported；不能以 FFmpeg 可解替代 Annex A 约束。
>
> 本文档与每个 PR 同步更新。任何新增的 `Unsupported` 必须写明原因与计划。
>
> **M2 状态说明：** Baseline 的无重排、单参考 CAVLC I/P 解码已通过整帧 Oracle。
> `Conformant` 仅针对表内注明的范围；Baseline Profile 整体保留 `Partial`，因为
> FMO/ASO已由M7接入并通过规范像素向量；冗余slice及其余组合边界仍按各项记录。编码流水线从M4开始。
>
> **M3 进展：** CABAC/CAVLC 多参考 I/P/B、MMCO 与长期参考已验证；
> High 8×8/ScalingMatrix、Main 渐进帧 I/P/B、direct、加权预测和重排均已验证。
> Profile 整体保留 Partial，范围外的字段/高位深工具仍明确拒绝；本地门禁证据见 gates/m3.md。

> **M7 进展：** 当前分支`feat/m7-high422`。第1–21项及共享预测/滤波/运动前置已完成；
> High10渐进420双熵I/P/B解码、原生LE输出与压缩编码通过独立Oracle，编码32配置共384帧三方逐采样一致。
> High422/High444渐进8/10-bit双熵编解码、原生LE与实际CLI已验收；六格式QP/矩阵/权重/B/码控三方Oracle通过。MBAFF解码见下，PAFF与transform bypass仍拒绝，不声明完整Profile支持。
> 共享预测已提供byte/uint16类型实例：高位深帧内、六抽头/色度MC、权重以及422矩形色度预测。
> 独立提交快照通过完整常规门禁；这些内核入口不单独扩大Profile或编解码组合的支持声明。
> 共享去块已完成8/10-bit类型参数化，弱/强滤波解析解及独立提交完整常规门禁通过；流水线按后续条目验收。
> 共享运动搜索已覆盖8/10-bit原始SAD及所有亚像素阶段，低两位纹理与负MV解析测试、独立完整常规门禁通过。
> 8/10-bit422解码两格式×九配置各八帧与FFmpeg逐采样一致，覆盖双熵I/P/B、矩阵/权重/多slice；裁剪、PCM、格式切换及预算边界通过，独立完整常规门禁通过。
> 422的2×4色度DC正逆变换与八项扫描已通过规范解析值和独立完整门禁；后续编码/解码使用同一内核。
> CABAC八项色度DC已按规范上下文轨迹验证，仍复用ChromaDC类别；独立完整门禁通过，420路径不变。
> CAVLC八系数ChromaDC422的规范码字、固定比特串与独立完整门禁通过，保留420四系数路径。
> PPS的六/八/十二个缩放列表由transform_8x8_mode_flag和色度共同确定；未开启8×8时不再误读写两个额外标志，固定规范RBSP及独立完整门禁通过。
> High444共同编码双熵I/P/B两格式×九配置各八帧与FFmpeg一致；十四类CABAC、逐分量矩阵与QP、亮度插值及共享亮度bS已接入。独立平面PCM规范向量与单色投影通过；FFmpeg8不支持原始独立平面封装，投影不替代该工具能力声明。
> MBAFF解码已接入宏块对、帧场混合邻域、场参考/扫描与逐对滤波；六格式×双熵×两场顺序×I/P/B/矩阵×全场/真实混合192配置共1152帧与FFmpeg一致，独立完整门禁及实际六帧CLI通过。
> 420对侧场色度MV的±2相位已由规范码流和FFmpeg锁定；Main隔行语料为conformant。隔行编码已支持真实全field-coded MBAFF及TFF/BFF，六格式双熵三方Oracle和实际CLI场序通过；不做自适应帧/场RDO，PAFF仍拒绝。

## 1. Profile

| Profile | profile_idc | 关键工具 | Decoder | Encoder | 目标里程碑 |
|---|---|---|---|---|---|
| Baseline | 66 | CAVLC、I/P | Partial | Partial | M4 I_PCM/I4/I16、M5 I/P/CAVLC及码控已验证；高级工具M7，禁止Baseline B |
| Main | 77 | CABAC、B、隔行 | Partial | Partial | 渐进I/P/B、MBAFF解码及真实场宏块TFF/BFF编码通过Oracle；编码限既有spatial B工具，PAFF拒绝 |
| High | 100 | 8×8 变换、ScalingMatrix | Partial | Partial | 4/8×8、矩阵、MBAFF解码和全场宏块TFF/BFF编码通过Oracle；PAFF拒绝，B编码限既有spatial-direct |
| High 10 | 110 | 10 bit | Partial | Partial | 渐进420双熵、MBAFF解码与真实场宏块编码通过Oracle；PAFF/transform bypass拒绝，编码B限既有spatial-direct |
| High 4:2:2 | 122 | 4:2:2、10 bit | Partial | Partial | 渐进422双熵、MBAFF解码与真实场宏块编码通过Oracle；PAFF/transform bypass拒绝，编码B限既有spatial-direct |
| High 4:4:4 Predictive | 244 | 4:4:4、独立色平面 | Partial | Partial | 共同444编解码、MBAFF解码与真实场宏块编码通过Oracle；独立平面按规范与单色投影验证，PAFF/transform bypass拒绝 |

## 2. 容器与封装

| 特性 | Decoder | Encoder | 备注 |
|---|---|---|---|
| Annex-B（3/4 字节起始码） | Conformant | Conformant | M4 编码输出四字节起始码、完整 AU，PCM/Intra 双解码器 Oracle |
| AVCC（1/2/3/4 字节长度前缀） | Partial | Partial | M1 读写已验证；尚无真实 MP4 语料 |
| avcC 配置记录 | Partial | Partial | M1 解析/序列化；尚无真实 avcC 语料 |
| Access Unit 组装（多 slice） | Conformant | Conformant | M4 1..64 slice 参数化，跨 slice 预测/熵隔离，滤波 idc=0；M6 增量 I/O |
| MP4 demux / mux | Unsupported | Unsupported | 明确不在核心范围内 |
| RTP 打包 | Unsupported | Unsupported | 未来独立适配层 |

## 3. 语法层

| 语法元素 | Spec | Decoder | Encoder | 备注 |
|---|---|---|---|---|
| SPS | 7.3.2.1 | Partial | Partial | M7扩展Profile及帧/场/MBAFF语法通过规范字节；真实隔行h264probe成功，像素流水线的场/MBAFF仍拒绝，组合级范围见台账 |
| PPS | 7.3.2.2 | Partial | Partial | 核心与缩放扩展；M7 FMO 0–6读写、动态cycle与帧场地址映射通过独立规范字节/解析解；Baseline/Extended CAVLC像素遍历/ASO与实际CLI通过，组合限制见台账 |
| VUI / HRD | E.1 / E.2 / Annex A | Conformant | Conformant | 完整条件参数读写、NAL/VCL调度/尺度、level倍率/DPB及显式预算通过规范固定字节和边界测试；状态仅语法API，不声称编码流水线已输出完整HRD/SEI时序 |
| ScalingList / ScalingMatrix | 7.3.2.1.1.1 | Conformant | Conformant | SPS/PPS 4×4/8×8、444十二列表与fallback A/B；完整presence组合、原生8/10-bit编解码Oracle、尾部错误写前原子拒绝、缓存/语法一致性和固定反量化值通过；Profile禁止的扩展不会静默输出 |
| SEI（user_data / recovery point） | 7.3.2.3 | Partial | Partial | M1 框架与 user_data；其余语义消息 M2/M3 |
| Slice Header | 7.3.3 | Conformant | Partial | M3 I/P/B、重排、权重和 MMCO 参数保留并消费；范围外工具明确拒绝 |
| Macroblock Layer | 7.3.4 | Conformant | Partial | M4 I_PCM/I4/I16 的 CBP/QP/MPM/残差已验证；P/B 编码在 M5 |
| Slice Data Partitioning | 7.3.5 | Conformant | Unsupported | Extended/CAVLC的A/B/C按slice_id关联，PCM与帧内/帧间残差独立消费；规范非零DC/MVD/混合像素、坏关联恢复及真实CLI两帧通过。不隐藏缺失残差；未提供分区编码 |
| SP / SI Slice | 7.3.3 / 8.6 | Conformant | Unsupported | Extended逐行8-bit420/CAVLC/flat4×4；primary/secondary、QP/QS独立、SP Skip切换量化及SI特殊/普通I宏块通过非零空间基与完整帧规范解析解。不是普通P近似，不提供切换编码 |
| Redundant Slice | 7.4.1.2.3 | Conformant | Unsupported | primary与各cnt独立重建，选择最小完整表示，仅一次输出/参考提交；ASO/不完整回退/非零MVD/坏覆盖恢复通过规范像素。不拼接不同cnt的宏块，不生成冗余编码 |

## 4. 熵编码

| 特性 | Decoder | Encoder | 备注 |
|---|---|---|---|
| CAVLC：coeff_token / trailing ones | Conformant | Conformant | M4 规范码字、极端系数、完整 Intra Oracle |
| CAVLC：total_zeros / run_before | Conformant | Conformant | M4 亮度 DC/AC、色度 DC、长游程与整帧 Oracle |
| CAVLC：level prefix / suffix / 转义 | Conformant | Conformant | M4 suffix 0/1..6、百万 level 转义、14,400 组往返及 Oracle |
| CABAC：算术编解码器 | Conformant | Conformant | 编码区间解析解、64状态与 Main/High I/P 整帧 FFmpeg Oracle |
| CABAC：上下文初始化与状态转移 | Conformant | Conformant | I/PB0..2、QP 0..51 共用初始化表；编码 slice 使用 idc=0 |
| CABAC：宏块/MV/残差上下文 | Conformant | Partial | I/P四分区与4×4/8×8/DC/AC、非参考B_Direct/B_Skip残差Oracle；B非Direct分区未生成 |
| CABAC：terminate / cabac_zero_word | Conformant | Conformant | 停止位/PCM重启、逐slice bin密度预算、终端03封装、阈值解析解与5000个MPS密集流读回；现有整帧Oracle回归通过 |

## 5. 预测

| 特性 | Decoder | Encoder | 备注 |
|---|---|---|---|
| Intra 4×4（9 模式） | Conformant | Conformant | M4 全模式 SAD 实际选中，MPM/边界与 60 帧六类图案 Oracle |
| Intra 8×8（High） | Conformant | Conformant | 共享九模式预测、规范邻域、实际Intra8决策、High320帧三方Oracle；编码需WithTransform8x8 |
| Intra 16×16（4 模式） | Conformant | Conformant | M4 全模式实际选中、共享预测与 DC/AC 重建 Oracle |
| Chroma 8×8（4:2:0） | Conformant | Conformant | M4 全模式实际选中、双平面 SAD、共享 DC/AC/滤波 Oracle |
| Chroma 4:2:2 | Conformant | Conformant | M7第6/7/10项8×16预测、2×4 DC、八DC双熵、MC与全高滤波通过规范解析解和编解码三方Oracle；8/10-bit，水平偶数、垂直可奇数 |
| Chroma 4:4:4 | Conformant | Conformant | M7第8/9/10项全尺寸预测/亮度插值、逐分量矩阵与QP、十四类CABAC、共享亮度bS及原生8/10-bit通过三方Oracle；显示宽高均可奇数 |
| Inter 16×16 / 16×8 / 8×16 / 8×8 | Conformant | Conformant | M5a 四种分区实际选中；单参考 P，360+600 帧双解码器 Oracle |
| 亚像素插值（1/2、1/4） | Conformant | Conformant | M5a 共享 MC、解析亚像素平移与完整 P 帧 Oracle |
| Weighted Prediction（explicit / implicit） | Conformant | Partial | 编码 Main/High 单参考 P 显式权重已验证：自动分析/固定三平面权重、负权重、裁剪、隐含默认值、多 slice 与160帧三方 Oracle；B/implicit 编码待实现 |
| B Direct（temporal / spatial） | Conformant | Partial | 编码spatial-direct零MV双参考、B_Skip/B_Direct残差实际选中，1120帧三方Oracle；temporal与非Direct运动搜索未生成 |
| MBAFF 宏块对 | Conformant | Conformant | 解码帧/场混合192配置共1152帧Oracle；编码固定真实场宏块，六格式双熵、TFF/BFF、多slice/B/矩阵/显式PCM三方Oracle和实际CLI场序通过，不做自适应帧/场RDO |
| PAFF 场图像 | Unsupported | Unsupported | 场图片/非MBAFF隔行序列的参考管理和场对输出尚未接入，明确ErrUnsupported，不以MBAFF支持代替 |

## 6. 变换与量化

| 特性 | Decoder | Encoder | 备注 |
|---|---|---|---|
| 4×4 逆/正整数变换 | Conformant | Conformant | M4 正向独立矩阵基底与完整 Oracle；编码重建共享逆内核 |
| 4×4 亮度 DC Hadamard | Conformant | Conformant | M4 常量/基底解析解及 I16 Oracle |
| 2×2 色度 DC Hadamard | Conformant | Conformant | M4 常量解析解及完整 Intra Oracle |
| 8×8 逆/正变换 | Conformant | Conformant | 规范逆基定点分析、解析空间基；High Intra8/P 8×8候选/信令/共享逆变换/去块，320帧三方Oracle |
| 量化 / 反量化 | Conformant | Conformant | M4 平坦矩阵 QP 0..51；编码重建共享反量化；非平坦正向 M5c |
| ScalingMatrix 应用 | Conformant | Conformant | 编码High六个4×4列表/DC首项与intra/inter8×8列表，光栅配置、PPS扫描/delta与共享重建；320帧组合及126帧4×4/DC+B三方Oracle；限渐进8-bit4:2:0 |
| 10 bit 变换路径 | Partial | Partial | M7第3项共享内核已按规范解析解验证：QP'0..63、负QPY与色度映射、4×4/8×8/DC及正负极值；编解码流水线尚未接入，Profile不升级 |

## 7. 滤波

| 特性 | Decoder | Encoder | 备注 |
|---|---|---|---|
| 边界强度 bS 推导 | Conformant | Conformant | M4 intra 内部=3、宏块边界=4，混合 PCM Oracle |
| 亮度边缘滤波 | Conformant | Conformant | M4 编解码共享内核，三方像素一致 |
| 色度边缘滤波 | Conformant | Conformant | M4 非线性色度 QP 先映射再平均，共享内核 |
| slice 边界 / disable_deblocking | Conformant | Partial | M4 PCM 通道 idc=1；Intra idc=0 跨 slice 滤波；暂无用户自定义 offsets/idc |

## 8. 参考帧管理

| 特性 | Decoder | Encoder | 备注 |
|---|---|---|---|
| 单参考帧 | Conformant | Conformant | M5a 已滤波参考、IDR 边界、P_Skip、16 帧号回绕与长序列 Oracle |
| 多参考帧 + reordering | Conformant | TODO | M3 P/B 列表、短期/长期重排通过解析解与 Oracle |
| POC type 0 / 1 / 2 | Conformant | Partial | 编码I/P type2；启用B为type0、16-bit POC LSB、IDR锚点与显示重排已验证；type1编码未生成 |
| 滑动窗口标记 | Conformant | TODO | M3 PicNum 回绕排序、淘汰与所有权单测/Oracle |
| MMCO（含长期参考） | Conformant | TODO | M3 操作 1..6、长期参考、重置及专设 Oracle 语料；字段 M7 |
| 输出重排序与延迟输出 | Conformant | Conformant | 编码未来参考锚点先于B输出、规则PTS/Duration和递增DTS、Flush/Reset/Close/输入深拷贝，1120帧三方Oracle |
| Flush 排空语义 | Conformant | Conformant | M4 单槽输出、重复 Flush、排空 EOF、Reset/Close/背压测试 |

## 9. 编码器工具

| 特性 | 状态 | 目标里程碑 |
|---|---|---|
| CQP | Conformant | M4 固定 QP 与宏块 QP 回调，QP=0 仍为有损；无残差沿用前 QP |
| ABR / CBR / VBV | Conformant | M5d | 两尺寸×三模式各300帧三方Oracle、ABR偏差<1%、CBR十窗口、VBV300步独立模型；另有分数FPS/filler和120帧突变压力。I/P/B反馈失败不污染历史；不含HRD信令 |
| GOP 结构 / IDR 间隔 | Conformant | M5d | 独立GOP/IDR、连续1..16非参考B、强制IDR与场景边界；两尺寸长矩阵ffprobe帧型/关键帧正确；B需规则PTS与恒定Duration |
| 场景切换检测 | Conformant | M5d | 直接阈值比较或WithLookahead未来窗口确认/闪光抑制；实际IDR序列、PTS/DTS与三方Oracle |
| Lookahead | Conformant | M5d | WithLookahead(1..16)有界深拷贝，未来确认场景/抑制闪光；B调度避免跨切换边界；背压/Flush/Reset/Close/失败事务已测 |
| GOP头部探测与断言 | Conformant | M5d | cmd/h264probe有界解析实际参数集/POC/帧型/每图slice数，图片/B深度/IDR断言失败返回非零；12份长产物与ffprobe交叉核对，不替代像素Oracle |
| RD 模式决策（J = D + λR） | Conformant | 定点lambda、三平面SSE、Skip/P/I4/I16/PCM；P/B两种死区保留原候选按J选优。完整长质量矩阵通过；CABAC率仍为CAVLC代理，不宣称全局最优 |
| 自适应 QP | Conformant | M5b 活动度 QP 选项 0..6、QP 0..51 裁剪，默认关闭，显式回调/PCM 冲突拒绝 |
| SSE2 / AVX2 / NEON | TODO | M8 |

## 10. 流式I/O（M6）

| 能力 | 状态 | 证据与边界 |
|---|---|---|
| 四个流接口 | Conformant | 具体适配器及同步pipeline均已实现；输出所有权、同步消费、EOF与错误释放测试通过 |
| 8-bit YUV Reader | Conformant | 420/422/444、奇数尺寸、独立帧所有权、序号PTS/Duration1、帧边界EOF和尾帧截断；已知字节布局验证。原始I/O不扩大编码器格式支持 |
| 8-bit YUV Writer | Conformant | 跳过stride填充、最小末行缓冲、写前完整校验、短写及I/O失败后停止追加；已知字节与三格式多帧往返验证。无原始流元数据头 |
| 增量Annex-B NAL Reader | Conformant | 跨1/2/3/7/4096/整文件chunk，独立EBSP、默认8MiB单NAL限额、严格前导与残缺尾起始码处理；只切分NAL，不提供AU/帧/时间戳 |
| 增量AVCC NAL Reader | Conformant | 1..4长度前缀、从提供的avcC取宽度并先交付复制的参数集，默认8MiB单NAL、分配前限额、截断/错误粘性；仅NAL，不解析MP4或提供AU/时间戳 |
| 增量AU Reader | Conformant | 渐进8/10-bit、MBAFF、FMO/ASO和A/B/C头部；默认按图片标识分组，支持ASO跨A关联B/C，拒绝重复/孤立/逆序分区，实际CLI通过。默认32MiB/4096 NAL，时间戳均0，不验证残差，PAFF/冗余像素仍按各项边界 |
| Annex-B AU Writer | Conformant | 完整封装/预算验证后写四字节起始码，坏尾部不提前写、短写/I/O错误后停止追加；固定字节及High多slice语料重扫一致。只接受调用方已分组的Annex-B AU，不验证slice或序列化时间戳；默认8MiB单NAL、32MiB输入及输出AU、4096 NAL |
| AVCC AU Writer | Conformant | 已分组Annex-B AU转1..4字节大端前缀（默认4），与Annex-B Writer共享完整封装/限额验证；长度容量在扫描分配前收紧，坏尾部/后续NAL超限零写出，短写/I/O错误粘性。独立字节解析解及5份语料分块往返通过；默认8MiB单NAL、32MiB输入及输出AU、4096 NAL，不生成avcC/MP4或时间戳，不验证slice |
| EncodeStream / DecodeStream | Conformant | 自有codec、同步背压、源EOF自动Flush到io.EOF；全部26份支持语料×Annex-B/AVCC×四种chunk对独立FFmpeg哈希，PCM/B/Lookahead尾部与时钟已测。MaxInputBuffer默认16MiB限制单输入，不是RSS总上限 |
| 取消与背压 | Conformant | 无worker/预取队列；Context端点与单帧操作边界检查，输入/输出阻塞取消及重复取消无泄漏测试。永久阻塞普通I/O须所有者中断或实现Context*接口，不能宣称任意io.Reader自动可取消 |
| h264enc / h264dec | Conformant | 文件/二进制管道、显式Annex-B/AVCC、参数与资源预检、stderr进度、信号取消、文件别名保护；实际子进程PCM像素测试。仍只生成/解码当前支持工具，无MP4/时间戳容器 |
| h264probe语法树 | Conformant | -syntax逐NAL JSONL，SPS/PPS/VUI/ScalingList/SEI/slice头部，Baseline/Main/High与ffprobe元数据和帧数对照；保留默认GOP断言，不解析残差 |
| 30秒内存稳定性 | Conformant | tools/streamcheck按独立PCM常量解析值及预设RSS/GC后堆阈值验收通过，预热5秒、每5秒采样；取消后goroutine回到1。证据见gates/m6_smoke.json；仅证明该输入的短时稳定性，不保证长期无泄漏 |

## 11. 明确不支持（第一版）

| 特性 | 原因 |
|---|---|
| MVC / SVC / MVCD / 3D-AVC | 独立扩展项目，不属核心 AVC |
| MP4 / RTP / WebRTC 封装 | 作为独立适配层，核心只提供 AU 接口 |
| RGB 转换 | 编解码链路只在 YUV 域定义，转换放库外 |
| SP / SI Slice 编码 | 仅计划解码支持（M7） |
| FMO / ASO 编码 | 仅计划解码支持（M7），编码收益低、复杂度高 |
