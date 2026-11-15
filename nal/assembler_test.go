package nal

import "testing"

// pushAll 推入全部 unit 并收集完成的 AU，最后 Flush。
func pushAll(t *testing.T, units []Unit, infos []*SliceInfo) []*AccessUnit {
	t.Helper()
	a := NewAssembler()
	var out []*AccessUnit
	for i, u := range units {
		var info *SliceInfo
		if i < len(infos) {
			info = infos[i]
		}
		if au := a.Push(u, info); au != nil {
			out = append(out, au)
		}
	}
	if au := a.Flush(); au != nil {
		out = append(out, au)
	}
	return out
}

func typesOf(au *AccessUnit) []UnitType {
	out := make([]UnitType, len(au.Units))
	for i, u := range au.Units {
		out[i] = u.Header.Type
	}
	return out
}

func equalTypes(got []UnitType, want ...UnitType) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestAssembler_PrefixAndTwoPictures 验证前缀 NAL 归属与多帧切分。
func TestAssembler_PrefixAndTwoPictures(t *testing.T) {
	units := []Unit{
		unitOf(TypeSPS, 3),
		unitOf(TypePPS, 3),
		unitOf(TypeSEI, 0),
		unitOf(TypeSliceIDR, 3),
		unitOf(TypeSPS, 3),
		unitOf(TypePPS, 3),
		unitOf(TypeSliceIDR, 3),
	}
	infos := []*SliceInfo{
		nil, nil, nil,
		{FirstMBInSlice: 0, FrameNum: 0, IDR: true, NALRefIDC: 3},
		nil, nil,
		{FirstMBInSlice: 0, FrameNum: 0, IDR: true, NALRefIDC: 3},
	}
	aus := pushAll(t, units, infos)
	if len(aus) != 2 {
		t.Fatalf("得到 %d 个 AU，期望 2", len(aus))
	}
	if got := typesOf(aus[0]); !equalTypes(got, TypeSPS, TypePPS, TypeSEI, TypeSliceIDR) {
		t.Fatalf("AU0 = %v", got)
	}
	if got := typesOf(aus[1]); !equalTypes(got, TypeSPS, TypePPS, TypeSliceIDR) {
		t.Fatalf("AU1 = %v", got)
	}
}

// TestAssembler_MultiSlice 验证同一帧的多个 slice 不被切分。
func TestAssembler_MultiSlice(t *testing.T) {
	base := SliceInfo{FrameNum: 7, PicParameterSetID: 0, HasPicOrderCnt: true, PicOrderCntLsb: 14, NALRefIDC: 3}
	slices := []SliceInfo{
		base,
		base,
		base,
	}
	slices[0].FirstMBInSlice = 0
	slices[1].FirstMBInSlice = 9
	slices[2].FirstMBInSlice = 18
	units := []Unit{unitOf(TypeSlice, 3), unitOf(TypeSlice, 3), unitOf(TypeSlice, 3)}
	aus := pushAll(t, units, []*SliceInfo{&slices[0], &slices[1], &slices[2]})
	if len(aus) != 1 || aus[0].VCLCount() != 3 {
		t.Fatalf("多 slice 帧被切分: %d AU", len(aus))
	}

	// 下一帧的首 slice 开启新 AU。
	next := base
	next.FrameNum = 8
	next.PicOrderCntLsb = 16
	next.FirstMBInSlice = 0
	a := NewAssembler()
	for i, u := range units {
		if au := a.Push(u, &slices[i]); au != nil {
			t.Fatalf("第 %d 个 slice 不应完成 AU", i)
		}
	}
	if au := a.Push(unitOf(TypeSlice, 3), &next); au == nil {
		t.Fatal("下一帧首 slice 应完成前一 AU")
	}
	if au := a.Flush(); au == nil || au.VCLCount() != 1 {
		t.Fatal("Flush 应返回新帧")
	}
}

// TestAssembler_AUD 验证 AUD 开启新 AU。
func TestAssembler_AUD(t *testing.T) {
	units := []Unit{
		unitOf(TypeAUD, 0), unitOf(TypeSliceIDR, 3),
		unitOf(TypeAUD, 0), unitOf(TypeSliceIDR, 3),
	}
	aus := pushAll(t, units, nil)
	if len(aus) != 2 {
		t.Fatalf("得到 %d 个 AU，期望 2", len(aus))
	}
	if !equalTypes(typesOf(aus[0]), TypeAUD, TypeSliceIDR) {
		t.Fatalf("AU0 = %v", typesOf(aus[0]))
	}
}

// TestAssembler_EndOfSequenceClosesAU 验证 EOS 归入当前 AU 并关闭它。
func TestAssembler_EndOfSequenceClosesAU(t *testing.T) {
	units := []Unit{
		unitOf(TypeSliceIDR, 3),
		unitOf(TypeEndOfSequence, 3),
		unitOf(TypeSPS, 3),
		unitOf(TypeSliceIDR, 3),
	}
	aus := pushAll(t, units, nil)
	if len(aus) != 2 {
		t.Fatalf("得到 %d 个 AU，期望 2", len(aus))
	}
	if !equalTypes(typesOf(aus[0]), TypeSliceIDR, TypeEndOfSequence) {
		t.Fatalf("AU0 = %v", typesOf(aus[0]))
	}
	if !equalTypes(typesOf(aus[1]), TypeSPS, TypeSliceIDR) {
		t.Fatalf("AU1 = %v", typesOf(aus[1]))
	}
}

// TestAssembler_HintOverridesDetection 验证显式 hint 覆盖自动判定。
func TestAssembler_HintOverridesDetection(t *testing.T) {
	info := &SliceInfo{FirstMBInSlice: 5, FrameNum: 1, NALRefIDC: 3}
	a := NewAssembler()
	if au := a.Push(unitOf(TypeSlice, 3), info); au != nil {
		t.Fatal("首个 unit 不应完成 AU")
	}
	if au := a.PushHinted(unitOf(TypeSlice, 3), info, BoundaryNew); au == nil {
		t.Fatal("显式 New 应完成当前 AU")
	}
	if au := a.Flush(); au == nil || len(au.Units) != 1 {
		t.Fatal("Flush 应返回新 AU")
	}
}

// TestAssembler_UnknownTreatedAsSame 验证缺少 slice 信息时按同一 AU 处理。
func TestAssembler_UnknownTreatedAsSame(t *testing.T) {
	a := NewAssembler()
	if au := a.Push(unitOf(TypeSlice, 3), nil); au != nil {
		t.Fatal("首个 unit 不应完成 AU")
	}
	if au := a.Push(unitOf(TypeSlice, 3), nil); au != nil {
		t.Fatal("Unknown 不应完成 AU")
	}
	if au := a.Flush(); au == nil || len(au.Units) != 2 {
		t.Fatal("Flush 应返回包含两个 unit 的 AU")
	}
}

// TestAssembler_FlushAndReset 验证 Flush 空状态与重置语义。
func TestAssembler_FlushAndReset(t *testing.T) {
	a := NewAssembler()
	if au := a.Flush(); au != nil {
		t.Fatal("空组装器 Flush 应返回 nil")
	}
	a.Push(unitOf(TypeSPS, 3), nil)
	if a.Pending() != 1 {
		t.Fatalf("Pending = %d，期望 1", a.Pending())
	}
	if au := a.Flush(); au == nil || len(au.Units) != 1 {
		t.Fatal("Flush 应返回挂起的 AU")
	}
	if a.Pending() != 0 {
		t.Fatalf("Flush 后 Pending = %d", a.Pending())
	}
	if au := a.Flush(); au != nil {
		t.Fatal("第二次 Flush 应返回 nil")
	}
}

func TestAssembler_ClearConsumedUnitSlots(t *testing.T) {
	a := NewAssembler()
	u := unitOf(TypeSPS, 3)
	u.Payload = []byte{1, 2, 3}
	a.Push(u, nil)
	backing := a.pending
	if au := a.Flush(); au == nil {
		t.Fatal("Flush 应返回挂起的 AU")
	}
	if backing[0].Payload != nil {
		t.Fatal("已交付 AU 的 pending 槽仍保留 payload")
	}

	u = unitOf(TypePPS, 3)
	u.Payload = []byte{4, 5, 6}
	a.Push(u, nil)
	backing = a.pending
	a.reset()
	if backing[0].Payload != nil {
		t.Fatal("reset 后的 pending 槽仍保留 payload")
	}
}

// TestAccessUnit_Helpers 验证 AU 查询方法。
func TestAccessUnit_Helpers(t *testing.T) {
	au := &AccessUnit{Units: []Unit{
		unitOf(TypeSPS, 3),
		unitOf(TypeSliceIDR, 3),
		unitOf(TypeSlice, 3),
	}}
	if !au.HasVCL() || au.VCLCount() != 2 || !au.IsIDR() {
		t.Fatalf("helpers = hasVCL=%v vcl=%d idr=%v", au.HasVCL(), au.VCLCount(), au.IsIDR())
	}
	var nilAU *AccessUnit
	if nilAU.HasVCL() || nilAU.VCLCount() != 0 || nilAU.IsIDR() {
		t.Fatal("nil AccessUnit 的 helper 应安全返回零值")
	}
}
