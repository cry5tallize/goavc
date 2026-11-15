package encoder

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/picture"
)

// 通过公开SendFrame在不同组合的后续通道拒绝QP，验证完整图像的唯一owner。
// FieldView/独立分量描述符只能借用；失败不能遗留租约，成功不能重复归还。
func TestEncoder_CompositeFailureReturnsOwnedPicture(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		field, separate, cabac bool
		failAt                 int
	}{
		{"frame", false, false, false, 2},
		{"cabac", false, false, true, 2},
		{"second_field", true, false, false, 3},
		{"second_field_cabac", true, false, true, 3},
		{"second_plane", false, true, false, 5},
		{"second_plane_cabac", false, true, true, 5},
		{"second_plane_field", true, true, false, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := h264.DefaultEncoderConfig()
			cfg.Width, cfg.Height = 32, 32
			cfg.Profile, cfg.PixelFormat = h264.ProfileHigh444, h264.YUV444P
			calls, reject := 0, true
			options := []Option{WithCABAC(tc.cabac), WithMacroblockQP(func(x, y int) int {
				calls++
				if reject && calls == tc.failAt {
					return 52
				}
				return 26
			})}
			if tc.field {
				options = append(options, WithInterlaced(true))
			}
			if tc.separate {
				options = append(options, WithSeparateColourPlanes())
			}
			e, err := New(cfg, options...)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			f, err := h264.NewFrame(32, 32, cfg.PixelFormat)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Release()
			for comp := range f.Planes {
				for i := range f.Planes[comp].Data {
					f.Planes[comp].Data[i] = byte(40 + comp*40)
				}
			}
			if err = e.SendFrame(f); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatal(err)
			}
			gets, puts, _ := e.picturePool.Stats()
			if gets != puts {
				t.Fatalf("失败遗留图像: gets=%d puts=%d", gets, puts)
			}
			if e.frameStorage.leasedBytes != 0 {
				t.Fatal("failed composite retained metadata leases", e.frameStorage.leasedBytes)
			}
			if e.reference != nil || e.out != nil {
				t.Fatal("失败提交了参考或输出")
			}
			reject = false
			if err = e.SendFrame(f); err != nil {
				t.Fatal(err)
			}
			if _, err = e.ReceivePacket(); err != nil {
				t.Fatal(err)
			}
			gets, puts, _ = e.picturePool.Stats()
			if gets-puts != 1 {
				t.Fatalf("成功重建必须唯一持有: gets=%d puts=%d", gets, puts)
			}
			if err = e.Reset(); err != nil {
				t.Fatal(err)
			}
			gets, puts, _ = e.picturePool.Stats()
			if gets != puts {
				t.Fatalf("Reset遗留图像: gets=%d puts=%d", gets, puts)
			}
			if e.frameStorage.leasedBytes != 0 {
				t.Fatal("Reset仍保留元数据租约", e.frameStorage.leasedBytes)
			}
		})
	}
}

// 与不复用空闲元数据的真实编解码通道比较；脏缓存和QP尝试不能改变后续
// 码流、选中QP、重建或仍存活的参考像素。
func TestEncoder_FrameStorageConsumerLifetimes(t *testing.T) {
	for _, format := range []h264.PixelFormat{h264.YUV420P, h264.YUV420P10LE, h264.YUV422P, h264.YUV422P10LE, h264.YUV444P, h264.YUV444P10LE} {
		for _, field := range []bool{false, true} {
			separates := []bool{false}
			if subW, subH := format.ChromaSubsampling(); subW == 1 && subH == 1 {
				separates = append(separates, true)
			}
			for _, separate := range separates {
				t.Run(fmt.Sprintf("%s/field=%v/separate=%v", format, field, separate), func(t *testing.T) {
					cfg := h264.DefaultEncoderConfig()
					cfg.Width, cfg.Height, cfg.Profile, cfg.PixelFormat = 32, 32, h264.ProfileHigh444, format
					// 代表配置覆盖RC best/attempt和B重排，不放大六格式矩阵。
					if format == h264.YUV420P || separate && field {
						cfg.RateControl, cfg.Bitrate = h264.RateControlABR, 30_000
						cfg.MaxBFrames = 2
					}
					options := []Option{WithCABAC(field)}
					if field {
						options = append(options, WithInterlaced(true))
					}
					if separate {
						options = append(options, WithSeparateColourPlanes())
					}
					pooled, err := New(cfg, options...)
					if err != nil {
						t.Fatal(err)
					}
					defer pooled.Close()
					isolated, err := New(cfg, options...)
					if err != nil {
						t.Fatal(err)
					}
					defer isolated.Close()
					frame, err := h264.NewFrame(32, 32, format)
					if err != nil {
						t.Fatal(err)
					}
					defer frame.Release()
					outputs := 0
					drain := func() {
						for {
							got, gotErr := pooled.ReceivePacket()
							// 旧租约仍归原owner，下一输出不复用其空闲backing。
							isolated.frameStorage = new(frameStoragePool)
							want, wantErr := isolated.ReceivePacket()
							if gotErr != wantErr {
								t.Fatalf("输出错误不同: %v / %v", gotErr, wantErr)
							}
							if errors.Is(gotErr, h264.ErrAgain) || errors.Is(gotErr, h264.ErrEOF) {
								return
							}
							if gotErr != nil {
								t.Fatal(gotErr)
							}
							if !bytes.Equal(got.Data, want.Data) || got.PTS != want.PTS || got.DTS != want.DTS || pooled.LastQP() != isolated.LastQP() {
								t.Fatalf("元数据复用改变第%d个输出", outputs)
							}
							a, err := pooled.Reconstruction()
							if err != nil {
								t.Fatal(err)
							}
							b, err := isolated.Reconstruction()
							if err != nil {
								a.Release()
								t.Fatal(err)
							}
							for c := range a.Planes {
								if !bytes.Equal(a.Planes[c].Data, b.Planes[c].Data) {
									t.Errorf("第%d个输出平面%d重建改变", outputs, c)
								}
							}
							a.Release()
							b.Release()
							outputs++
						}
					}
					for pass := range 2 {
						for n := range 7 {
							frame.PTS, frame.Duration = int64(n), 1
							for c := range frame.Planes {
								p := &frame.Planes[c]
								step := format.BitDepth() / 8
								if format.BitDepth() > 8 {
									step = 2
								}
								for i := 0; i < len(p.Data); i += step {
									v := uint16((i/step*3 + n*17 + c*43 + pass*29) & 255)
									if step == 2 {
										binary.LittleEndian.PutUint16(p.Data[i:], v*4+uint16(n%4))
									} else {
										p.Data[i] = byte(v)
									}
								}
							}
							if err = pooled.SendFrame(frame); err != nil {
								t.Fatal(err)
							}
							isolated.frameStorage = new(frameStoragePool)
							if err = isolated.SendFrame(frame); err != nil {
								t.Fatal(err)
							}
							drain()
						}
						if err = pooled.Flush(); err != nil {
							t.Fatal(err)
						}
						if err = isolated.Flush(); err != nil {
							t.Fatal(err)
						}
						drain()
						if outputs != (pass+1)*7 {
							t.Fatal("缺少输出帧", outputs)
						}
						if err = pooled.Reset(); err != nil {
							t.Fatal(err)
						}
						if pooled.frameStorage.leasedBytes != 0 {
							t.Fatal("Reset后仍保留元数据租约", pooled.frameStorage.leasedBytes)
						}
						if err = isolated.Reset(); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		}
	}
}

func TestEncoder_RateBestMetadataSurvivesLaterCandidates(t *testing.T) {
	for _, composite := range []bool{false, true} {
		t.Run(fmt.Sprintf("composite=%v", composite), func(t *testing.T) {
			cfg := h264.DefaultEncoderConfig()
			cfg.Width, cfg.Height, cfg.Profile, cfg.PixelFormat = 32, 32, h264.ProfileHigh444, h264.YUV444P10LE
			cfg.RateControl, cfg.Bitrate = h264.RateControlABR, 30_000
			options := []Option{WithCABAC(true)}
			if composite {
				options = append(options, WithInterlaced(true), WithSeparateColourPlanes())
			}
			e, err := New(cfg, options...)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			src, err := picture.New(2, 2, cfg.PixelFormat)
			if err != nil {
				t.Fatal(err)
			}
			defer src.Release()
			for c := range src.Planes {
				p := &src.Planes[c]
				for y := range p.Height {
					for x := range p.Width {
						p.SetSample(x, y, uint16((x*31+y*17+c*131)&1023))
					}
				}
			}
			data, best, qp, err := e.encodeRateControlled(src, true, true)
			if err != nil {
				t.Fatal(err)
			}
			defer releaseFrameState(best)
			want, exact, err := e.encodeAtQP(src, true, true, qp)
			if err != nil {
				t.Fatal(err)
			}
			defer releaseFrameState(exact)
			if !bytes.Equal(data, want) {
				t.Fatal("RC所选输出不同于相同QP的独立编码")
			}
			var compare func(*frameState, *frameState)
			compare = func(a, b *frameState) {
				if a == nil || b == nil {
					if a != b {
						t.Fatal("保留的场状态丢失")
					}
					return
				}
				if !slices.Equal(a.info, b.info) || !slices.Equal(a.nz, b.nz) || !slices.Equal(a.mvd, b.mvd) {
					t.Fatal("保留RC元数据不同于所选QP的独立编码")
				}
				for c := range a.cnz {
					if !slices.Equal(a.cnz[c], b.cnz[c]) {
						t.Fatal("保留色度系数改变")
					}
				}
				for c := range a.dcCoded {
					if !slices.Equal(a.dcCoded[c], b.dcCoded[c]) {
						t.Fatal("保留DC编码决策改变")
					}
				}
				if a.mv != nil && (b.mv == nil || !slices.Equal(a.mv.MV, b.mv.MV) || !slices.Equal(a.mv.Ref, b.mv.Ref)) {
					t.Fatal("保留运动网格改变")
				}
				for c := range a.pic.Planes {
					if !slices.Equal(a.pic.Planes[c].Data16, b.pic.Planes[c].Data16) {
						t.Fatal("保留重建像素改变")
					}
				}
				for c := range a.fields {
					compare(a.fields[c], b.fields[c])
				}
			}
			compare(best, exact)
			for _, nextQP := range []int{0, 51, 12, 39, qp} {
				_, trial, err := e.encodeAtQP(src, true, true, nextQP)
				if err != nil {
					t.Fatal(err)
				}
				releaseFrameState(trial)
				compare(best, exact)
			}
			releaseFrameState(best)
			releaseFrameState(exact)
			if e.frameStorage.leasedBytes != 0 {
				t.Fatal("已完成RC消费者仍持有元数据租约", e.frameStorage.leasedBytes)
			}
		})
	}
}

func TestEncoder_FrameStorageBudgetKeepsActiveConsumers(t *testing.T) {
	p := new(frameStoragePool)
	held := p.get(1, 1)
	v := held.mv.MV[0]
	v.X, v.Y = 12, -8
	held.mv.SetRect(0, 0, 1, 1, v, 0)
	idle := p.get(1, 1)
	p.put(idle)
	before := p.idleBytes
	large := p.get(256, 256)
	if large.bytes <= frameStorageBudget {
		t.Fatal("超预算测试几何不足")
	}
	p.put(large)
	if p.idleBytes != before {
		t.Fatal("超预算存储不能进入空闲缓存")
	}
	if got, ref, ok := held.mv.Get(0, 0); !ok || got != v || ref != 0 {
		t.Fatal("缓存政策改变活跃消费者的运动数据", got, ref, ok)
	}
	p.put(held)
	reused := p.get(1, 1)
	if _, _, ok := reused.mv.Get(0, 0); ok {
		t.Fatal("新帧继承旧运动邻居可用性")
	}
	p.put(reused)
	var active [5]*frameStorage
	for i := range active {
		active[i] = p.get(1, 1)
	}
	for _, s := range active {
		p.put(s)
	}
	if p.count > 3 || p.leasedBytes != 0 || p.idleBytes > frameStorageBudget {
		t.Fatal("空闲缓存超出槽数/字节预算", p.count, p.leasedBytes, p.idleBytes)
	}
}

func BenchmarkEncoderFrameStateMetadataReuse(b *testing.B) {
	cfg := h264.DefaultEncoderConfig()
	cfg.Width, cfg.Height = 1280, 720
	e, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()
	src, err := picture.New(80, 45, cfg.PixelFormat)
	if err != nil {
		b.Fatal(err)
	}
	defer src.Release()
	// 与场/独立平面相同地借用重建描述符，隔离像素分配与元数据成本。
	e.planeReconstruction = src
	warm, err := e.newFrameState(src)
	if err != nil {
		b.Fatal(err)
	}
	releaseFrameState(warm)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s, err := e.newFrameState(src)
		if err != nil {
			b.Fatal(err)
		}
		releaseFrameState(s)
	}
}
