// Package chromaplane 为不支持独立色平面的外部Oracle提供规范单色投影。
// 只改参数集与slice头，所有宏块/残差比特保持原样；不参与运行时编解码。
package chromaplane

import (
	"fmt"
	"math/bits"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/transform"
	"github.com/goavc/h264/nal"
	"github.com/goavc/h264/syntax"
)

func ueBits(v uint32) int { return 2*bits.Len64(uint64(v)+1) - 1 }

func copyBits(w *bitstream.Writer, r *bitstream.Reader, n int) error {
	for n > 0 {
		count := min(n, 64)
		v, err := r.ReadBits(uint(count))
		if err != nil {
			return err
		}
		if err := w.WriteBits(uint(count), v); err != nil {
			return err
		}
		n -= count
	}
	return nil
}

func appendUnit(dst []byte, typ nal.UnitType, ref uint8, rbsp []byte) []byte {
	dst = append(dst, 0, 0, 0, 1, ref<<5|byte(typ))
	return append(dst, bitstream.RBSPToEBSP(rbsp)...)
}

// monochromeSPS 依§7.4.2.1.1移除separate_colour_plane_flag，改写chroma_format_idc。
// 原缩放矩阵已在PPS中按colour_plane_id显式选择；crop单位仍为1×1。
func monochromeSPS(s *syntax.SPS) ([]byte, error) {
	plain := *s
	plain.SeqScalingMatrixPresentFlag = false
	source := bitstream.NewWriter()
	if err := plain.WriteTo(source); err != nil {
		return nil, err
	}
	r := bitstream.NewReader(source.Bytes())
	w := bitstream.NewWriter()
	prefix := 24 + ueBits(s.SeqParameterSetID)
	if err := copyBits(w, r, prefix); err != nil {
		return nil, err
	}
	if _, err := r.ReadUE(); err != nil {
		return nil, err
	}
	if _, err := r.ReadBits(1); err != nil {
		return nil, err
	}
	w.WriteUE(0)
	if err := copyBits(w, r, int(source.BitsWritten())-prefix-ueBits(3)-1); err != nil {
		return nil, err
	}
	if err := bitstream.WriteRBSPTrailingBits(w); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

func scaling(values []uint8, scan []int, size int) *syntax.ScalingList {
	list := &syntax.ScalingList{Size: size, Deltas: make([]int32, len(scan))}
	last := int32(8)
	for i, pos := range scan {
		v := int32(values[pos])
		delta := (v-last+128)&255 - 128
		list.Deltas[i] = delta
		last = v
	}
	return list
}

// Project 将一个独立平面码流投影成三个逐平面单色码流，用于外部像素Oracle。
// PPS id的Exp-Golomb码长比原来多2(mod 8)位，恰好抵消colour_plane_id；
// 因而包括I_PCM与CABAC在内的全部原始slice数据字节对齐保持不变。
// 该结果不证明外部工具支持原始独立平面封装，原始封装另由规范向量验证。
func Project(data []byte) ([3][]byte, error) {
	var out [3][]byte
	units, err := nal.SplitAnnexB(data)
	if err != nil {
		return out, err
	}
	spss := map[uint32]*syntax.SPS{}
	ppss := map[uint32]*syntax.PPS{}
	ids := map[uint32]uint32{}
	used := map[uint32]bool{}
	for _, u := range units {
		payload := u.Payload
		// Spec: 7.4.1，CABAC尾部00 00 03是合法的零字封装；不将末03当作截断。
		if u.Header.IsVCL() && len(payload) >= 3 && payload[len(payload)-3] == 0 && payload[len(payload)-2] == 0 && payload[len(payload)-1] == 3 {
			payload = payload[:len(payload)-1]
		}
		rb, err := bitstream.EBSPToRBSP(payload)
		if err != nil {
			return out, err
		}
		switch u.Header.Type {
		case nal.TypeSPS:
			s, err := syntax.ParseSPS(rb)
			if err != nil {
				return out, err
			}
			if !s.SeparateColourPlaneFlag || s.ChromaFormatIDC != 3 {
				return out, fmt.Errorf("chromaplane: requires independent 444 planes: %w", h264.ErrInvalidData)
			}
			spss[s.SeqParameterSetID] = s
			mono, err := monochromeSPS(s)
			if err != nil {
				return out, err
			}
			for c := range 3 {
				out[c] = appendUnit(out[c], nal.TypeSPS, u.Header.RefIDC, mono)
			}
		case nal.TypePPS:
			r := bitstream.NewReader(rb)
			id, err := r.ReadUE()
			if err != nil {
				return out, err
			}
			sid, err := r.ReadUE()
			if err != nil {
				return out, err
			}
			s := spss[sid]
			if s == nil {
				return out, fmt.Errorf("chromaplane: missing SPS: %w", h264.ErrInvalidData)
			}
			p, err := syntax.ParsePPS(rb, s)
			if err != nil {
				return out, err
			}
			ppss[id] = p
			target, ok := ids[id]
			if !ok {
				length := ueBits(id) + 2
				if length > 15 {
					length -= 8
				}
				first, last := uint32(1)<<(length/2)-1, min(uint32(255), uint32(1)<<(length/2+1)-2)
				found := false
				for target = first; target <= last; target++ {
					if !used[target] {
						found = true
						break
					}
				}
				if !found {
					return out, fmt.Errorf("chromaplane: no alignment-preserving PPS id: %w", h264.ErrResourceLimit)
				}
				ids[id], used[target] = target, true
			}
			matrices, err := syntax.SelectScalingMatrices(s, p)
			if err != nil {
				return out, err
			}
			for c := range 3 {
				monoS := *s
				monoS.ChromaFormatIDC = 0
				monoS.SeparateColourPlaneFlag = false
				monoS.SeqScalingMatrixPresentFlag = false
				monoP := *p
				monoP.PicParameterSetID = target
				monoP.PicScalingMatrixPresentFlag = true
				monoP.PicScalingListPresentFlag = [12]bool{}
				monoP.PicScalingList = [12]*syntax.ScalingList{}
				for _, inter := range []int{0, 1} {
					i := 3 * inter
					monoP.PicScalingListPresentFlag[i] = true
					monoP.PicScalingList[i] = scaling(matrices.Four[i+c][:], transform.Zigzag4x4[:], 4)
					if p.Transform8x8ModeFlag {
						monoP.PicScalingListPresentFlag[6+inter] = true
						monoP.PicScalingList[6+inter] = scaling(matrices.Eight[2*c+inter][:], transform.Zigzag8x8[:], 8)
					}
				}
				payload, err := monoP.MarshalRBSP(&monoS)
				if err != nil {
					return out, err
				}
				out[c] = appendUnit(out[c], nal.TypePPS, u.Header.RefIDC, payload)
			}
		case nal.TypeSlice, nal.TypeSliceIDR:
			r := bitstream.NewReader(rb)
			first, err := r.ReadUE()
			if err != nil {
				return out, err
			}
			typ, err := r.ReadUE()
			if err != nil {
				return out, err
			}
			id, err := r.ReadUE()
			if err != nil {
				return out, err
			}
			plane, err := r.ReadBits(2)
			if err != nil {
				return out, err
			}
			if plane > 2 || ppss[id] == nil {
				return out, fmt.Errorf("chromaplane: invalid slice reference: %w", h264.ErrInvalidData)
			}
			w := bitstream.NewWriter()
			w.WriteUE(first)
			w.WriteUE(typ)
			w.WriteUE(ids[id])
			if err := copyBits(w, r, int(r.BitsRemaining())); err != nil {
				return out, err
			}
			out[plane] = appendUnit(out[plane], u.Header.Type, u.Header.RefIDC, w.Bytes())
		case nal.TypeAUD, nal.TypeEndOfSequence, nal.TypeEndOfStream:
			for c := range 3 {
				out[c] = appendUnit(out[c], u.Header.Type, u.Header.RefIDC, rb)
			}
		}
	}
	return out, nil
}
