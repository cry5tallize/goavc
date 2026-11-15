package benchmarks_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/decoder"
	"github.com/cry5tallize/goavc/stream"
)

// 输入现场生成或使用已有语料；组AU和文件I/O均在计时外。
func benchmarkAccessUnits(t testing.TB) (string, []h264.Packet) {
	t.Helper()
	path := os.Getenv("GOAVC_BENCH_INPUT")
	if path == "" {
		t.Skip("设置GOAVC_BENCH_INPUT为Annex-B语料路径后运行直接AU测量")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	nr, err := stream.NewAnnexBReader(bytes.NewReader(data), 0)
	if err != nil {
		t.Fatal(err)
	}
	ar, err := stream.NewAccessUnitReader(nr, stream.AccessUnitConfig{})
	if err != nil {
		t.Fatal(err)
	}
	var packets []h264.Packet
	for {
		p, err := ar.ReadAccessUnit()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		packets = append(packets, p)
	}
	if len(packets) == 0 {
		t.Fatal("输入没有AU")
	}
	return path, packets
}

func decodeAccessUnits(t testing.TB, packets []h264.Packet, visit func(*h264.Frame)) {
	t.Helper()
	d, err := decoder.New(h264.DecoderConfig{Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	frames := 0
	drain := func() {
		for {
			f, err := d.ReceiveFrame()
			if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			frames++
			if visit != nil {
				visit(f)
			}
			f.Release()
		}
	}
	for _, p := range packets {
		if err := d.SendPacket(p); err != nil {
			t.Fatal(err)
		}
		drain()
	}
	if err := d.Flush(); err != nil {
		t.Fatal(err)
	}
	drain()
	if frames != len(packets) {
		t.Fatalf("得到%d帧，输入%d个AU", frames, len(packets))
	}
}

func BenchmarkDecoder_DirectAU_Auto(b *testing.B) {
	_, packets := benchmarkAccessUnits(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		decodeAccessUnits(b, packets, nil)
	}
	b.ReportMetric(float64(b.N*len(packets))/b.Elapsed().Seconds(), "frames/s")
}

func TestDecoder_DirectAUPerformanceOracle(t *testing.T) {
	path, packets := benchmarkAccessUnits(t)
	var got bytes.Buffer
	w, err := stream.NewYUVWriter(&got)
	if err != nil {
		t.Fatal(err)
	}
	decodeAccessUnits(t, packets, func(f *h264.Frame) {
		if err := w.WriteFrame(f); err != nil {
			t.Fatal(err)
		}
	})
	// 该显式性能输入测试要求独立Oracle可执行，缺少工具直接失败。
	cmd := exec.Command("ffmpeg", "-v", "error", "-xerror", "-i", path, "-f", "rawvideo", "-pix_fmt", "yuv420p", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	want, err := cmd.Output()
	if err != nil {
		t.Fatalf("FFmpeg: %v\n%s", err, &stderr)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("直接AU像素Oracle不一致：本地%d字节，FFmpeg%d字节", got.Len(), len(want))
	}
}
