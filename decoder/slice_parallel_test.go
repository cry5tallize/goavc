package decoder

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/nal"
)

func TestDecoder_ParallelSlicePixels(t *testing.T) {
	for _, path := range []string{
		"../testdata/baseline/baseline_320x240_yuv420p_cavlc_ip_multislice_003.264",
		"../testdata/main/main_320x240_yuv420p_cabac_ip_multislice_004.264",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		want := ffmpegRaw(t, ffmpegPath(t), path)
		for _, threads := range []int{1, 2, 4, 0, 128} {
			t.Run(fmt.Sprintf("%s/threads%d", path, threads), func(t *testing.T) {
				d := mustDecoder(t, h264.DecoderConfig{Threads: threads})
				if err := d.SendPacket(h264.Packet{Data: data}); err != nil {
					t.Fatal(err)
				}
				if err := d.Flush(); err != nil {
					t.Fatal(err)
				}
				compareHigh422(t, collectNativeFrames(t, d, nil), want, h264.YUV420P)
			})
		}
	}
}

func TestDecoder_ParallelSliceSixFormatOracle(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("需要至少两个可用线程验证并行")
	}
	ff := high422FFmpeg(t)
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE, h264.YUV422P, h264.YUV422P10LE, h264.YUV444P, h264.YUV444P10LE} {
		for _, cabac := range []int{0, 1} {
			t.Run(fmt.Sprintf("%v/cabac%d", format, cabac), func(t *testing.T) {
				params := fmt.Sprintf("cabac=%d:keyint=24:bframes=2:b-adapt=0:slices=2:direct=temporal:weightb=1:ref=2:8x8dct=1", cabac)
				path := makeHigh422Oracle(t, ff, format, 24, params)
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				want := high422Raw(t, ff, path, format)
				type frameTag struct {
					pts, duration int64
					key           bool
				}
				var tags [2][]frameTag
				units, err := nal.SplitAnnexB(data)
				if err != nil {
					t.Fatal(err)
				}
				for side := range tags {
					d := mustDecoder(t, h264.DecoderConfig{Threads: side + 1, OutputFormat: format})
					var got []byte
					drain := func() {
						for {
							f, err := d.ReceiveFrame()
							if errors.Is(err, h264.ErrAgain) || errors.Is(err, h264.ErrEOF) {
								return
							}
							if err != nil {
								t.Fatal(err)
							}
							tags[side] = append(tags[side], frameTag{f.PTS, f.Duration, f.KeyFrame})
							for c, p := range f.Planes {
								_, rows := format.PlaneDimensions(c, f.Width, f.Height)
								for y := range rows {
									got = append(got, p.Data[y*p.Stride:][:format.RowBytes(c, f.Width)]...)
								}
							}
							f.Release()
						}
					}
					au := int64(0)
					for first := 0; first < len(units); {
						end := first + 1
						for end < len(units) && units[end].Header.Type != nal.TypeAUD {
							end++
						}
						if err := d.SendPacket(h264.Packet{Data: annex(units[first:end]...), PTS: 100 + au*7, Duration: 1 + au}); err != nil {
							t.Fatal(err)
						}
						if side == 1 {
							var entries []sliceEntry
							for _, u := range units[first:end] {
								if u.Header.Type == nal.TypeSlice || u.Header.Type == nal.TypeSliceIDR {
									entry, err := d.parseSlice(u)
									if err != nil {
										t.Fatal(err)
									}
									entries = append(entries, entry)
								}
							}
							if len(entries) != 2 || parallelSliceWorkers(2, entries) != 2 {
								t.Fatalf("AU %d 未触发两片并行", au)
							}
						}
						drain()
						au++
						first = end
					}
					if err := d.Flush(); err != nil {
						t.Fatal(err)
					}
					drain()
					compareHigh422(t, got, want, format)
				}
				if len(tags[0]) != 8 || !slices.Equal(tags[0], tags[1]) {
					t.Fatalf("frame metadata or display order differs: %v", tags)
				}
			})
		}
	}
}

// 整行 PCM 向量有独立像素解析解。缩短两片后，串并行必须选择同一个
// 前序错误；失败不得发布半帧，同一个实例可以从下一 IDR 恢复。
func TestDecoder_ParallelSliceFailureRecovery(t *testing.T) {
	good := fmoPacket(t, fmoVector{}, []fmoSliceVector{
		{addresses: []int{0, 1, 2, 3, 4, 5, 6, 7}, i4: -1},
		{addresses: []int{8, 9, 10, 11, 12, 13, 14, 15}, i4: -1},
	})
	units, err := nal.SplitAnnexB(good)
	if err != nil || len(units) != 4 {
		t.Fatalf("unexpected fixture: %d NALs, %v", len(units), err)
	}
	units[2].Payload = units[2].Payload[:len(units[2].Payload)-30]
	units[3].Payload = units[3].Payload[:len(units[3].Payload)-70]
	bad := h264.Packet{Data: annex(units...)}
	serial := mustDecoder(t, h264.DecoderConfig{Threads: 1})
	want := serial.SendPacket(bad)
	if !errors.Is(want, h264.ErrInvalidData) {
		t.Fatalf("fixture did not fail during decode: %v", want)
	}
	d := mustDecoder(t, h264.DecoderConfig{Threads: 4})
	for range 4 {
		if got := d.SendPacket(bad); !errors.Is(got, h264.ErrInvalidData) || got.Error() != want.Error() {
			t.Fatalf("parallel error %v, serial error %v", got, want)
		}
		if _, err := d.ReceiveFrame(); !errors.Is(err, h264.ErrAgain) {
			t.Fatalf("failed picture published: %v", err)
		}
		if err := d.SendPacket(h264.Packet{Data: good}); err != nil {
			t.Fatal(err)
		}
		fmoAssertPicture(t, d, -1)
	}
	if err := d.Reset(); err != nil {
		t.Fatal(err)
	}
	if err := d.SendPacket(h264.Packet{Data: good}); err != nil {
		t.Fatal(err)
	}
	fmoAssertPicture(t, d, -1)
}

func TestDecoder_ParallelSliceFallback(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("需要至少两个可用线程验证路由")
	}
	for _, tc := range []struct {
		name   string
		splits [][]int
		want   int
	}{
		{"rows", [][]int{{0, 1, 2, 3, 4, 5, 6, 7}, {8, 9, 10, 11, 12, 13, 14, 15}}, 2},
		{"midrow", [][]int{{0, 1, 2, 3, 4, 5, 6}, {7, 8, 9, 10, 11, 12, 13, 14, 15}}, 1},
		{"aso", [][]int{{8, 9, 10, 11, 12, 13, 14, 15}, {0, 1, 2, 3, 4, 5, 6, 7}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := fmoPacket(t, fmoVector{}, []fmoSliceVector{{addresses: tc.splits[0], i4: -1}, {addresses: tc.splits[1], i4: -1}})
			units, err := nal.SplitAnnexB(data)
			if err != nil || len(units) != 4 {
				t.Fatalf("unexpected fixture: %d NALs, %v", len(units), err)
			}
			d := mustDecoder(t, h264.DecoderConfig{Threads: 2})
			if err := d.SendPacket(h264.Packet{Data: annex(units[:2]...)}); err != nil {
				t.Fatal(err)
			}
			var entries [2]sliceEntry
			for i, unit := range units[2:] {
				entries[i], err = d.parseSlice(unit)
				if err != nil {
					t.Fatal(err)
				}
			}
			if got := parallelSliceWorkers(2, entries[:]); got != tc.want {
				t.Fatalf("workers=%d, want %d", got, tc.want)
			}
			if err := d.SendPacket(h264.Packet{Data: annex(units[2:]...)}); err != nil {
				t.Fatal(err)
			}
			fmoAssertPicture(t, d, -1)
		})
	}
}
