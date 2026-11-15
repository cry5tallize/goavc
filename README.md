# goavc

纯 Go 的 H.264/AVC 编解码库，用于在 Go 程序中将原始 YUV 帧编码为 H.264，或将 H.264 码流解码为 YUV 帧。运行时只依赖标准库，不需要 CGO、FFmpeg 或其他外部编解码库。

- 原生 8/10-bit YUV420、YUV422、YUV444。
- CAVLC/CABAC、I/P/B 帧，CQP/ABR/CBR/VBV 码率控制。
- 帧级 Send/Receive API，以及基于 `io.Reader` / `io.Writer` 的流式适配。
- Annex-B、AVCC 和访问单元（AU）组装；可用时按内核选择 SIMD 实现，保留纯 Go 回退。

## 安装

需要 **Go 1.27 或更高版本**。

```bash
go get github.com/cry5tallize/goavc
```

使用本地源码时，可在调用方模块中添加 `replace github.com/cry5tallize/goavc => /path/to/goavc`。

| 包 | 用途 |
|---|---|
| `github.com/cry5tallize/goavc` | `Frame`、`Plane`、`Packet`、配置及错误值 |
| `github.com/cry5tallize/goavc/encoder` | 帧级编码器 |
| `github.com/cry5tallize/goavc/decoder` | 帧级解码器 |
| `github.com/cry5tallize/goavc/stream` | YUV 读写、码流封装、AU 组装与流式编解码 |

## 编码 YUV 帧

下面是完整程序：创建一张 64×64 灰色 YUV420P 图像，将它编码为 `out.264`。保存为 `encode/main.go`，执行 `go run ./encode`。

```go
package main

import (
	"errors"
	"log"
	"os"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/encoder"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg := h264.DefaultEncoderConfig()
	cfg.Width, cfg.Height = 64, 64
	cfg.QP = 26

	enc, err := encoder.New(cfg)
	if err != nil {
		return err
	}
	defer enc.Close()

	frame, err := h264.NewFrame(cfg.Width, cfg.Height, cfg.PixelFormat)
	if err != nil {
		return err
	}
	defer frame.Release()
	for _, plane := range frame.Planes {
		for i := range plane.Data {
			plane.Data[i] = 128
		}
	}
	frame.Duration = 1

	if err := enc.SendFrame(frame); err != nil {
		return err
	}
	if err := enc.Flush(); err != nil {
		return err
	}

	out, err := os.Create("out.264")
	if err != nil {
		return err
	}
	defer out.Close()
	for {
		packet, err := enc.ReceivePacket()
		if errors.Is(err, h264.ErrEOF) {
			return out.Close()
		}
		if err != nil {
			return err
		}
		if _, err := out.Write(packet.Data); err != nil {
			return err
		}
	}
}
```

默认配置使用 Baseline、CAVLC、CQP，不启用 B 帧。每个输出 `Packet.Data` 是一个完整的 Annex-B AU；IDR 的 AU 包含 SPS/PPS。
连续编码时，每次送帧后循环接收至 `ErrAgain`，全部输入结束后再 `Flush` 并接收至 `ErrEOF`。启用 B 帧或 Lookahead 后，不能假定一进一出。
Profile、B 帧、8×8 变换、权重预测与码控选项见[编码 API](docs/encoder.md)。

## 解码 H.264 文件

这个完整程序把 Annex-B 文件 `out.264` 解码为紧凑平面的 `decoded.yuv`。保存为 `decode/main.go`，执行 `go run ./decode`；也可以直接读取上一个示例的输出。

```go
package main

import (
	"context"
	"errors"
	"io"
	"log"
	"os"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/stream"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	in, err := os.Open("out.264")
	if err != nil {
		return err
	}
	defer in.Close()

	nals, err := stream.NewAnnexBReader(in, 0)
	if err != nil {
		return err
	}
	src, err := stream.NewAccessUnitReader(nals, stream.AccessUnitConfig{})
	if err != nil {
		return err
	}

	out, err := os.Create("decoded.yuv")
	if err != nil {
		return err
	}
	defer out.Close()
	dst, err := stream.NewYUVWriter(out)
	if err != nil {
		return err
	}

	err = stream.DecodeStream(context.Background(), src, dst, stream.DecodeConfig{
		Decoder: h264.DefaultDecoderConfig(),
	})
	// DecodeStream 在正常 EOF 后自动 Flush、排空输出并释放帧，最终返回 io.EOF。
	if !errors.Is(err, io.EOF) {
		return err
	}
	return out.Close()
}
```

默认输出为 YUV420P。读取其他采样格式时，设置 `DecoderConfig.OutputFormat` 为对应的 `h264.YUV422P`、`h264.YUV444P` 或 10-bit LE 格式。
原始 YUV 不含宽高或格式信息，调用方需要自行保存这些信息。

需要逐帧处理图像时，使用 `decoder.New`、`SendPacket` 和 `ReceiveFrame`，对每个成功接收的帧处理后调用 `Release()`。
`SendPacket` 的输入必须是完整 Annex-B AU，不能逐个 NAL 发送，也不能把任意多帧文件当作一个 Packet；上面的 `NewAccessUnitReader` 负责组装边界。
AVCC 输入和自定义帧处理见[流式 API](docs/stream.md)。

## 支持范围

| 能力 | 当前范围 |
|---|---|
| 像素格式 | `YUV420P`、`YUV422P`、`YUV444P` 及对应的 `P10LE` 格式 |
| 编解码工具 | 双熵编码、I/P/B、4×4/8×8 变换、缩放矩阵、去块；组合受 Profile 约束 |
| 码率控制 | CQP、ABR、CBR、VBV |
| 隔行与扩展工具 | 限定 MBAFF；FMO/ASO、分区、SP/SI、冗余等按支持矩阵区分编解码能力 |
| 封装 | Annex-B、AVCC、avcC 与 AU 适配；不包含 MP4、RTP、WebRTC |
| 平台与执行 | 纯 Go 回退，amd64 SSE2/AVX2、arm64 NEON；单实例串行 |

Baseline、Main、High 及高位深 Profile 均有已实现的工具组合，**不是完整 Profile 一致性认证**。
PAFF、transform bypass、单色和 9/12/14-bit 像素流水线未支持；RGB 转换由调用方完成。
arm64 已交叉编译，尚未原生验证。完整组合与已知限制见[支持矩阵](docs/supported-features.md)，吞吐与分配见[性能报告](docs/performance.md)。

## API 注意事项

- **帧布局**：平面顺序为 Y、U、V，`Plane.Stride` 的单位是字节；10-bit 采样以 16-bit 小端存储，有效值为 0..1023。外部缓冲可以直接构造 `Frame`，送入前调用 `Validate()`。
- **所有权**：`SendFrame` 返回后可复用或释放输入；`SendPacket` 返回后可复用输入 Data。接收的 Frame 用完须 `Release()`，需要独立副本时 `Clone()`；接收的 Packet.Data 归调用方。
- **输出排空**：输入与输出不是一一对应。持续接收到 `ErrAgain`，输入结束后 `Flush` 并接收到 `ErrEOF`，避免丢失 B 帧或其他延迟输出。
- **错误判断**：使用 `errors.Is`。`ErrAgain` 是流控，`ErrEOF` 表示结束，`ErrUnsupported` 是合法但未支持的能力，`ErrInvalidData` 是非法数据，`ErrResourceLimit` 是超出资源预算。
- **并发**：同一 Encoder/Decoder 不得并发调用，`Threads>1` 当前不支持；多路视频使用独立实例。
- **I/O 生命周期**：流式 API 不关闭调用方的 Reader/Writer。可能阻塞的网络或管道 I/O，其取消机制由调用方配合实现，详见[取消与阻塞 I/O](docs/stream.md#取消与阻塞-io)。

## 命令行工具

仓库也提供文件编解码和码流探测工具：

```bash
go install github.com/cry5tallize/goavc/cmd/h264enc
go install github.com/cry5tallize/goavc/cmd/h264dec
go install github.com/cry5tallize/goavc/cmd/h264probe

h264enc -input input.yuv -output out.264 -width 640 -height 360 -profile high -qp 26
h264dec -input out.264 -output decoded.yuv
h264probe -input out.264
```

更多参数见[CLI 用法](docs/stream.md#cli)。

## 文档

- [编码配置与选项](docs/encoder.md)
- [流式 API、封装适配与 CLI](docs/stream.md)
- [支持范围](docs/supported-features.md) · [一致性报告](docs/conformance.md) · [性能报告](docs/performance.md)
- [版本记录](CHANGELOG.md)
- 开发者资料：[架构](docs/architecture.md) · [实现状态](docs/implementation-status.md) · [路线图](docs/Roadmap.md)

## 许可证

[MIT](LICENSE)。代码许可不授予 H.264/AVC 专利许可，使用者需独立评估适用的专利义务，见 [PATENTS.md](PATENTS.md) 和[许可说明](docs/licensing.md)。
