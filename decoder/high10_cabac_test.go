package decoder

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/entropy/cabac"
	"github.com/goavc/h264/internal/transform"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

// 定向覆盖负SliceQPY初始化与64项QP回绕；像素结果仍由FFmpeg独立判定。
func high10CABACQPStream(t *testing.T, initialQP int) []byte {
	t.Helper()
	s := &syntax.SPS{ProfileIDC: 110, LevelIDC: 10, ChromaFormatIDC: 1, BitDepthLumaMinus8: 2, BitDepthChromaMinus8: 2,
		PicWidthInMbsMinus1: 3, MaxNumRefFrames: 1, FrameMbsOnlyFlag: true, Direct8x8InferenceFlag: true}
	p := &syntax.PPS{EntropyCodingModeFlag: true, DeblockingFilterControlPresentFlag: true}
	srb, err := s.MarshalRBSP()
	if err != nil {
		t.Fatal(err)
	}
	prb, err := p.MarshalRBSP(s)
	if err != nil {
		t.Fatal(err)
	}
	w := bitstream.NewWriter()
	w.WriteUE(0)
	w.WriteUE(2)
	w.WriteUE(0)
	w.WriteBits(4, 0)
	w.WriteUE(0)
	w.WriteBits(4, 0)
	w.WriteBits(2, 0)
	w.WriteSE(int32(initialQP - 26))
	w.WriteUE(1)
	for w.BitsWritten()%8 != 0 {
		w.WriteBits(1, 1)
	}
	e, err := cabac.NewEncoder(w)
	if err != nil {
		t.Fatal(err)
	}
	// Oracle向量明确按规范Clip3(0,51,SliceQPY)初始化，不加位深offset。
	ctx, err := cabac.InitContexts(true, 0, max(0, min(51, initialQP)))
	if err != nil {
		t.Fatal(err)
	}
	for mb, delta := range []int{-1, 1, -32, 31} {
		mbCtx, qpCtx := 4, 61
		if mb == 0 {
			mbCtx, qpCtx = 3, 60
		}
		e.Bin(&ctx[mbCtx], 1)
		e.Terminate(0)    // 非PCM的I16x16。
		e.Bin(&ctx[6], 0) // 无亮度AC。
		e.Bin(&ctx[7], 0) // 无色度残差。
		e.Bin(&ctx[9], 1)
		e.Bin(&ctx[10], 0) // I16x16 DC模式。
		e.Bin(&ctx[64], 0) // 色度DC模式。
		e.Bin(&ctx[qpCtx], 1)
		v := -2 * delta
		if delta > 0 {
			v = 2*delta - 1
		}
		for n := 1; n < v; n++ {
			index := 63
			if n == 1 {
				index = 62
			}
			e.Bin(&ctx[index], 1)
		}
		index := 63
		if v == 1 {
			index = 62
		}
		e.Bin(&ctx[index], 0)
		e.Bin(&ctx[88], 1) // 两侧DC邻块推导均为coded。
		var dc [16]int32
		dc[0] = 3
		if _, err := e.Residual(&ctx, cabac.LumaDC, dc[:], transform.LumaDCScan[:]); err != nil {
			t.Fatal(err)
		}
		last := uint8(0)
		if mb == 3 {
			last = 1
		}
		e.Terminate(last)
	}
	if err := e.Err(); err != nil {
		t.Fatal(err)
	}
	unit := func(typ nal.UnitType, rbsp []byte) nal.Unit {
		return nal.Unit{Header: nal.Header{Type: typ, RefIDC: 3}, Payload: bitstream.RBSPToEBSP(rbsp)}
	}
	return annex(unit(nal.TypeSPS, srb), unit(nal.TypePPS, prb), unit(nal.TypeSliceIDR, w.Bytes()))
}

func TestHigh10_CABACNegativeQPOracle(t *testing.T) {
	ffmpeg := ffmpegPath(t)
	for _, qp := range []int{-12, 51} {
		t.Run(fmt.Sprintf("slice_qp_%d", qp), func(t *testing.T) {
			data := high10CABACQPStream(t, qp)
			path := filepath.Join(t.TempDir(), "cabac_qp.264")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			d := mustDecoder(t, h264.DecoderConfig{})
			if err := d.SendPacket(h264.Packet{Data: data}); err != nil {
				t.Fatal(err)
			}
			if err := d.Flush(); err != nil {
				t.Fatal(err)
			}
			compareHigh10(t, collectNativeFrames(t, d, nil), high10FFmpegRaw(t, ffmpeg, path))
		})
	}
}
