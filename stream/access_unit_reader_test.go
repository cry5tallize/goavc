package stream

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/nal"
)

// Spec: 7.3.2/3逐字段写出的独立比特向量，不调用syntax序列化器。
// slice仅含合法头部，验证组装而非声称残差或像素可解码。
func auBits(fields string) []byte {
	s := strings.Join(strings.Fields(fields), "")
	s += strings.Repeat("0", (8-len(s)%8)%8)
	b := make([]byte, len(s)/8)
	for i := range b {
		n, err := strconv.ParseUint(s[i*8:i*8+8], 2, 8)
		if err != nil {
			panic(err)
		}
		b[i] = byte(n)
	}
	return b
}

func auParameters() [][]byte {
	return [][]byte{
		append([]byte{0x67}, auBits("01000010 00000000 00011110 1 1 1 1 010 0 010 1 1 1 0 0 1")...),
		append([]byte{0x68}, auBits("1 1 0 0 1 1 1 0 00 1 1 1 0 0 0 1")...),
	}
}

func auSlice(firstMB, frame, poc string, idr bool) []byte {
	header, marking := byte(0x61), "0"
	id := ""
	if idr {
		header, marking, id = 0x65, "00", "1"
	}
	return append([]byte{header}, auBits(firstMB+" 011 1 "+frame+" "+id+" "+poc+" "+marking+" 1 1")...)
}

func auAnnex(units ...[]byte) []byte {
	var b []byte
	for _, u := range units {
		b = append(b, 0, 0, 0, 1)
		b = append(b, u...)
	}
	return b
}

func auPackets(t *testing.T, src NALReader, cfg AccessUnitConfig) []h264.Packet {
	t.Helper()
	r, err := NewAccessUnitReader(src, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var out []h264.Packet
	for {
		p, err := r.ReadAccessUnit()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	if _, err := r.ReadAccessUnit(); err != io.EOF {
		t.Fatal("EOF不粘性", err)
	}
	return out
}

func TestAUReader_KnownBoundariesChunkMatrix(t *testing.T) {
	params := auParameters()
	i0, i1 := auSlice("1", "0000", "0000", true), auSlice("010", "0000", "0000", true)
	p0, p1 := auSlice("1", "0001", "0010", false), auSlice("010", "0001", "0010", false)
	// filler不能抹掉最近VCL标识；第三张图像首slice非零，仅POC变化也必须拆开。
	filler, sei, aud := []byte{12, 0xff, 0x80}, []byte{6, 0x80}, []byte{9, 0xf0}
	q := auSlice("010", "0001", "0100", false)
	last := auSlice("1", "0010", "0110", false)
	groups := [][]byte{auAnnex(params[0], params[1], i0, i1, filler), auAnnex(p0, p1), auAnnex(sei, q), auAnnex(aud, last, []byte{10, 0x80}, []byte{11, 0x80})}
	raw := bytes.Join(groups, nil)
	for _, chunk := range []int{1, 2, 3, 7, 4096, len(raw)} {
		src, _ := NewAnnexBReader(annexChunkReader{bytes.NewReader(raw), chunk}, 0)
		got := auPackets(t, src, AccessUnitConfig{})
		if len(got) != len(groups) {
			t.Fatalf("chunk%d: AU=%d", chunk, len(got))
		}
		for i, p := range got {
			if !bytes.Equal(p.Data, groups[i]) || p.KeyFrame != (i == 0) || p.PTS != 0 || p.DTS != 0 || p.Duration != 0 {
				t.Fatalf("chunk%d AU%d=%+v", chunk, i, p)
			}
		}
		got[0].Data[0] = 99
		if got[1].Data[0] != 0 || raw[0] != 0 {
			t.Fatal("Packet共享缓冲")
		}
	}
}

func TestAUReader_FirstMBPolicyAndIdentityAcrossFiller(t *testing.T) {
	params := auParameters()
	slice := auSlice("1", "0001", "0010", false)
	raw := auAnnex(params[0], params[1], slice, []byte{12, 0x80}, slice)
	for mode, want := range map[BoundaryMode]int{BoundaryPicture: 1, BoundaryFirstMB: 2} {
		src, _ := NewAnnexBReader(bytes.NewReader(raw), 0)
		if got := len(auPackets(t, src, AccessUnitConfig{BoundaryMode: mode})); got != want {
			t.Fatal("first_mb策略失效", mode, got)
		}
	}
	// 没有SEI/AUD，仅POC变化；不能依赖first_mb或紧邻VCL。
	next := auSlice("010", "0001", "0100", false)
	src, _ := NewAnnexBReader(bytes.NewReader(auAnnex(params[0], params[1], slice, []byte{12, 0x80}, next)), 0)
	if got := len(auPackets(t, src, AccessUnitConfig{})); got != 2 {
		t.Fatal("filler丢失图像标识", got)
	}
}

type auNALSource struct {
	units   []nal.Unit
	calls   int
	err     error
	scratch []byte
}

func (s *auNALSource) ReadNAL() (nal.Unit, error) {
	s.calls++
	if len(s.units) == 0 {
		if s.err != nil {
			return nal.Unit{}, s.err
		}
		return nal.Unit{}, io.EOF
	}
	u := s.units[0]
	s.units = s.units[1:]
	s.scratch = append(s.scratch[:0], u.Payload...)
	u.Payload = s.scratch
	return u, nil
}
func auUnits(raws ...[]byte) []nal.Unit {
	var out []nal.Unit
	for _, b := range raws {
		h, _ := nal.ParseHeader(b[0])
		out = append(out, nal.Unit{Header: h, Payload: b[1:]})
	}
	return out
}

func TestAUReader_ReusedNALBufferAndAVCC(t *testing.T) {
	params := auParameters()
	i, p := auSlice("1", "0000", "0000", true), auSlice("1", "0001", "0010", false)
	units := auUnits(params[0], params[1], i, p)
	got := auPackets(t, &auNALSource{units: units}, AccessUnitConfig{})
	if !bytes.Equal(got[0].Data, auAnnex(params[0], params[1], i)) || !bytes.Equal(got[1].Data, auAnnex(p)) {
		t.Fatal("源复用缓冲损坏挂起AU")
	}
	// AVCC输入只改变封装，输出统一Annex-B，参数集来自同一NAL源。
	var avcc []byte
	for _, u := range [][]byte{params[0], params[1], i, p} {
		avcc = append(avcc, byte(len(u)))
		avcc = append(avcc, u...)
	}
	src, _ := NewAVCCReader(bytes.NewReader(avcc), 1, 0)
	if other := auPackets(t, src, AccessUnitConfig{}); !reflect.DeepEqual(other, got) {
		t.Fatal("AVCC组装与独立期望不符")
	}
}

func TestAUReader_CABACTerminalAndPrefixTail(t *testing.T) {
	params := auParameters()
	params[0][1] = 77 // CABAC必须使用Main，不能借非法Baseline参数集。
	// PPS entropy_coding_mode_flag=1；I slice头部不含cabac_init_idc。
	cabac := append([]byte{0x68}, auBits("1 1 1 0 1 1 1 0 00 1 1 1 0 0 0 1")...)
	slice := append(auSlice("1", "0000", "0000", true), 0, 0, 3)
	src := &auNALSource{units: auUnits(params[0], cabac, slice)}
	got := auPackets(t, src, AccessUnitConfig{})
	if !bytes.Equal(got[0].Data, auAnnex(params[0], cabac, slice)) {
		t.Fatal("CABAC终端转义未原样保留")
	}
	// 下一个前缀已证明先前AU完整，但无VCL的尾部不能成为另一张图片。
	first := auSlice("1", "0000", "0000", true)
	src = &auNALSource{units: auUnits(params[0], params[1], first, []byte{9, 0xf0})}
	r, _ := NewAccessUnitReader(src, AccessUnitConfig{})
	if p, err := r.ReadAccessUnit(); err != nil || !bytes.Equal(p.Data, auAnnex(params[0], params[1], first)) {
		t.Fatal("坏尾部丢弃了完整前AU", err)
	}
	if p, err := r.ReadAccessUnit(); p.Valid() || !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal("无VCL尾部被当作AU", err)
	}
}

func TestAUReader_ErrorsAndBudgets(t *testing.T) {
	params := auParameters()
	slice := auSlice("1", "0001", "0010", false)
	valid := auUnits(params[0], params[1], slice)
	exactSize := len(auAnnex(params[0], params[1], slice))
	for _, tc := range []struct {
		name            string
		units           []nal.Unit
		cfg             AccessUnitConfig
		sourceErr, want error
	}{
		{"prefix only", valid[:2], AccessUnitConfig{}, nil, h264.ErrInvalidData},
		{"unknown pps", valid[2:], AccessUnitConfig{}, nil, h264.ErrInvalidData},
		{"unknown sps", valid[1:2], AccessUnitConfig{}, nil, h264.ErrInvalidData},
		{"byte budget", valid, AccessUnitConfig{MaxAccessUnitSize: exactSize - 1}, nil, h264.ErrResourceLimit},
		{"count budget", valid, AccessUnitConfig{MaxNALUnits: 2}, nil, h264.ErrResourceLimit},
		{"unit budget", valid, AccessUnitConfig{MaxAccessUnitSize: 4}, nil, h264.ErrResourceLimit},
		{"io", valid, AccessUnitConfig{}, io.ErrClosedPipe, io.ErrClosedPipe},
		{"truncated header", auUnits(params[0], params[1], []byte{0x61}), AccessUnitConfig{}, nil, h264.ErrInvalidData},
		{"truncated data partition", auUnits([]byte{0x62, 0x80}), AccessUnitConfig{}, nil, h264.ErrInvalidData},
		{"PAFF SPS", auUnits([]byte{0x67, 0x4d, 0x00, 0x1e, 0xf6, 0x68, 0xea, 0x90}), AccessUnitConfig{}, nil, h264.ErrUnsupported},
		{"cavlc terminal", auUnits(params[0], params[1], append(append([]byte{}, slice...), 0, 0, 3)), AccessUnitConfig{}, nil, h264.ErrInvalidData},
		{"out of range mb", auUnits(params[0], params[1], auSlice("011", "0001", "0010", false)), AccessUnitConfig{}, nil, h264.ErrInvalidData},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := &auNALSource{units: tc.units, err: tc.sourceErr}
			r, _ := NewAccessUnitReader(src, tc.cfg)
			p, err := r.ReadAccessUnit()
			if p.Valid() || !errors.Is(err, tc.want) {
				t.Fatal("错误分类或部分AU泄露", err)
			}
			if r.size != 0 || r.assembler.Pending() != 0 || r.hasVCL || r.closed {
				t.Fatal("错误后保留AU容量状态")
			}
			calls := src.calls
			if _, again := r.ReadAccessUnit(); again != err || src.calls != calls {
				t.Fatal("错误后继续读取", again)
			}
		})
	}
	if got := auPackets(t, &auNALSource{units: valid}, AccessUnitConfig{MaxAccessUnitSize: exactSize, MaxNALUnits: 3}); len(got) != 1 {
		t.Fatal("恰好预算被拒绝")
	}
	if got := auPackets(t, &auNALSource{}, AccessUnitConfig{}); len(got) != 0 {
		t.Fatal("空流产生AU")
	}
	for _, cfg := range []AccessUnitConfig{{MaxAccessUnitSize: -1}, {MaxNALUnits: -1}, {BoundaryMode: 2}} {
		if _, err := NewAccessUnitReader(&auNALSource{}, cfg); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal(err)
		}
	}
	if _, err := NewAccessUnitReader(nil, AccessUnitConfig{}); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal(err)
	}
}

func TestAUReader_RegisteredCorpusChunkMatrix(t *testing.T) {
	// 数量来自登记语料的独立FFmpeg门禁，不由本库header推导。
	for _, tc := range []struct {
		path           string
		frames, slices int
	}{
		{"baseline/baseline_320x240_yuv420p_cavlc_ip_multislice_003.264", 6, 2},
		{"main/main_320x240_yuv420p_cabac_ip_multislice_004.264", 4, 2},
		{"high/high_192x128_yuv420p_cabac_multislice_007.264", 12, 2},
		{"main/main_64x48_yuv420p_cabac_ipb_001.264", 3, 1},
		{"main/main_16x16_yuv420p_cavlc_poc1_007.264", 6, 1},
	} {
		t.Run(tc.path, func(t *testing.T) {
			raw, err := os.ReadFile("../testdata/" + tc.path)
			if err != nil {
				t.Fatal(err)
			}
			var reference []h264.Packet
			for _, chunk := range []int{len(raw), 1, 2, 3, 7, 4096} {
				src, _ := NewAnnexBReader(annexChunkReader{bytes.NewReader(raw), chunk}, 0)
				got := auPackets(t, src, AccessUnitConfig{})
				if len(got) != tc.frames {
					t.Fatalf("chunk%d AU数=%d，期望%d", chunk, len(got), tc.frames)
				}
				for _, p := range got {
					units, err := nal.SplitAnnexB(p.Data)
					if err != nil {
						t.Fatal(err)
					}
					n := 0
					for _, u := range units {
						if u.IsVCL() {
							n++
						}
					}
					if n != tc.slices {
						t.Fatal("多slice被合并或拆散", n)
					}
				}
				if reference == nil {
					reference = got
				} else if !reflect.DeepEqual(got, reference) {
					t.Fatal(fmt.Sprintf("chunk%d输出变化", chunk))
				}
			}
		})
	}
}
