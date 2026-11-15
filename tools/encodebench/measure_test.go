package main

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/encoder"
)

// 通过工具入口比较字节、RC轨迹与媒体时钟，不对非确定性的性能数值设阈值。
func TestEndpointMeasurementMatchesQuality(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width, c.Height = 32, 32
	c.Profile, c.MaxBFrames, c.GOPSize = h264.ProfileHigh, 2, 7
	c.FPSNum, c.FPSDen = 30000, 1001
	c.RateControl, c.Bitrate, c.VBVBufferSize = h264.RateControlVBV, 9000, 9000
	const frames = 9
	raw := bytes.Repeat([]byte{128}, 32*32*3/2*frames)
	for i := range frames {
		for j := range 32 * 32 {
			raw[i*32*32*3/2+j] = byte(124 + i)
		}
	}
	extra := []encoder.Option{encoder.WithTransform8x8(), encoder.WithWeightedPrediction(), encoder.WithLookahead(2)}
	quality, want, err := measureConfigured(c, frames, 4, false, bytes.NewReader(raw), extra, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	cfg := measurementConfig{Encoder: c, Frames: frames, SliceCount: 4, Transform8x8: true, WeightedPrediction: true, Lookahead: 2}
	var output bytes.Buffer
	got, err := runMeasurement(cfg, bytes.NewReader(raw), &output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), want) || got.StreamSHA256 != quality.StreamSHA256 || got.InputSHA256 != quality.InputSHA256 || got.InputBytes != int64(len(raw)) || got.OutputBytes != int64(len(want)) {
		t.Fatal("measure与quality字节或完整哈希不相等")
	}
	if !reflect.DeepEqual(got.QPSequence, quality.QPSequence) || !reflect.DeepEqual(got.FrameBits, quality.FrameBits) || !reflect.DeepEqual(got.VBVFullnessSequence, quality.VBVFullnessSequence) || !reflect.DeepEqual(got.PacketPTS, quality.PacketPTS) || !reflect.DeepEqual(got.PacketDTS, quality.PacketDTS) {
		t.Fatal("measure改变了RC轨迹或重排时钟")
	}
	for i := range got.InputPTS {
		if got.InputPTS[i] != int64(i) || got.InputDuration[i] != 1 || got.PacketDuration[i] != quality.PacketDuration[i] {
			t.Fatal("输入或输出时钟错误", i)
		}
	}
	if got.InputFrames != frames || got.Packets != frames {
		t.Fatal("测量边界或采样不完整", got)
	}
	// 无输出文件时也必须哈希全部包，不能依赖writer保留码流。
	withoutOutput, err := runMeasurement(cfg, bytes.NewReader(raw), nil)
	if err != nil || withoutOutput.StreamSHA256 != got.StreamSHA256 {
		t.Fatal("可选输出改变了码流哈希", err)
	}
}

type failedPacketWriter struct{ err error }

func (w failedPacketWriter) Write([]byte) (int, error) { return 0, w.err }

func TestEndpointMeasurementErrorsDoNotPublishSuccess(t *testing.T) {
	c := h264.DefaultEncoderConfig()
	c.Width, c.Height = 16, 16
	cfg := measurementConfig{Encoder: c, Frames: 2, SliceCount: 1}
	raw := bytes.Repeat([]byte{128}, 16*16*3)
	writeErr := errors.New("packet writer failed")
	for _, tc := range []struct {
		name   string
		input  []byte
		writer io.Writer
		want   error
	}{
		{"truncated", raw[:len(raw)-1], nil, io.ErrUnexpectedEOF},
		{"writer", raw, failedPacketWriter{writeErr}, writeErr},
		{"short writer", raw, shortReconWriter{}, io.ErrShortWrite},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := runMeasurement(cfg, bytes.NewReader(tc.input), tc.writer)
			if !errors.Is(err, tc.want) || !reflect.DeepEqual(r, measurementReport{}) {
				t.Fatal("错误路径发布了成功报告或丢失错误", r, err)
			}
		})
	}
	if _, err := runMeasurement(cfg, nil, nil); err == nil {
		t.Fatal("measure必须要求raw输入")
	}
	if _, err := runMeasurement(cfg, bytes.NewReader(append(raw, 0)), nil); err == nil {
		t.Fatal("完整输入哈希不能忽略尾随数据")
	}
	cfg.Encoder.Threads = 2
	if _, err := runMeasurement(cfg, bytes.NewReader(raw), nil); !errors.Is(err, h264.ErrUnsupported) {
		t.Fatal("不得放行不支持的多线程配置", err)
	}
}
