package decoder

import (
	"errors"
	"os"
	"testing"

	h264 "github.com/goavc/h264"
)

// 每次迭代解码同一条三帧 1080p I/P 序列，包含构造、Flush、输出消费
// 与 Close；输入文件 I/O 不计时。该基线有意保留 M2 的全部分配成本。
func BenchmarkDecoder_1080p_Generic(b *testing.B) {
	data, err := os.ReadFile("../testdata/baseline/baseline_1920x1080_yuv420p_cavlc_ip_005.264")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(1920 * 1080 * 3 / 2 * 3))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d, err := New(h264.DecoderConfig{})
		if err != nil {
			b.Fatal(err)
		}
		if err := d.SendPacket(h264.Packet{Data: data}); err != nil {
			d.Close()
			b.Fatal(err)
		}
		if err := d.Flush(); err != nil {
			d.Close()
			b.Fatal(err)
		}
		frames := 0
		for {
			f, err := d.ReceiveFrame()
			if errors.Is(err, h264.ErrEOF) {
				break
			}
			if err != nil {
				d.Close()
				b.Fatal(err)
			}
			frames++
			f.Release()
		}
		d.Close()
		if frames != 3 {
			b.Fatalf("decoded %d frames", frames)
		}
	}
	b.ReportMetric(float64(b.N*3)/b.Elapsed().Seconds(), "frames/s")
}
