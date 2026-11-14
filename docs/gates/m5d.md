# M5d：GOP与码率控制本地门禁

日期：2026-11-14；Go1.27.4 windows/amd64、FFmpeg8.0，生产基线`fbcaad2`。
常规CGO=0，完整race使用CGO=1。全部原始数值与SHA-256见
[机器核验记录](./m5cd_results.json)，完整窗口/质量/内存/延迟见[性能报告§14](../performance.md)。

## 范围与退出证据

| 要求 | 实现与验证 |
|---|---|
| GOP/IDR/B位置 | GOPSize非IDR I刷新与独立IDRInterval；实际非IDR I有ffprobe断言。B1/2/3/16、强制IDR、规则PTS/Duration、DTS递增和尾部Flush已验证 |
| Lookahead | 1..16帧有界深拷贝，仅复制活动平面；未来窗口确认切换、抑制闪光和B锚点边界；背压/Reset/Close/失败重试及大stride所有权测试 |
| ABR | 720p2M与1080p4M各300帧，平均偏差分别+0.475360%/-0.769220%，均在±10%内；短120帧回归通过 |
| CBR | 两尺寸各300帧，十个30帧窗口完整公开；真实AU包含filler，分数FPS预算余数与静态60帧误差≤1%的Oracle通过 |
| VBV | 两尺寸各300帧，缓冲2M/4Mbit且初始半满；独立按逐AU比特和累计恒定排空重算全部300步，与轨迹逐项相等且不越界 |
| 反馈QP搜索 | I/P/B独立历史，只在成功提交后更新；相邻级指数夹紧后二分，冷启动全范围、无可行时穷举剩余QP；52起点×53单调边界、突变远处目标、失败事务和Reset测试 |
| 场景切换压力 | High96×64、120帧、B2/8×8/P权重/Lookahead/VBV；平移、40/80帧切换及60帧闪光，实际IDR仅0/40/80，逐AU独立充放水和三方像素Oracle |
| 大尺寸完整互操作 | ABR/CBR/VBV两尺寸共1800帧；本地重建=Go Decoder=FFmpeg，h264probe/ffprobe每组完整300帧、High、显示尺寸、IDR5/P100/B195、每图4slice与关键帧位置均正确 |
| M5总质量门禁 | 两尺寸×QP18/26/34全部通过原1.5dB绝对差，实测等码率匹配误差≤1%；PSNR/SSIM、码量和明显落后的等码率效率无选择性公开 |
| 测量与证据 | QP序列、每AU比特、VBV轨迹、PTS/DTS、30帧窗口、源码revision/dirty及DecoderVerified可观测；strict Resume和成功后原子发布报告，不接受空JSON作为完成 |

所有成功AU均在发布前检查Level/资源与VBV门禁；失败不修改参考、输出槽、
已接受队列或反馈历史。filler只补传输码量，不改VCL/重建，也不掩盖QP搜索方向。
原始rawvideo参考编码有估计时长提示；本次按固定FPS、实际300帧与完整YUV长度
复核，不将该提示混同为H.264码流解码错误。

## 工程验证与复现

最新CGO=0的gofmt/vet/build、非缓存全包test（codec30.680秒）、depscheck30包与
manifest27条均通过；编码实现完整非缓存race（codec681.840秒）后，仅新增诊断包，
该包独立补跑race1.243秒；最新六目标交叉构建通过。新增压力测试在完整test/race内
执行；不把旧race算作新包验证，也不把交叉编译描述为原生执行证据。
十二帧CBR稳态前后基准及CPU热点见performance.md§13。

入口为`scripts/verify_m5.ps1`，`-Mode quality`和`-Mode rate`。
用不同结果目录配合`-Sizes 720p`/`1080p`及`-RateModes abr`/`cbr`/`vbv`
可运行独立实例；最终必须收齐完整矩阵。所有长测构建均clean；质量与码控的
具体版本边界见M5c门禁，未使用停止的早期码控任务或历史旧工具的报告。

`cmd/h264probe`另有实际B1/2/3/16、独立GOP4/IDR7、多slice、资源限界及
错误期望测试；12份长产物全部执行图片/B深度/IDR断言并逐图核对POC显示映射。
其源码基线为`1f00d90`，不改变编码库，未以探测成功代替像素解码。

当前策略为单pass帧QP反馈，不生成HRD信令，不做多pass或完整B模式搜索；
没有把VBV本地模型写成HRD时序认证。完整Profile仍为Partial，等码率质量和
标量吞吐仍有改进空间，30FPS保留为M8目标。仓库未配置远端，远端CI未执行。

本地阶段标签：`v0.6.0`。M5a/b/c/d四个子阶段的本地门禁均已关闭；
远端CI状态单独保留，不将本地收尾误写为远端验证通过。
