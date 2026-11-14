# M5b：RD 模式决策本地门禁

日期：2026-11-14；HW-M2，Go1.27.4 windows/amd64，FFmpeg/ffprobe8.0。
RD/等价剪枝生产基线 `3ba066f37e60b83612bf6d2db13ed46312db8078`，
后续 Annex A 边界修复 `37c41bb`。普通构建与工具均 CGO=0，race 单独 CGO=1。

## 实现与退出证据

| 要求 | 证据 |
|---|---|
| SATD Hadamard | `internal/transform/satd_test.go` 常量、脉冲、正负极值、8/16块和非对齐 stride 的解析解 |
| 按 QP 的 RD 代价 | Q8 整数 lambda、SSE+lambda×bits；QP0..51严格单调、整数平方根边界测试 |
| MVD 与头部位数估计 | SE/UE/MVD 精确长度与 writer 相等；运动搜索回调同时考虑 MVD 比特；宏块残差直接试写真实 CAVLC |
| 全模式比较 | Skip、四种 P 分区、I4、I16、PCM；`MatchesExhaustiveCandidateCosts` 独立枚举所有合法候选，验证最低 J；QP0/26/51、静态/棋盘/纹理/纯色 |
| 早期 Skip / terminate | D>=0 与最少语法位数下界；常量源/预测和完整插值 halo 的支配证明。关闭全部剪枝后候选结构相等，300帧码流哈希相同 |
| 模式实际选择 | I4九种、I16/色度各四种仍有实际决策覆盖；四种 Inter 分区逐4×4矢量与输入运动场相等；场景变化实际选择 P picture 内 I16 |
| 活动度 QP | `WithAdaptiveQP(0..6)`，平坦块降低、高活动增加，QP裁剪到0..51；实际QP23/29，36帧独立 Oracle；与显式回调/PCM冲突拒绝 |
| 本地重建与状态 | 矢量、模式、NZ、像素完整恢复；参考只读；CAVLC/共享预测、反量化、逆变换、滤波不重复实现；既有360帧和其他短序列 Oracle回归 |
| 质量对比报告 | 720p、300帧、QP26 同参数 x264 veryfast/tune psnr：Y-PSNR49.225/48.310，差0.915dB，符合1.5dB门槛；同时公开 Go 约4.3%码率增加，见 performance.md §9 |

R 中 skip_run 使用独立宏块开销估计，因此只承诺该候选模型的最小 J，不宣称全 slice
全局最优。QP18/34、1080p、Main/High 质量与等码率报告仍待后续 M5；本阶段不代表总门禁通过。

## 长序列与验证

显式运行 `H264_LONG_TEST=1 go test -run '^TestEncoder_InterLongOracle$' -count=1 -v -timeout 30m ./tests/codec`。
Baseline、QP26、30FPS、GOP30、四 slice；720p/1080p各300帧，三方逐像素/哈希一致，Flush→EOF。
下列结果来自剪枝基线3ba066f；正采样且QP26的输入不触发后续Profile边界修复。

| 尺寸 | 输入 SHA-256 | Annex-B SHA-256 | 本地重建=FFmpeg SHA-256 | 字节 |
|---|---|---|---|---|
| 1280×720 | `dc1c14f1d0063c2743152dd81eef293a2b668a9cf00c29817104cf07994614e9` | `6469319f99d81bbf710b3762e6f05de63f8950e6ddfde2b37a955870b58f042f` | `3e0335bf28da9beece96f76ca44814468da1a6e4d4d782b120547cf6e1187581` | 6,761,461 |
| 1920×1080 | `a252864539b1817e9a3886014a55ac085739abcf31baf49269616fe79bc65336` | `ab50e42348b1028bedf8e4a55bde2ba76ba17940ae6cdb8aff068d8b7039c230` | `2ce779b666b344492ec4fed164cc399200d9b9d4e492badb57ab443e8c7ce88f` | 15,038,969 |

- 修复后完整 gofmt/vet/build/`go test -count=1 ./...`、depscheck29包、manifest27条、diff检查通过。
- `CGO_ENABLED=1 go test -race -count=1 ./...` 全包通过。
- 参考帧 fuzz10秒2,673次；帧内/PCM fuzz5秒21,850次，通过。低QP受限输入在明确拒绝后重试合法输入，仍检查重建/自解码一致和无部分输出。
- 六目标 Windows/Linux/macOS × amd64/arm64 CGO=0 build通过；其他平台未做原生执行。
- 本地标签 `v0.5.2`，远端CI尚未执行。

## Profile 边界审计

依据 Annex A.2.1/A.2.2，Baseline/Main 不能使用值为0的PCM采样或超过15的CAVLC
level_prefix。此前的 PCM 互操作测试能被 FFmpeg 解码，不能代替此 Profile 约束。
修复先新增拒绝测试并观察失败，再限制候选与发布事务；测试同时保留合法 PCM 精确比较。
极低QP、极端色度且无法选择合法候选时返回 ErrUnsupported，调用方可调整QP或输入后重试。
没有截断系数、改写像素或自动切换Profile。完整Profile仍为Partial。

当前 M5c 的 CABAC/Main/High/B/8×8 与 M5d 的完整GOP/ABR/CBR/VBV、CPU profile 和
其余质量对比尚未完成；不能把 `v0.5.2` 解释为完整M5版本。
