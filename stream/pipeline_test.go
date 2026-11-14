package stream_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/encoder"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/stream"
)

type packetSink func(h264.Packet) error

func (f packetSink) WriteAccessUnit(p h264.Packet) error { return f(p) }

type frameSink func(*h264.Frame) error

func (f frameSink) WriteFrame(p *h264.Frame) error { return f(p) }

type frameSource func() (*h264.Frame, error)

func (f frameSource) ReadFrame() (*h264.Frame, error) { return f() }

type packetSource func() (h264.Packet, error)

func (f packetSource) ReadAccessUnit() (h264.Packet, error) { return f() }

func rawFrame(index int) *h264.Frame {
	f := &h264.Frame{Width: 16, Height: 16, Format: h264.YUV420P, PTS: int64(index), Duration: 1}
	for c := range f.Planes {
		w, h := f.Format.PlaneDimensions(c, 16, 16)
		f.Planes[c] = h264.Plane{Data: bytes.Repeat([]byte{byte(30 + index + c)}, w*h), Stride: w}
	}
	return f
}
func smallConfig() h264.EncoderConfig {
	c := h264.DefaultEncoderConfig()
	c.Width, c.Height = 16, 16
	return c
}

func TestPipeline_PCMBytesFlushAndOwnership(t *testing.T) {
	var inputFrames []*h264.Frame
	src := frameSource(func() (*h264.Frame, error) {
		if len(inputFrames) == 4 {
			return nil, io.EOF
		}
		f := rawFrame(len(inputFrames))
		inputFrames = append(inputFrames, f)
		return f, nil
	})
	var encoded bytes.Buffer
	dst, _ := stream.NewAnnexBWriter(&encoded, stream.AnnexBWriterConfig{})
	err := stream.EncodeStream(context.Background(), src, dst, stream.EncodeConfig{Encoder: smallConfig(), Options: []encoder.Option{encoder.WithPCM()}})
	if err != io.EOF {
		t.Fatal("编码未自动排空", err)
	}
	for _, f := range inputFrames {
		if !errors.Is(f.Validate(), h264.ErrInvalidData) {
			t.Fatal("输入未释放")
		}
	}
	nr, _ := stream.NewAnnexBReader(bytes.NewReader(encoded.Bytes()), 0)
	ar, _ := stream.NewAccessUnitReader(nr, stream.AccessUnitConfig{})
	var got bytes.Buffer
	yw, _ := stream.NewYUVWriter(&got)
	if err := stream.DecodeStream(context.Background(), ar, yw, stream.DecodeConfig{}); err != io.EOF {
		t.Fatal(err)
	}
	var want bytes.Buffer
	for i := range 4 {
		f := rawFrame(i)
		for _, p := range f.Planes {
			want.Write(p.Data)
		}
		f.Release()
	}
	if !bytes.Equal(got.Bytes(), want.Bytes()) {
		t.Fatal("PCM规范解析值或末帧丢失")
	}
}

func TestPipeline_BLookaheadTailAndClock(t *testing.T) {
	cfg := smallConfig()
	cfg.Profile = h264.ProfileHigh
	cfg.MaxBFrames = 2
	cfg.GOPSize = 60
	index := 0
	src := frameSource(func() (*h264.Frame, error) {
		if index == 8 {
			return nil, io.EOF
		}
		f := rawFrame(index)
		index++
		return f, nil
	})
	var pts, dts []int64
	dst := packetSink(func(p h264.Packet) error { pts = append(pts, p.PTS); dts = append(dts, p.DTS); return nil })
	err := stream.EncodeStream(context.Background(), src, dst, stream.EncodeConfig{Encoder: cfg, Options: []encoder.Option{encoder.WithLookahead(2)}})
	if err != io.EOF {
		t.Fatal(err)
	}
	// 两个非参考B，先交付未来P锚点，Flush必须交付不足一组的尾帧。
	want := []int64{0, 3, 1, 2, 6, 4, 5, 7}
	if fmt.Sprint(pts) != fmt.Sprint(want) {
		t.Fatal("B/Lookahead尾部或顺序不符", pts)
	}
	for i, d := range dts {
		if d != int64(i-2) {
			t.Fatal("DTS错误", dts)
		}
	}
}

func TestPipeline_BudgetsErrorsAndRelease(t *testing.T) {
	for _, limit := range []int{1, 383} {
		f := rawFrame(0)
		n := 0
		err := stream.EncodeStream(context.Background(), frameSource(func() (*h264.Frame, error) { n++; return f, nil }), packetSink(func(h264.Packet) error { t.Fatal("超限写出"); return nil }), stream.EncodeConfig{Encoder: smallConfig(), MaxInputBuffer: limit})
		if !errors.Is(err, h264.ErrResourceLimit) || n != 1 || f.Validate() == nil {
			t.Fatal("帧预算或释放失效", err, n)
		}
	}
	r, _ := stream.NewYUVReader(bytes.NewReader([]byte{1}), 16, 16, h264.YUV420P)
	err := stream.EncodeStream(context.Background(), r, packetSink(func(h264.Packet) error { return nil }), stream.EncodeConfig{Encoder: smallConfig(), MaxInputBuffer: 383})
	if !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatal("内置源未在读取前限界", err)
	}
	p := h264.Packet{Data: make([]byte, 17)}
	err = stream.DecodeStream(context.Background(), packetSource(func() (h264.Packet, error) { return p, nil }), frameSink(func(*h264.Frame) error { t.Fatal("超限输出"); return nil }), stream.DecodeConfig{MaxInputBuffer: 16})
	if !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatal("AU预算失效", err)
	}
	boom := errors.New("传输失败")
	for _, readErr := range []error{boom, errors.Join(h264.ErrInvalidData, io.EOF)} {
		err = stream.EncodeStream(context.Background(), frameSource(func() (*h264.Frame, error) { return nil, readErr }), packetSink(func(h264.Packet) error { return nil }), stream.EncodeConfig{Encoder: smallConfig()})
		if !errors.Is(err, readErr) {
			t.Fatal("源错误被吞掉或误Flush", err)
		}
	}
	f := rawFrame(0)
	err = stream.EncodeStream(context.Background(), frameSource(func() (*h264.Frame, error) { return f, nil }), packetSink(func(h264.Packet) error { return boom }), stream.EncodeConfig{Encoder: smallConfig()})
	if !errors.Is(err, boom) || f.Validate() == nil {
		t.Fatal("Writer错误或帧释放错误", err)
	}
}

type chunked struct {
	io.Reader
	n int
}

func (r chunked) Read(b []byte) (int, error) { return r.Reader.Read(b[:min(len(b), r.n)]) }

func TestPipeline_CorpusChunkOracle(t *testing.T) {
	var manifest struct {
		Entries []struct{ Path, Status, ExpectedYUVSha256 string }
	}
	data, err := os.ReadFile("../testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	// 使用已登记独立FFmpeg输出哈希；覆盖所有受支持语料，含B/High/多slice/裁剪。
	for _, e := range manifest.Entries {
		if e.Status != "conformant" {
			continue
		}
		data, err := os.ReadFile(filepath.Join("..", e.Path))
		if err != nil {
			t.Fatal(err)
		}
		for _, format := range []string{"annexb", "avcc"} {
			input := data
			if format == "avcc" {
				input = nil
				units, err := nal.SplitAnnexB(data)
				if err != nil {
					t.Fatal(err)
				}
				for _, u := range units {
					n := len(u.Payload) + 1
					input = append(input, byte(n>>24), byte(n>>16), byte(n>>8), byte(n), u.Header.Byte())
					input = append(input, u.Payload...)
				}
			}
			for _, size := range []int{1, 7, 4096, len(input)} {
				t.Run(fmt.Sprintf("%s/%s/%d", filepath.Base(e.Path), format, size), func(t *testing.T) {
					var nr stream.NALReader
					var err error
					source := chunked{bytes.NewReader(input), size}
					if format == "annexb" {
						nr, err = stream.NewAnnexBReader(source, 0)
					} else {
						nr, err = stream.NewAVCCReader(source, 4, 0)
					}
					if err != nil {
						t.Fatal(err)
					}
					ar, err := stream.NewAccessUnitReader(nr, stream.AccessUnitConfig{})
					if err != nil {
						t.Fatal(err)
					}
					hash := sha256.New()
					yw, _ := stream.NewYUVWriter(hash)
					err = stream.DecodeStream(context.Background(), ar, yw, stream.DecodeConfig{})
					if err != io.EOF {
						t.Fatal("分块解码/尾部Flush失败", err)
					}
					if hex.EncodeToString(hash.Sum(nil)) != e.ExpectedYUVSha256 {
						t.Fatal("与独立Oracle显示顺序像素不符")
					}
				})
			}
		}
	}
}

// context端点直接等待ctx，不创建不可回收的后台Read goroutine。
type cancelFrameSource struct{ entered chan struct{} }

func (cancelFrameSource) ReadFrame() (*h264.Frame, error) { panic("不得调用非context路径") }
func (r cancelFrameSource) ReadFrameContext(ctx context.Context) (*h264.Frame, error) {
	close(r.entered)
	<-ctx.Done()
	return nil, ctx.Err()
}

type cancelPacketSource struct{ entered chan struct{} }

func (cancelPacketSource) ReadAccessUnit() (h264.Packet, error) {
	panic("不得调用非context路径")
}
func (r cancelPacketSource) ReadAccessUnitContext(ctx context.Context) (h264.Packet, error) {
	close(r.entered)
	<-ctx.Done()
	return h264.Packet{}, ctx.Err()
}

func TestPipeline_CancellationHasNoWorkers(t *testing.T) {
	before := runtime.NumGoroutine()
	for range 100 {
		for _, encode := range []bool{true, false} {
			ctx, cancel := context.WithCancel(context.Background())
			// 在读端点内取消；不依赖调度延迟，测试本身只启动有明确join的调用者。
			done := make(chan error, 1)
			entered := make(chan struct{})
			go func() {
				if encode {
					done <- stream.EncodeStream(ctx, cancelFrameSource{entered}, packetSink(func(h264.Packet) error { return nil }), stream.EncodeConfig{Encoder: smallConfig()})
				} else {
					done <- stream.DecodeStream(ctx, cancelPacketSource{entered}, frameSink(func(*h264.Frame) error { return nil }), stream.DecodeConfig{})
				}
			}()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				cancel()
				t.Fatal("context端点未启动")
			}
			cancel()
			select {
			case err := <-done:
				if err != context.Canceled {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("取消未返回")
			}
		}
	}
	// pipeline从不启动worker；给已join goroutine一个退出调度点，不允许累计泄漏。
	runtime.Gosched()
	if n := runtime.NumGoroutine(); n > before+2 {
		t.Fatal("取消后goroutine增长", before, n)
	}
}

func TestPipeline_SlowConsumerBackpressure(t *testing.T) {
	var read atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	src := frameSource(func() (*h264.Frame, error) {
		n := read.Add(1)
		if n > 3 {
			return nil, io.EOF
		}
		return rawFrame(int(n)), nil
	})
	first := true
	dst := packetSink(func(h264.Packet) error {
		if first {
			first = false
			close(entered)
			<-release
		}
		return nil
	})
	done := make(chan error, 1)
	go func() {
		done <- stream.EncodeStream(context.Background(), src, dst, stream.EncodeConfig{Encoder: smallConfig()})
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("未写出首帧")
	}
	if n := read.Load(); n != 1 {
		close(release)
		t.Fatal("慢消费者未背压", n)
	}
	close(release)
	select {
	case err := <-done:
		if err != io.EOF {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("背压解除未完成")
	}
}

type canceledPacketWriter struct{ entered chan struct{} }

func (c canceledPacketWriter) WriteAccessUnit(h264.Packet) error {
	panic("不得调用非context写路径")
}
func (c canceledPacketWriter) WriteAccessUnitContext(ctx context.Context, _ h264.Packet) error {
	close(c.entered)
	<-ctx.Done()
	return ctx.Err()
}

type canceledFrameWriter struct {
	entered chan struct{}
	frame   *h264.Frame
}

func (c *canceledFrameWriter) WriteFrame(*h264.Frame) error { panic("不得调用非context写路径") }
func (c *canceledFrameWriter) WriteFrameContext(ctx context.Context, f *h264.Frame) error {
	c.frame = f
	close(c.entered)
	<-ctx.Done()
	return ctx.Err()
}

func TestPipeline_CancelBlockedOutputReleasesFrame(t *testing.T) {
	for _, encode := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		entered := make(chan struct{})
		done := make(chan error, 1)
		fw := &canceledFrameWriter{entered: entered}
		var data []byte
		if !encode {
			var b bytes.Buffer
			aw, _ := stream.NewAnnexBWriter(&b, stream.AnnexBWriterConfig{})
			r, _ := stream.NewYUVReader(bytes.NewReader(bytes.Repeat([]byte{128}, 384)), 16, 16, h264.YUV420P)
			if err := stream.EncodeStream(context.Background(), r, aw, stream.EncodeConfig{Encoder: smallConfig(), Options: []encoder.Option{encoder.WithPCM()}}); err != io.EOF {
				t.Fatal(err)
			}
			data = b.Bytes()
		}
		go func() {
			if encode {
				done <- stream.EncodeStream(ctx, frameSource(func() (*h264.Frame, error) { return rawFrame(0), nil }), canceledPacketWriter{entered}, stream.EncodeConfig{Encoder: smallConfig()})
			} else {
				nr, _ := stream.NewAnnexBReader(bytes.NewReader(data), 0)
				ar, _ := stream.NewAccessUnitReader(nr, stream.AccessUnitConfig{})
				done <- stream.DecodeStream(ctx, ar, fw, stream.DecodeConfig{})
			}
		}()
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			cancel()
			t.Fatal("输出未启动")
		}
		cancel()
		select {
		case err := <-done:
			if err != context.Canceled {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("输出取消未返回")
		}
		if !encode && (fw.frame == nil || fw.frame.Validate() == nil) {
			t.Fatal("取消写出后未释放解码帧")
		}
	}
}

func TestPipeline_TruncatedInputIsNotEOF(t *testing.T) {
	r, _ := stream.NewYUVReader(bytes.NewReader(bytes.Repeat([]byte{128}, 383)), 16, 16, h264.YUV420P)
	err := stream.EncodeStream(context.Background(), r, packetSink(func(h264.Packet) error { t.Fatal("截断帧被写出"); return nil }), stream.EncodeConfig{Encoder: smallConfig()})
	if !errors.Is(err, h264.ErrInvalidData) || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("截断原始帧未拒绝", err)
	}
	// 合法PCM AU移除最后几个字节：封装和头部仍可组装，但Decoder必须拒绝残差/采样截断。
	var b bytes.Buffer
	aw, _ := stream.NewAnnexBWriter(&b, stream.AnnexBWriterConfig{})
	full, _ := stream.NewYUVReader(bytes.NewReader(bytes.Repeat([]byte{128}, 384)), 16, 16, h264.YUV420P)
	if err := stream.EncodeStream(context.Background(), full, aw, stream.EncodeConfig{Encoder: smallConfig(), Options: []encoder.Option{encoder.WithPCM()}}); err != io.EOF {
		t.Fatal(err)
	}
	for _, limit := range []int{0, 8} {
		data := b.Bytes()
		if limit == 0 {
			data = data[:len(data)-8]
		}
		nr, _ := stream.NewAnnexBReader(bytes.NewReader(data), 0)
		ar, _ := stream.NewAccessUnitReader(nr, stream.AccessUnitConfig{})
		err := stream.DecodeStream(context.Background(), ar, frameSink(func(*h264.Frame) error { t.Fatal("坏流或超限被写出"); return nil }), stream.DecodeConfig{MaxInputBuffer: limit})
		want := h264.ErrInvalidData
		if limit > 0 {
			want = h264.ErrResourceLimit
		}
		if !errors.Is(err, want) {
			t.Fatal("尾部截断或组装预算错误", err)
		}
	}
}
