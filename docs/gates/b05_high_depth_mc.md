# B05b：10-bit分数像素MC SSE2

在B05a加权`29f25d8`之后独立增加高位深MC。decoder及encoder通过已有共享prediction入口
消费，444继续六抽头、420/422继续双线性及既有相位换算；不另写流水线内核。
新增MCLumaFractional16/MCChroma16实际槽，amd64有SSE2时选择；purego、其他架构及
非4倍数宽仍为原Generic。支持4/8/12/16宽、最多16行、亮度全16相位和色度全64相位。
整数相位沿用原复制；无10-bit Put/Avg融合，也没有新增AVX2/NEON插值声明。

## 数值和访存

Spec:8.4.2.2.1/8.4.2.2.2。亮度每批四个uint16，六抽头第一遍10-bit范围
[-10230,42966]，以int32保存；第二遍在未裁剪中间值上计算，+512后>>10才Clip1023。
保留单轴+16/>>5及qpel组合顺序，不把中间值压成int16。完整uint16旧入口域的保守
两遍绝对值52²×65535<2^28，也不会溢出。四分之一相位继续共享唯一组合控制。

色度以PMADDWL处理减32768后的采样，bias加回32768×64+32；四权重之和64，完整
uint16输入域也精确。结果减32768后有符号pack再翻回，避免SSE2缺少无符号dword pack。
不因输出为10-bit而新增输入裁剪。非零轴才访问一个色度halo或2/3亮度halo；
独立stride与最小末行len，不要求额外padding。FieldView的逻辑边界由既有领域patch保持。
无新增unsafe，静态noescape不保留scratch或调用者slice。

## 同机性能

HW-M2、Go1.27.4 windows/amd64、CGO0、GOAMD64v1、GOMAXPROCS16。
同版本Generic/SSE2全块入口对比，4/8/12/16宽、高16、src stride29/dst stride23；
包含全部scratch、插值及最终组合，源准备不计时。100ms×5、全部0B/0alloc，
全部64个格子的原始五样本见[b05_samples.json](b05_samples.json)。

| 16×16代表相位 | Generic→SSE2 ns/op中位 | 耗时下降 |
|---|---:|---:|
| 亮度(2,0) | 1008→218.8 | 78.3% |
| 亮度(0,2) | 911.8→224.8 | 75.3% |
| 亮度(2,2) | 1888→327.4 | 82.7% |
| 亮度(1,3) | 1873→466.6 | 75.1% |
| 亮度(3,2) | 2309→549.4 | 76.2% |
| 色度(3,0) | 294.2→102.1 | 65.3% |
| 色度(0,7) | 382.5→101.1 | 73.6% |
| 色度(3,7) | 539.7→101.0 | 81.3% |

亮度已测格快54.6%–82.7%，色度已测格快62.7%–84.0%。局部核收益不是端点FPS，
新High10 300帧与原8-bit主场景分别核对独立FFmpeg像素；见[B05收尾记录](b05_results.md)。

## 正确性与回退

全相位标量差分覆盖宽1/3的回退及4/8/12/16、行1/7/16、低两位纹理、0/1023交替、
全1023、完整uint16宽域、输入不变和未写区。新核有最小halo逐行保护页测试和局部数组
0分配断言；真实块入口测试另覆盖三种10-bit采样、负MV、内部/边界及帧/双场视图。
既有非均匀规范解析值、原生六格式/MBAFF/独立平面及外部Oracle继续通过。
CGO0全库门禁、purego/386/测试标签、共享核/decoder/encoder相关race和六目标交叉构建
通过；arm64仅证明可编译，不记作NEON执行。

复现：`go test ./internal/simd -run 'HighDepth' -count=1`；
`go test -tags=goavc_simdtest ./internal/prediction -run '^TestHighDepthBackend_' -count=1`；
`go test ./internal/simd -run '^$' -bench '^Benchmark(MC|MCChroma)HighDepth_Block_Backend$' -benchtime=100ms -count=5 -benchmem`。
常规命令均使用CGO_ENABLED=0。
