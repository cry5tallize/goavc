# 版本记录

## v1.0.2 — 2026-11-17

- Direct运动补偿合并参考和双列表MV相同的像素任务，保留原逐4×4运动元数据。
- B预测增加单参考无权重直写、均值块快路与块级权重准备，继续调用共享MC内核。
- 4×4零/DC残差跳过完整逆变换，反量化错误校验与原生8/10-bit裁剪保持不变。
- 固定共享预测跨包调用边界，消除局部帧内邻域逃逸；删除重复运动网格Reset。
- CABAC多比特归一化批量读取，单比特直读；截断位置、lastBit、终止与PCM重启语义不变。
- 整帧网格访问复用线性下标并减少slice地址推导，保留mapped slice、nil与越界语义。
- 各项独立提交；同环境五次中位耗时：1080p High IDR下降12.7%，32帧I/P/B下降46.1%。
  IDR端点分配从23,801降至115次/op。完整数据和测量范围见[性能报告 §18](docs/performance.md)。
- 完整常规门禁、受影响包purego/386/race及CABAC/decoder短fuzz通过；独立FFmpeg像素结果一致。
- 本次不扩展Profile、位深或并发支持；32帧短样本不替代300帧主矩阵或长期实时承诺。

## v1.0.1 — 2026-11-16

- 模块路径统一为 `github.com/cry5tallize/goavc`，同步更新全部源码/测试导入、README 安装和示例、CLI 安装命令及开发文档。
- 根包名保持 `h264`；可使用 `h264 "github.com/cry5tallize/goavc"` 导入，不改变公开类型、方法或编解码行为。
- 原 `v1.0.0` 标签保持不变，新模块路径从 `v1.0.1` 版本开始使用。

## v1.0.0 — 2026-11-16

本地版本收尾，基于 v0.9.5 已有实现整理使用说明、支持边界和发布记录，不新增算法或公共 API。版本提交后创建注释标签 `v1.0.0`，不执行 push 或远端 release。

### 已有能力

- 纯 Go、零 CGO、标准库运行时；共享预测、变换、重建和滤波内核。
- 六种 8/10-bit YUV420/422/444 原生编解码，CAVLC/CABAC I/P/B，CQP/ABR/CBR/VBV；具体工具组合见[支持矩阵](docs/supported-features.md)。
- Annex-B/AVCC、完整 AU 适配、同步流式编解码、Flush、背压和取消；`h264enc`、`h264dec`、`h264probe` 可直接处理文件。
- 按槽选择 SIMD/Generic 后端、有界空闲缓冲缓存、串行工作区复用和已有热路径分配断言。

### 本次整理

- README 更新为当前版本，补齐本地安装、三个 CLI 的用法及帧级/流式 API 入口。
- 集中说明 Frame/Packet 所有权、单实例并发边界与错误处理。
- 复用[一致性报告](docs/conformance.md)、[性能报告](docs/performance.md#16-m8测试与测量收尾)和原有测试，不新增发布框架或长测任务。
- 实际运行三个 CLI：32×32 单帧 High/YUV420P 编码、解码和图片数探测成功；常量 128 的所有输出采样与输入一致，临时文件已清理。常规验证结果见[实施状态](docs/implementation-status.md#m9)。

### 使用边界

- 完整 Profile 仍为 Partial；未支持工具继续显式报错，不因版本号变更扩大兼容性声明。
- 单实例非并发安全，内部 slice/前瞻/ME worker 未实现，`Threads>1` 仍拒绝。
- PAFF、transform bypass、单色和 9/12/14-bit 像素流水线不支持；MBAFF、FMO/ASO、SP/SI、分区和独立色平面沿用支持矩阵的限定组合。
- arm64 仅有交叉编译结果，未原生执行；8 小时混合输入长稳及远端 CI 未执行，不宣称完整生产认证。
- 不保证所有配置实时；已测 1080p High 解码与 720p High 编码仍低于 30 FPS，具体数值保留在性能报告中。
- MIT 代码许可不提供 H.264 专利授权，见 [PATENTS.md](PATENTS.md)。

## v0.9.5 — 2026-11-16

M8 授权范围内的实现、测试与性能记录收尾。详细变更及前后测量见 [M8 实施记录](docs/m8-development/12_execution_log.md)和[性能报告](docs/performance.md)。
