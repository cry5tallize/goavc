# 一致性与 M5 质量门禁

M5 开工时固定以下测量协议，结果不得通过放宽阈值或选择性报告变成通过。

- 规范正确性：同一码流的 FFmpeg 输出、自研 Decoder 输出、Encoder 本地重建必须逐像素相等。
- 连续视频：720p 与 1080p 各至少 300 帧，30 FPS、8-bit YUV420、固定输入 SHA-256。
  基础序列使用 FFmpeg `testsrc2`，另报告平移纹理与场景切换定向序列。
- 等 QP：QP=18/26/34，x264 `-preset veryfast -tune psnr`，与被测编码器使用相同 Profile、
  分辨率、帧率、GOP、B 帧数和输入。整段 Y-PSNR 相差不得超过 1.5 dB；U/V、码率同时报告。
- 等码率：使用实测平均码率匹配，公开 PSNR 差异，不设隐藏筛选条件。
- ABR：长序列平均码率偏差不超过目标 ±10%；CBR 报告窗口波动；VBV 检查每帧充放水与上下界。
- 输出顺序、PTS/DTS、Flush、GOP/IDR/B 参数必须与实际码流一致，不以枚举或配置存在代替实现。

M5a/b/c/d 分别记录独立验证证据；子阶段通过不等同 M5 总门禁通过。
FFmpeg/x264 仅供开发期 Oracle 和质量测量，运行时仍为纯 Go、仅标准库。

## 可复现的M5长门禁入口

Windows PowerShell（结果目录须有足够原始YUV空间，建议至少25GiB）：

```powershell
.\scripts\verify_m5.ps1 -OutputDirectory C:\validation\m5 -Mode oracle
.\scripts\verify_m5.ps1 -OutputDirectory C:\validation\m5 -Mode quality -Resume
.\scripts\verify_m5.ps1 -OutputDirectory C:\validation\m5 -Mode rate -Resume
```

脚本固定720p/1080p各300帧、High/CABAC、B2、GOP/IDR60、4slice、8×8、
P显式权重和Lookahead2。同QP x264额外固定`ipratio=pbratio=1.0`、b-adapt=0、
b-pyramid=none、scenecut=0、ref=2、threads=1，避免I/P/B QP比例与帧型决策差异
混入比较。所有QP测量均完成后统一返回门禁状态，保留失败项，不放宽1.5dB阈值。

匹配码率按Go实测码率迭代x264目标（最多四次修正），报告实测误差；超过1%时不能
称为等码率结果。文件存在不是验证证据：`-Resume`核对配置、完整帧数、输入/码流/
重建SHA-256；需要Go Decoder验证的调用还必须存在`DecoderVerified=true`标记。
该标记来自逐帧比较，不替代独立FFmpeg像素Oracle。Windows PowerShell的原生stderr
重定向按FFmpeg真实退出码处理，日志保留用于审计。

当前测量器还记录逐AU的PTS/DTS、比特数、VBV fullness与实际缓冲大小。
码控脚本从初始半满缓冲和累计恒定排空量独立重算每一步VBV，逐项核对报告，
不只检查生产代码提供的最小/最大值。所有质量项也执行三方像素检查。
`-Resume`还要求源码revision、clean构建、8×8/权重/Lookahead/FPS/slice配置及
完整轨迹吻合；成功退出后才将`.pending`报告发布为JSON。
为缩短等待，可在不同结果目录分别用`-Sizes 720p`/`-Sizes 1080p`、
`-RateModes abr`/`cbr`/`vbv`运行独立实例；完整验收仍须收齐全部矩阵。

`cmd/h264probe`以标准库和共享syntax/NAL解析实际参数集、POC、帧型、
每图slice数，支持`-expect-frames`、`-expect-max-b`和`-expect-idr-interval`
失败即非零退出。长门禁脚本对新结果和Resume均执行该断言；探测仅校验
头部，不代替像素Oracle。实际B1/2/3/16、独立GOP4/IDR7、多slice、
资源上限与错误期望均有测试；全部12份长码流已执行探测并与ffprobe核对。
