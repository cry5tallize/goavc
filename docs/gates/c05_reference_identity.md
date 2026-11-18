# C05：参考身份令牌可行性实验（未迁移生产布局）

父版本为C04 `1136482`。本提交只交付独立测试、基准和留存探针，生产仍使用
`[2][]*dpb.Identity`。**C05完整迁移验收尚未完成**，不能把实验数组减半称作decoder
已经省内存或提速。后续是否迁移仍须满足计划的完整端点、live heap与GC门禁。

复评结果：在真实 High/High10 的相同 GOP 采样位置，理想进程活跃堆降幅不到 5%，本轮不迁移；见 [DPB 驻留账本](c05_dpb_residency.md)。

## 身份与所有权

实验以每张图像独占的字典把原身份指针映射到uint32，0表示无参考；两列表共用字典，
不能直接比较不同图像字典里的同值令牌。记录仍引用原Identity，不复制长期标志快照，
不以ref_idx、POC或ID数值替代指针身份。原ID/POC相同的不同身份也保持不同。
没有全局map或令牌池，没有修改公共API或运动网格布局。

`TestReferenceTokens_PictureLocalIdentity`覆盖slice重排后的同ref_idx不同图像、
跨列表同图、无参考、原帧/顶场/底场、MMCO式长期标志更新及4096次后续字典复用。
仍被旧共位块引用的字典不复用，旧场身份继续指向同一原帧身份。
这些是隔离模型测试，不是完整DPB/SPS切换或错误恢复的迁移证据。

## 实验计量边界

1080p编码尺寸120×68宏块、130560个4×4块，两列表、16份身份。
`BenchmarkReferenceIdentity_1080p_Layout`把字典构造、两列表分配、分块填充、相邻身份
比较及每宏块一次解析都计入每op；令牌路径在slice级预先绑定列表，不逐4×4块查map。
它是合成数据流，不包含完整MV访问、Direct/去块/残差及实际图像输出。

`TestReferenceTokens_RetainedHeapProbe`仅在显式环境变量下运行：每个独立进程保存八张
图像的身份元数据，建立后解除map，保留稳定记录和两列表；强制GC后读取HeapAlloc及
`/gc/scan/heap:bytes`，再解除全部所有者、GC并记录残留差值。
该scan指标是**可扫描堆字节**，不是实际GC扫描工作量、暂停或CPU耗时。
不将强制GC实验的差值解释为自然GC延迟改善，不用测量噪声下的负差值宣称负内存。

正式五样本及五个独立进程的全部差值见[c05_samples.json](c05_samples.json)。
早期150ms×5只打印到会话输出，未保存日志，不用于正式量化结论。

环境为Ryzen7 5700X、Go1.27.4 Windows/amd64、CGO0、GOAMD64v1、GOMAXPROCS16。
200ms×5中位：指针1878829ns/2097167B/2alloc，令牌497485ns/1049488B/11alloc。
分配字节约减半，但对象数增加；不能只报告前者。八图留存五个独立进程中位如下：

| GC后差值 | 指针 | 令牌 |
|---|---:|---:|
| 活跃HeapAlloc（字节） | 16810416 | 8407544 |
| 可扫描heap（字节） | 16785080 | 8792 |
| 释放全部所有者后的HeapAlloc差（字节） | 27568 | 12280 |
| 释放全部所有者后的可扫描heap差（字节） | 7064 | 2328 |

小残留包含运行时/测试统计噪声，不是字典自身泄漏或自然GC改善证明。

## 迁移前仍须处理

- `frameState.setReferenceIdentity`成功提交时绑定稳定令牌，覆盖P/B分区、Direct、MBAFF和独立平面。
- 去块比较只使用同图字典令牌，共位块跨图解析需借用对应DPB字典，不能混用编号。
- `dpb.remove`、失败表示、Reset/Close、SPS/格式切换须同时释放令牌表和小型身份记录。
  当前`Identity.Frame`仅保留轻量身份，不可改成保留Reference/Picture的链。
- 增量字典需要按实际slice集合/资源预算限制，不能假定整图最多16份历史身份；
  DPB容量16不等于跨slice历史身份的永久上限。
- 新布局需纳入实际资源账本；386指针与uint32等宽，不能宣传其数组字节减半。
- 将映射建立/清理、GC和生命周期成本纳入原High/High10及留出完整端点，
  达到计划门槛后才采纳；未实测的速度和内存结果不预填。

复现（CGO_ENABLED=0）：

```text
go test ./decoder -run '^TestReferenceTokens_PictureLocalIdentity$' -count=1
go test ./decoder -run '^$' -bench '^BenchmarkReferenceIdentity_1080p_Layout$' -benchtime=200ms -count=5 -benchmem
```

留存探针需在独立进程分别设置`GOAVC_IDENTITY_LAYOUT=pointer`和`token`，执行
`go test ./decoder -run '^TestReferenceTokens_RetainedHeapProbe$' -count=1 -v`。

验证：CGO0完整gofmt/vet/build/test、depscheck和manifest，受影响bitstream/CABAC/decoder
的purego及386回归、bitstream/CABAC/DPB/decoder相关race均通过；无新增汇编或并发实现。
显式留存探针在普通测试中跳过，正式结果来自上述环境变量启用的独立进程。
