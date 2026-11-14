package encoder

import (
	"bytes"
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/entropy/cabac"
	"github.com/goavc/h264/internal/picture"
	"github.com/goavc/h264/nal"
)

// encodeCABAC 先接入规范 I_PCM 宏块通道。I_PCM 在 CABAC 中会终止当前
// 算术编码器，并在样本后重启；这条路径用于验证 NAL、slice header、终止
// 位、PCM 对齐和 zero-word 封装，残差宏块在后续提交中接入。
func (e *Encoder) encodeCABAC(src *picture.Picture, idr bool) ([]byte, *frameState, error) {
	if !idr {
		return nil, nil, fmt.Errorf("encoder: CABAC P slice is not implemented: %w", h264.ErrUnsupported)
	}
	s, err := e.newFrameState(src)
	if err != nil {
		return nil, nil, err
	}
	var out bytes.Buffer
	aw := nal.NewAnnexBWriter(&out)
	emit := func(typ nal.UnitType, rbsp []byte) error {
		return aw.Write(nal.Unit{Header: nal.Header{Type: typ, RefIDC: 3}, Payload: bitstream.RBSPToEBSP(rbsp)})
	}
	sps, err := e.sps.MarshalRBSP()
	if err != nil {
		return nil, nil, err
	}
	pps, err := e.pps.MarshalRBSP(e.sps)
	if err != nil {
		return nil, nil, err
	}
	if err = emit(nal.TypeSPS, sps); err != nil {
		return nil, nil, err
	}
	if err = emit(nal.TypePPS, pps); err != nil {
		return nil, nil, err
	}
	total := len(s.info)
	for slice := 0; slice < e.slices; slice++ {
		first, last := total*slice/e.slices, total*(slice+1)/e.slices
		if last-first != 1 {
			return nil, nil, fmt.Errorf("encoder: CABAC PCM slice requires one macroblock: %w", h264.ErrUnsupported)
		}
		w := bitstream.NewWriter()
		e.sliceHeader(w, first, false)
		for w.BitsWritten()%8 != 0 {
			if err = w.WriteBit(1); err != nil {
				return nil, nil, err
			}
		}
		for addr := first; addr < last; addr++ {
			s.info[addr].start = first
			ctx, err := cabac.InitContexts(true, 0, e.cfg.QP)
			if err != nil {
				return nil, nil, err
			}
			ce, err := cabac.NewEncoder(w)
			if err != nil {
				return nil, nil, err
			}
			// I_PCM 的 mb_type=25：I slice 的 ctxIdx 3，随后是
			// end_of_slice_flag=1（Spec: 9.3.3.1.2）。
			ce.Bin(&ctx[3], 1)
			ce.Terminate(1)
			if err = ce.Err(); err != nil {
				return nil, nil, err
			}
			if err = writeCABACPCM(w, s, addr); err != nil {
				return nil, nil, err
			}
			// PCM 后算术引擎重启，外层 slice 循环仍要消费一个
			// end_of_slice_flag；单宏块路径在此写出终止 1。
			next, err := cabac.NewEncoder(w)
			if err != nil {
				return nil, nil, err
			}
			next.Terminate(1)
			if err = next.Err(); err != nil {
				return nil, nil, err
			}
		}
		if err = emit(nal.TypeSliceIDR, w.Bytes()); err != nil {
			return nil, nil, err
		}
	}
	s.src = nil
	return out.Bytes(), s, nil
}

func writeCABACPCM(w *bitstream.Writer, s *frameState, addr int) error {
	info := &s.info[addr]
	info.pcm = true
	info.qp = 0
	info.cbp = 47
	for i := range info.modes {
		info.modes[i] = 2
		s.nz[addr*16+i] = 16
	}
	for c := range s.cnz {
		for i := 0; i < 4; i++ {
			s.cnz[c][addr*4+i] = 16
		}
	}
	for w.BitsWritten()%8 != 0 {
		if err := w.WriteBit(0); err != nil {
			return err
		}
	}
	for c := 0; c < 3; c++ {
		n := 16
		if c > 0 {
			n = 8
		}
		x, y := addr%s.pic.MBWidth*n, addr/s.pic.MBWidth*n
		for j := 0; j < n; j++ {
			row := s.src.Planes[c].Row(y + j)[x : x+n]
			copy(s.pic.Planes[c].Row(y + j)[x:x+n], row)
			for _, v := range row {
				if err := w.WriteByte(v); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
