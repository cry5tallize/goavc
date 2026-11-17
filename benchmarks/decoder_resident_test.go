package benchmarks_test

import (
	"errors"
	"runtime"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/decoder"
)

// BenchmarkDecoder_Resident_ResetCycle 用同一个实例反复解码完整输入。
// 每轮显式 Flush/Reset，保留实例缓存；它是循环播放负载，不替代 Sequence 冷端点。
// 同一输入的独立像素正确性由 Sequence 校验，本入口核对每轮完整输出帧数。
func BenchmarkDecoder_Resident_ResetCycle(b *testing.B) {
	_, packets := benchmarkAccessUnits(b)
	d, err := decoder.New(h264.DecoderConfig{Threads: 1})
	if err != nil {
		b.Fatal(err)
	}
	defer d.Close()
	count := 0
	drain := func() {
		for {
			f, err := d.ReceiveFrame()
			if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
				return
			}
			if err != nil {
				b.Fatal(err)
			}
			count++
			f.Release()
		}
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	b.ReportAllocs()
	for b.Loop() {
		count = 0
		for _, p := range packets {
			if err := d.SendPacket(p); err != nil {
				b.Fatal(err)
			}
			drain()
		}
		if err := d.Flush(); err != nil {
			b.Fatal(err)
		}
		drain()
		if count != len(packets) {
			b.Fatalf("得到%d帧，期望%d", count, len(packets))
		}
		if err := d.Reset(); err != nil {
			b.Fatal(err)
		}
	}
	runtime.ReadMemStats(&after)
	b.ReportMetric(float64(after.NumGC-before.NumGC)/float64(b.N), "gc/op")
	b.ReportMetric(float64(after.PauseTotalNs-before.PauseTotalNs)/float64(b.N), "gc-pause-ns/op")
}
