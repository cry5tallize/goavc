package encoder

import (
	"bytes"
	"testing"

	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/internal/entropy/cabac"
)

func TestCABACPadding_AnalyticBinBudget(t *testing.T) {
	for _, tc := range []struct {
		bins uint64
		mbs  int
		want int
	}{
		{0, 1, 1}, {117, 1, 1}, {118, 1, 4}, {1000, 1, 85}, {1000, 2, 76},
	} {
		got := cabacZeroWordPayload([]byte{0x80}, tc.bins, tc.mbs, 8)
		if len(got) != tc.want {
			t.Fatalf("bins=%d MB=%d 封装长度%d，期望%d", tc.bins, tc.mbs, len(got), tc.want)
		}
		if tc.bins*96 > uint64(len(got)+1)*1024+uint64(tc.mbs)*3072*3 {
			t.Fatal("填充后仍超出bin密度")
		}
		if got[0] != 0x80 {
			t.Fatal("改变原始RBSP")
		}
	}
	if !bytes.Equal(cabacZeroWordPayload([]byte{0x80}, 0, 1, 8), []byte{0x80}) {
		t.Fatal("无需填充时改变码流")
	}
}

func TestCABACPadding_DenseArithmeticStream(t *testing.T) {
	w := bitstream.NewWriter()
	ce, err := cabac.NewEncoder(w)
	if err != nil {
		t.Fatal(err)
	}
	ctx := cabac.Context{State: 63}
	for range 5000 {
		ce.Bin(&ctx, 0)
	}
	ce.Terminate(1)
	if err := ce.Err(); err != nil {
		t.Fatal(err)
	}
	payload := cabacZeroWordPayload(w.Bytes(), ce.BinCount(), 1, 8)
	if len(payload) <= len(w.Bytes()) {
		t.Fatal("高密度MPS未触发填充")
	}
	// 算术读回不依赖 EBSP 封装；先剥离专用终端03，再走严格去转义。
	rbsp, err := bitstream.EBSPToRBSP(payload[:len(payload)-1])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rbsp[:len(w.Bytes())], w.Bytes()) {
		t.Fatal("封装改变算术前缀")
	}
	d, err := cabac.New(bitstream.NewReader(rbsp))
	if err != nil {
		t.Fatal(err)
	}
	ctx = cabac.Context{State: 63}
	for i := 0; i < 5000; i++ {
		if d.Bin(&ctx) != 0 {
			t.Fatalf("bin%d被填充改变", i)
		}
	}
	if d.Terminate() != 1 || d.Err() != nil {
		t.Fatal("终止位读回错误", d.Err())
	}
}
