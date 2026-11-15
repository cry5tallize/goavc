package syntax

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/bitstream"
)

// encodeScalingList 用给定 delta 序列生成完整 RBSP（含尾位）。
func encodeScalingList(t *testing.T, deltas []int32) []byte {
	t.Helper()
	w := bitstream.NewWriter()
	for i, d := range deltas {
		if err := w.WriteSE(d); err != nil {
			t.Fatalf("写入 delta[%d] 失败: %v", i, err)
		}
	}
	if err := bitstream.WriteRBSPTrailingBits(w); err != nil {
		t.Fatalf("写入尾位失败: %v", err)
	}
	return w.Bytes()
}

func TestParseScalingList_AllZeroDeltas(t *testing.T) {
	deltas := make([]int32, 16)
	rbsp := encodeScalingList(t, deltas)
	r := bitstream.NewReader(rbsp)
	list, err := ParseScalingList(r, 4)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if list.Size != 4 || list.Count() != 16 {
		t.Fatalf("Size=%d Count=%d", list.Size, list.Count())
	}
	if len(list.Deltas) != 16 {
		t.Fatalf("Deltas 长度 = %d，期望 16", len(list.Deltas))
	}
	for i, v := range list.Values {
		if v != 8 {
			t.Fatalf("Values[%d] = %d，期望 8", i, v)
		}
	}
}

func TestParseScalingList_NextScaleZero(t *testing.T) {
	// 第一个 delta -8 使 nextScale 变为 0；后续不再读取 delta。
	rbsp := encodeScalingList(t, []int32{-8})
	r := bitstream.NewReader(rbsp)
	list, err := ParseScalingList(r, 4)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(list.Deltas) != 1 || list.Deltas[0] != -8 {
		t.Fatalf("Deltas = %v", list.Deltas)
	}
	for i, v := range list.Values {
		if v != 8 {
			t.Fatalf("Values[%d] = %d，期望 8", i, v)
		}
	}
}

func TestParseScalingList_ValueProgression(t *testing.T) {
	deltas := []int32{2, 5, -5, -2}
	rbsp := encodeScalingList(t, append(deltas, make([]int32, 12)...))
	r := bitstream.NewReader(rbsp)
	list, err := ParseScalingList(r, 4)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	want := []int32{10, 15, 10, 8}
	for i, v := range want {
		if list.Values[i] != v {
			t.Fatalf("Values[%d] = %d，期望 %d", i, list.Values[i], v)
		}
	}
}

func TestScalingList_RoundTrip(t *testing.T) {
	deltas := []int32{2, 5, -5, -2, 0, 1, -1, 3, -3, 8, -8, 127, -128, 0, 0, 0}
	for _, size := range []int{4, 8} {
		count := size * size
		input := make([]int32, count)
		copy(input, deltas)
		rbsp := encodeScalingList(t, input)
		r := bitstream.NewReader(rbsp)
		list, err := ParseScalingList(r, size)
		if err != nil {
			t.Fatalf("size=%d 解析失败: %v", size, err)
		}

		w := bitstream.NewWriter()
		if err := list.WriteTo(w); err != nil {
			t.Fatalf("size=%d 写出失败: %v", size, err)
		}
		if err := bitstream.WriteRBSPTrailingBits(w); err != nil {
			t.Fatalf("写入尾位失败: %v", err)
		}
		if !bytes.Equal(w.Bytes(), rbsp) {
			t.Fatalf("size=%d 往返字节不一致:\n got %x\nwant %x", size, w.Bytes(), rbsp)
		}

		again, err := ParseScalingList(bitstream.NewReader(w.Bytes()), size)
		if err != nil {
			t.Fatalf("size=%d 二次解析失败: %v", size, err)
		}
		if !reflect.DeepEqual(again, list) {
			t.Fatalf("size=%d 结构不一致", size)
		}
	}
}

func TestParseScalingList_Errors(t *testing.T) {
	t.Run("bad size", func(t *testing.T) {
		if _, err := ParseScalingList(bitstream.NewReader(nil), 5); err == nil {
			t.Fatal("size=5 应返回错误")
		}
	})
	t.Run("truncated", func(t *testing.T) {
		if _, err := ParseScalingList(bitstream.NewReader(nil), 4); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
		}
	})
	t.Run("delta out of range", func(t *testing.T) {
		rbsp := encodeScalingList(t, []int32{128})
		if _, err := ParseScalingList(bitstream.NewReader(rbsp), 4); !errors.Is(err, h264.ErrInvalidData) {
			t.Fatalf("错误 = %v，期望 ErrInvalidData", err)
		}
	})
}

func TestScalingList_WriteErrors(t *testing.T) {
	w := bitstream.NewWriter()
	if err := (&ScalingList{Size: 5}).WriteTo(w); err == nil {
		t.Fatal("size=5 应返回错误")
	}
	var nilList *ScalingList
	if err := nilList.WriteTo(w); err == nil {
		t.Fatal("nil list 应返回错误")
	}
	if err := (&ScalingList{Size: 4}).WriteTo(w); err == nil {
		t.Fatal("delta 不足应返回错误")
	}
	if err := (&ScalingList{Size: 4, Deltas: make([]int32, 17)}).WriteTo(w); err == nil {
		t.Fatal("delta 过多应返回错误")
	}
	if err := (&ScalingList{Size: 4, Deltas: []int32{128}}).WriteTo(w); err == nil {
		t.Fatal("delta 越界应返回错误")
	}
}
