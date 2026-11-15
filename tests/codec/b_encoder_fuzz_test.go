package codec_test

import (
	"bytes"
	"errors"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/decoder"
	"github.com/goavc/h264/encoder"
)

// 随机化延迟输入、分片、QP、矩阵和参考组边界；验证同码流显示输出与
// 本地重建一致。不以自往返替代独立Oracle，本入口负责回归和所有权压力。
func FuzzEncoder_BReorderRoundTrip(f *testing.F) {
	f.Add([]byte{0, 26, 1, 3, 255, 0, 128, 91})
	f.Add([]byte{1, 0, 2, 1, 0, 255, 17, 128})
	f.Add([]byte{2, 51, 0, 2, 9, 37, 127, 201})
	// 原Auto Level在此低QP输入下会合法触发AU预算；保留为完整重排回归种子。
	f.Add([]byte("0\x00!1 \xfb \x8b0"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 8 {
			return
		}
		cfg := h264.DefaultEncoderConfig()
		cfg.Width, cfg.Height = 16, 16
		cfg.Profile = h264.ProfileHigh
		// 本入口验证完整重排/像素/所有权；为任意低QP和自定义矩阵保留足够
		// 的合法Level预算，避免把正常ErrResourceLimit误报为编解码失败。
		// 生产Auto策略与其资源错误、已有Level预算测试保持不变。
		cfg.Level = 31
		cfg.MaxBFrames = 1 + int(data[0]%3)
		cfg.QP = int(data[1] % 52)
		cfg.GOPSize, cfg.IDRInterval = 5, 9
		options := []encoder.Option{encoder.WithSearchRange(1), encoder.WithTransform8x8()}
		if data[2]%2 == 0 {
			options = append(options, encoder.WithLookahead(1))
		}
		if data[3]%2 == 0 {
			var matrices [6][16]uint8
			for l := range matrices {
				for i := range matrices[l] {
					matrices[l][i] = 1 + data[(l+i)%len(data)]%254
				}
			}
			options = append(options, encoder.WithScalingMatrices4x4(matrices))
		}
		e, err := encoder.New(cfg, options...)
		if err != nil {
			t.Fatal(err)
		}
		defer e.Close()
		d, err := decoder.New(h264.DecoderConfig{})
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		recons := map[int64][]byte{}
		count := 0
		lastDTS := int64(-100)
		drainDecoder := func() {
			t.Helper()
			for {
				got, err := d.ReceiveFrame()
				if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if got.PTS != int64(count) || !bytes.Equal(packed(got), recons[got.PTS]) {
					t.Fatalf("B输出不一致：PTS%d，显示帧%d", got.PTS, count)
				}
				got.Release()
				count++
			}
		}
		drainEncoder := func() {
			t.Helper()
			for {
				p, err := e.ReceivePacket()
				if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if p.DTS <= lastDTS {
					t.Fatal("DTS非递增")
				}
				lastDTS = p.DTS
				r, err := e.Reconstruction()
				if err != nil {
					t.Fatal(err)
				}
				recons[p.PTS] = packed(r)
				r.Release()
				if err = d.SendPacket(p); err != nil {
					t.Fatal(err)
				}
				drainDecoder()
			}
		}
		for index := 0; index < 10; index++ {
			input := pattern(16, 16)
			input.PTS = int64(index)
			input.Duration = 1
			input.KeyFrame = index == int(data[4]%10)
			for comp := range input.Planes {
				for i := range input.Planes[comp].Data {
					input.Planes[comp].Data[i] = data[(i+index*3+comp)%len(data)]
				}
			}
			if err = e.SendFrame(input); err != nil {
				t.Fatal(err)
			}
			input.Release()
			drainEncoder()
		}
		if err = e.Flush(); err != nil {
			t.Fatal(err)
		}
		drainEncoder()
		if err = d.Flush(); err != nil {
			t.Fatal(err)
		}
		drainDecoder()
		if count != 10 {
			t.Fatal("Flush丢帧", count)
		}
	})
}
