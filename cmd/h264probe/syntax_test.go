package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strconv"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

func syntaxParameters() (*syntax.SPS, *syntax.PPS) {
	s := &syntax.SPS{
		ProfileIDC: 100, LevelIDC: 30, SeqParameterSetID: 3, ChromaFormatIDC: 1,
		MaxNumRefFrames: 2, PicWidthInMbsMinus1: 1, PicHeightInMapUnitsMinus1: 1,
		FrameMbsOnlyFlag: true, Direct8x8InferenceFlag: true,
		SeqScalingMatrixPresentFlag: true, VUIParametersPresentFlag: true,
		VUI: &syntax.VUI{
			AspectRatioInfoPresentFlag: true, AspectRatioIDC: 255, SARWidth: 4, SARHeight: 3,
			TimingInfoPresentFlag: true, NumUnitsInTick: 1001, TimeScale: 60000, FixedFrameRateFlag: true,
		},
	}
	s.SeqScalingListPresentFlag[0] = true
	s.SeqScalingList[0] = &syntax.ScalingList{Size: 4, Deltas: []int32{-8}}
	s.SeqScalingListPresentFlag[6] = true
	deltas := make([]int32, 64)
	deltas[0] = 8
	s.SeqScalingList[6] = &syntax.ScalingList{Size: 8, Deltas: deltas}
	p := &syntax.PPS{
		PicParameterSetID: 7, SeqParameterSetID: 3, EntropyCodingModeFlag: true,
		PicInitQPMinus26: -3, DeblockingFilterControlPresentFlag: true,
		HasOptionalBlock: true, Transform8x8ModeFlag: true, PicScalingMatrixPresentFlag: true,
	}
	p.PicScalingListPresentFlag[0] = true
	p.PicScalingList[0] = &syntax.ScalingList{Size: 4, Deltas: []int32{-8}}
	return s, p
}

func syntaxParameterUnits(t *testing.T, s *syntax.SPS, p *syntax.PPS) []nal.Unit {
	t.Helper()
	srbsp, err := s.MarshalRBSP()
	if err != nil {
		t.Fatal(err)
	}
	prbsp, err := p.MarshalRBSP(s)
	if err != nil {
		t.Fatal(err)
	}
	return []nal.Unit{
		{Header: nal.Header{RefIDC: 3, Type: nal.TypeSPS}, Payload: bitstream.RBSPToEBSP(srbsp)},
		{Header: nal.Header{RefIDC: 3, Type: nal.TypePPS}, Payload: bitstream.RBSPToEBSP(prbsp)},
	}
}

// 合成头部后不提供合法残差，确保探测器只消费共享 slice header 解析器。
func syntaxSlice(t *testing.T, ppsID, firstMB uint32) nal.Unit {
	t.Helper()
	w := bitstream.NewWriter()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(w.WriteUE(firstMB))
	check(w.WriteUE(2))
	check(w.WriteUE(ppsID))
	check(w.WriteBits(4, 0))
	check(w.WriteUE(4))
	check(w.WriteBits(4, 0))
	check(w.WriteBit(0))
	check(w.WriteBit(1))
	check(w.WriteSE(2))
	check(w.WriteUE(0))
	check(w.WriteSE(1))
	check(w.WriteSE(-1))
	check(bitstream.WriteRBSPTrailingBits(w))
	return nal.Unit{Header: nal.Header{RefIDC: 3, Type: nal.TypeSliceIDR}, Payload: bitstream.RBSPToEBSP(w.Bytes())}
}

func syntaxAnnexB(units []nal.Unit) []byte {
	var b []byte
	for _, u := range units {
		b = append(b, 0, 0, 0, 1)
		b = append(b, u.Bytes()...)
	}
	return b
}

func syntaxRecords(t *testing.T, data []byte) []syntaxRecord {
	t.Helper()
	var records []syntaxRecord
	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var r syntaxRecord
		if err := dec.Decode(&r); errors.Is(err, io.EOF) {
			return records
		} else if err != nil {
			t.Fatal(err)
		}
		records = append(records, r)
	}
}

func richSyntaxUnits(t *testing.T) []nal.Unit {
	t.Helper()
	s, p := syntaxParameters()
	units := syntaxParameterUnits(t, s, p)
	sei := &syntax.SEI{Messages: []syntax.SEIMessage{
		{Type: 300, Payload: []byte{1, 2, 3}},
		{Type: syntax.SEIPayloadTypeUserDataUnregistered, Payload: append(bytes.Repeat([]byte{9}, 16), []byte("中文标记")...)},
	}}
	rbsp, err := sei.MarshalRBSP()
	if err != nil {
		t.Fatal(err)
	}
	units = append(units, nal.Unit{Header: nal.Header{Type: nal.TypeSEI}, Payload: bitstream.RBSPToEBSP(rbsp)})
	return append(units, syntaxSlice(t, 7, 0), syntaxSlice(t, 7, 2))
}

func TestSyntax_JSONLinesNestedSyntaxAndMultislice(t *testing.T) {
	units := richSyntaxUnits(t)
	var out bytes.Buffer
	if err := runWithInput([]string{"-syntax", "-input", "-"}, bytes.NewReader(syntaxAnnexB(units)), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	records := syntaxRecords(t, out.Bytes())
	if len(records) != len(units) || bytes.Count(out.Bytes(), []byte{'\n'}) != len(units) {
		t.Fatal("未按NAL逐行输出", out.String())
	}
	for i, r := range records {
		if r.Index != i || r.Header != units[i].Header || r.Bytes != len(units[i].Bytes()) || r.Type != units[i].Header.Type.String() {
			t.Fatalf("第%d条NAL元数据错误: %+v", i, r)
		}
	}
	s, p := records[0].SPS, records[1].PPS
	if s == nil || s.SeqParameterSetID != 3 || s.VUI == nil || s.VUI.TimeScale != 60000 || s.VUI.NumUnitsInTick != 1001 || s.VUI.SARWidth != 4 {
		t.Fatal("SPS/VUI字段丢失", s)
	}
	if s.SeqScalingList[0] == nil || len(s.SeqScalingList[0].Deltas) != 1 || s.SeqScalingList[0].Deltas[0] != -8 || s.SeqScalingList[6] == nil || len(s.SeqScalingList[6].Values) != 64 || s.SeqScalingList[6].Values[0] != 16 {
		t.Fatal("SPS缩放矩阵丢失", s.SeqScalingList)
	}
	if p == nil || p.PicParameterSetID != 7 || p.SeqParameterSetID != 3 || !p.EntropyCodingModeFlag || !p.Transform8x8ModeFlag || p.PicScalingList[0] == nil {
		t.Fatal("PPS扩展字段丢失", p)
	}
	sei := records[2].SEI
	if sei == nil || len(sei.Messages) != 2 || sei.Messages[0].Type != 300 || !bytes.Equal(sei.Messages[0].Payload, []byte{1, 2, 3}) {
		t.Fatal("SEI消息丢失", sei)
	}
	user, err := syntax.ParseUserDataUnregistered(sei.Messages[1].Payload)
	if err != nil || string(user.Data) != "中文标记" {
		t.Fatal("SEI用户数据丢失", user, err)
	}
	for i, mb := range []uint32{0, 2} {
		h := records[i+3].SliceHeader
		if h == nil || h.FirstMBInSlice != mb || h.PicParameterSetID != 7 || h.IDRPicID != 4 || h.SliceQP != 25 || !h.LongTermReferenceFlag || h.SliceAlphaC0OffsetDiv2 != 1 || h.SliceBetaOffsetDiv2 != -1 || h.HeaderBits == 0 {
			t.Fatal("多slice头部字段丢失", h)
		}
	}
}

func TestSyntax_AVCCLengthsAndGOPCompatibility(t *testing.T) {
	units := richSyntaxUnits(t)
	for size := 1; size <= 4; size++ {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			data, err := nal.MarshalAVCC(units, size)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"-input", "-", "-format", "avcc", "-length-size", strconv.Itoa(size)}
			var out bytes.Buffer
			if err := runWithInput(append(args, "-syntax"), bytes.NewReader(data), &out, io.Discard); err != nil {
				t.Fatal(err)
			}
			if got := syntaxRecords(t, out.Bytes()); len(got) != len(units) || got[4].SliceHeader.FirstMBInSlice != 2 {
				t.Fatal("AVCC语法树错误", got)
			}
			out.Reset()
			if err := runWithInput(append(args, "-expect-frames", "1", "-expect-max-b", "0", "-expect-idr-interval", "1"), bytes.NewReader(data), &out, io.Discard); err != nil {
				t.Fatal(err)
			}
			var got report
			if err := json.Unmarshal(out.Bytes(), &got); err != nil || len(got.Pictures) != 1 || got.Pictures[0].Slices != 2 {
				t.Fatal("默认GOP JSON不兼容", got, err)
			}
		})
	}
}

func TestSyntax_ParameterSetsFollowNALOrder(t *testing.T) {
	s, p := syntaxParameters()
	units := syntaxParameterUnits(t, s, p)
	units = append(units, syntaxSlice(t, 7, 0))
	// 保留旧 PPS，新增另一个 ID，并改变 QP，验证引用不会取到最后出现的 PPS。
	p.PicParameterSetID, p.PicInitQPMinus26 = 11, 4
	next := syntaxParameterUnits(t, s, p)
	units = append(units, next[1], syntaxSlice(t, 7, 2), syntaxSlice(t, 11, 0))
	// 同 ID 替换仅作用于后续 NAL，先前输出的语法记录不得被回写。
	p.PicParameterSetID, p.PicInitQPMinus26 = 7, 7
	next = syntaxParameterUnits(t, s, p)
	units = append(units, next[1], syntaxSlice(t, 7, 0))
	var out bytes.Buffer
	if err := runWithInput([]string{"-syntax", "-input", "-"}, bytes.NewReader(syntaxAnnexB(units)), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	r := syntaxRecords(t, out.Bytes())
	qps := []int{r[2].SliceHeader.SliceQP, r[4].SliceHeader.SliceQP, r[5].SliceHeader.SliceQP, r[7].SliceHeader.SliceQP}
	if !reflect.DeepEqual(qps, []int{25, 25, 32, 35}) || r[1].PPS.PicInitQPMinus26 != -3 || r[6].PPS.PicInitQPMinus26 != 7 {
		t.Fatal("参数集未按序生效", qps)
	}
	for name, reordered := range map[string][]nal.Unit{
		"PPS早于SPS":   {units[1], units[0]},
		"slice早于PPS": {units[0], units[2], units[1]},
		"未知PPS":      {units[0], units[1], syntaxSlice(t, 19, 0)},
	} {
		t.Run(name, func(t *testing.T) {
			if err := runWithInput([]string{"-syntax", "-input", "-"}, bytes.NewReader(syntaxAnnexB(reordered)), io.Discard, io.Discard); !errors.Is(err, h264.ErrInvalidData) {
				t.Fatal("参数集顺序错误未被拒绝", err)
			}
		})
	}
}

func TestSyntax_CABACTerminalEscapeRequiresKnownPPS(t *testing.T) {
	s, p := syntaxParameters()
	for _, cabac := range []bool{false, true} {
		p.EntropyCodingModeFlag = cabac
		units := syntaxParameterUnits(t, s, p)
		u := syntaxSlice(t, 7, 0)
		u.Payload = append(u.Payload, 0, 0, 3)
		units = append(units, u)
		err := runWithInput([]string{"-syntax", "-input", "-"}, bytes.NewReader(syntaxAnnexB(units)), io.Discard, io.Discard)
		if cabac && err != nil || !cabac && !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("CABAC=%v终端转义门禁错误: %v", cabac, err)
		}
		if err := runWithInput([]string{"-syntax", "-input", "-"}, bytes.NewReader(syntaxAnnexB([]nal.Unit{u})), io.Discard, io.Discard); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal("无PPS时接受了终端转义", err)
		}
	}
	units := syntaxParameterUnits(t, s, p)
	units[0].Payload = append(units[0].Payload, 0, 0, 3)
	if err := runWithInput([]string{"-syntax", "-input", "-"}, bytes.NewReader(syntaxAnnexB(units)), io.Discard, io.Discard); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal("参数集接受了CABAC专用终端转义", err)
	}
}

func TestSyntax_ErrorsTruncationAndLimits(t *testing.T) {
	units := richSyntaxUnits(t)
	for _, tc := range []struct {
		name string
		data []byte
		args []string
		want error
	}{
		{"非法NAL头", []byte{0, 0, 1, 0xe7, 0x80}, nil, h264.ErrInvalidData},
		{"截断SPS", syntaxAnnexB([]nal.Unit{{Header: units[0].Header, Payload: []byte{100}}}), nil, h264.ErrInvalidData},
		{"截断PPS", syntaxAnnexB([]nal.Unit{units[0], {Header: units[1].Header, Payload: []byte{0}}}), nil, h264.ErrInvalidData},
		{"截断slice头", syntaxAnnexB(append(units[:2:2], nal.Unit{Header: units[3].Header, Payload: units[3].Payload[:2]})), nil, h264.ErrInvalidData},
		{"截断SEI", syntaxAnnexB([]nal.Unit{{Header: nal.Header{Type: nal.TypeSEI}, Payload: []byte{5, 16, 1, 0x80}}}), nil, h264.ErrInvalidData},
		{"截断数据分区A头", syntaxAnnexB([]nal.Unit{{Header: nal.Header{Type: nal.TypeSlicePartitionA}, Payload: []byte{0x80}}}), nil, h264.ErrInvalidData},
		{"AVCC前缀截断", []byte{0, 0}, []string{"-format", "avcc"}, h264.ErrInvalidData},
		{"AVCC负载截断", []byte{0, 0, 0, 5, 0x67, 100}, []string{"-format", "avcc"}, h264.ErrInvalidData},
		{"AVCC先限额", []byte{0xff, 0xff, 0xff, 0xff}, []string{"-format", "avcc", "-max-nal-size", "16"}, h264.ErrResourceLimit},
		{"AnnexB先限额", syntaxAnnexB(units), []string{"-max-nal-size", "4"}, h264.ErrResourceLimit},
		{"NAL数限额", syntaxAnnexB(units), []string{"-max-nals", "2"}, h264.ErrResourceLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"-syntax", "-input", "-"}, tc.args...)
			if err := runWithInput(args, bytes.NewReader(tc.data), io.Discard, io.Discard); !errors.Is(err, tc.want) {
				t.Fatalf("错误分类=%v，期望%v", err, tc.want)
			}
		})
	}
	for _, args := range [][]string{
		{"-format", "guess"}, {"-length-size", "0"}, {"-length-size", "5"},
		{"-max-nals", "0"}, {"-max-nals", "16777217"}, {"-max-nal-size", "0"}, {"-max-nal-size", "67108865"},
		{"-expect-frames", "1"}, {"-expect-max-b", "0"}, {"-expect-idr-interval", "1"},
	} {
		if err := runWithInput(append([]string{"-syntax", "-input", "-"}, args...), bytes.NewReader(nil), io.Discard, io.Discard); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal("无效选项未拒绝", args, err)
		}
	}
	var out bytes.Buffer
	data := syntaxAnnexB(units)
	if err := runWithInput([]string{"-syntax", "-input", "-", "-max-nals", strconv.Itoa(len(units))}, bytes.NewReader(data), &out, io.Discard); err != nil {
		t.Fatal("恰好达到NAL上限仍应成功", err)
	}
	s, p := syntaxParameters()
	s.PicWidthInMbsMinus1 = h264.MaxFrameWidth / 16
	s.LevelIDC = 60 // 合法Level6几何，独立触发库8192像素预算而非Annex A错误。
	if err := runWithInput([]string{"-syntax", "-input", "-"}, bytes.NewReader(syntaxAnnexB(syntaxParameterUnits(t, s, p))), io.Discard, io.Discard); !errors.Is(err, h264.ErrResourceLimit) {
		t.Fatal("SPS尺寸超限未拒绝", err)
	}
}

type syntaxFailedWriter struct{ err error }

func (w syntaxFailedWriter) Write([]byte) (int, error) { return 0, w.err }

func TestSyntax_PropagatesOutputFailure(t *testing.T) {
	want := errors.New("输出设备失败")
	if err := runWithInput([]string{"-syntax", "-input", "-"}, bytes.NewReader(syntaxAnnexB(richSyntaxUnits(t))), syntaxFailedWriter{want}, io.Discard); !errors.Is(err, want) {
		t.Fatal("输出错误未传播", err)
	}
}

// 第二次 Read 必须发生在首条 JSON 输出之后，防止实现退化为整流读取。
type syntaxStreamingReader struct {
	first, rest []byte
	out         *bytes.Buffer
}

func (r *syntaxStreamingReader) Read(p []byte) (int, error) {
	if len(r.first) != 0 {
		n := copy(p, r.first)
		r.first = r.first[n:]
		return n, nil
	}
	if r.out.Len() == 0 {
		return 0, errors.New("读取第二个NAL前尚未输出第一个NAL")
	}
	if len(r.rest) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.rest)
	r.rest = r.rest[n:]
	return n, nil
}

func TestSyntax_StreamsBeforeReadingWholeInput(t *testing.T) {
	units := richSyntaxUnits(t)
	first := append(syntaxAnnexB(units[:1]), 0, 0, 0, 1)
	rest := syntaxAnnexB(units[1:])[4:]
	var out bytes.Buffer
	r := &syntaxStreamingReader{first: first, rest: rest, out: &out}
	if err := runWithInput([]string{"-syntax", "-input", "-"}, r, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(syntaxRecords(t, out.Bytes())) != len(units) {
		t.Fatal("流式输出不完整")
	}
}

func TestSyntax_SPSSelectionUsesReferencedID(t *testing.T) {
	s, p := syntaxParameters()
	units := syntaxParameterUnits(t, s, p)
	// 新 SPS/PPS 不得覆盖旧 ID 的上下文；新宽度可容纳 first_mb=4，旧宽度不能。
	s.SeqParameterSetID, s.PicWidthInMbsMinus1 = 4, 2
	p.SeqParameterSetID, p.PicParameterSetID = 4, 11
	units = append(units, syntaxParameterUnits(t, s, p)...)
	units = append(units, syntaxSlice(t, 11, 4), syntaxSlice(t, 7, 2))
	var out bytes.Buffer
	if err := runWithInput([]string{"-syntax", "-input", "-"}, bytes.NewReader(syntaxAnnexB(units)), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	r := syntaxRecords(t, out.Bytes())
	if len(r) != 6 || r[0].SPS.SeqParameterSetID != 3 || r[2].SPS.SeqParameterSetID != 4 || r[4].SliceHeader.PicParameterSetID != 11 || r[5].SliceHeader.PicParameterSetID != 7 {
		t.Fatal("SPS/PPS引用关系错误", r)
	}
	units = append(units, syntaxSlice(t, 7, 4))
	out.Reset()
	if err := runWithInput([]string{"-syntax", "-input", "-"}, bytes.NewReader(syntaxAnnexB(units)), &out, io.Discard); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatal("错误使用了最近的SPS，旧PPS的越界地址未拒绝", err)
	}
	if len(syntaxRecords(t, out.Bytes())) != 6 {
		t.Fatal("后续错误破坏了已经输出的NAL记录")
	}
}

func TestSyntax_StrictAnnexBFraming(t *testing.T) {
	valid := syntaxAnnexB(richSyntaxUnits(t))
	for _, data := range [][]byte{
		append([]byte{0x7f}, valid...),
		append(append([]byte(nil), valid...), 0, 0, 1),
		{0, 0},
	} {
		if err := runWithInput([]string{"-syntax", "-input", "-"}, bytes.NewReader(data), io.Discard, io.Discard); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatal("损坏Annex-B封装被当作正常结束", err)
		}
	}
}
