package encoder

import (
	"bytes"

	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/internal/entropy/cabac"
	"github.com/cry5tallize/goavc/internal/motion"
	"github.com/cry5tallize/goavc/internal/picture"
	"github.com/cry5tallize/goavc/internal/transform"
	"github.com/cry5tallize/goavc/nal"
)

// WithInterlaced 编码逐行交织的完整帧；true为顶场优先，false为底场优先。
// 使用MBAFF帧、真实的场宏块对，不进行自适应帧/场模式选择。编码地址仍先顶后底；
// 显示时间顺序由pic_timing SEI声明，与宏块地址顺序无关。
func WithInterlaced(topFieldFirst bool) Option {
	return func(e *Encoder) error {
		e.interlaced, e.topFieldFirst = true, topFieldFirst
		return nil
	}
}

func (s *frameState) scan4() []int {
	if s.field {
		return transform.FieldScan4x4[:]
	}
	return transform.Zigzag4x4[:]
}

func (s *frameState) scan8() []int {
	if s.field {
		return transform.FieldScan8x8[:]
	}
	return transform.Zigzag8x8[:]
}

func (s *frameState) scanDC() []int {
	if s.field {
		return transform.FieldLumaDCScan[:]
	}
	return transform.LumaDCScan[:]
}

// 两个Grid分别只含同奇偶场；该坐标空间已经完成MBAFF邻块定位。
func (s *frameState) fieldNeighbors(x, y, width int) motion.FieldNeighbors {
	get := func(x, y int) motion.FieldNeighbor {
		mv, ref, available := s.mv.Get(x, y)
		return motion.FieldNeighbor{MV: mv, Ref: ref, Available: available, Structure: motion.TopFieldMotion}
	}
	return motion.FieldNeighbors{A: get(x-1, y), B: get(x, y-1), C: get(x+width, y-1), D: get(x-1, y-1)}
}

func (s *frameState) predictFieldPartition(addr, kind, index int) motion.MV {
	p := partitions[kind][index]
	x, y := addr%s.pic.MBWidth*4+p.x/4, addr/s.pic.MBWidth*4+p.y/4
	part := motion.FieldMedian
	if kind == 1 {
		part = motion.Field16x8Top
		if index == 1 {
			part = motion.Field16x8Bottom
		}
	} else if kind == 2 {
		part = motion.Field8x16Left
		if index == 1 {
			part = motion.Field8x16Right
		}
	}
	return motion.PredictField(motion.TopFieldMotion, 0, part, s.fieldNeighbors(x, y, p.w/4))
}

// interlacedStates借用同一个帧缓冲，两套元数据分别采用场光栅坐标。
// 场内预测/变换/运动搜索及重建仍调用逐行通道的共享内核。
func (e *Encoder) interlacedStates(src, recon *picture.Picture) ([2]*frameState, [2]*picture.Picture, [2]*picture.Picture, error) {
	var states [2]*frameState
	var past, future [2]*picture.Picture
	e.ensureFrameStorage()
	channel := *e
	if e.autoWeights && e.reference != nil && !e.encodingB {
		weights := e.predictionWeights(src, e.reference, 3-2*flag(e.separateColourPlane))
		channel.autoWeights, channel.fixedWeights = false, &weights
	}
	for parity := range 2 {
		channel.analysisIndex = e.analysisIndex + parity + 1
		input, err := picture.FieldView(src, parity == 1)
		if err != nil {
			return states, past, future, err
		}
		output, err := picture.FieldView(recon, parity == 1)
		if err != nil {
			return states, past, future, err
		}
		channel.planeReconstruction = &output
		if e.qpAt != nil {
			channel.qpAt = func(x, y int) int { return e.qpAt(x, y*2+parity) }
		}
		if e.reference != nil {
			ref, err := picture.FieldView(e.reference, parity == 1)
			if err != nil {
				return states, past, future, err
			}
			channel.reference = &ref
		}
		for index, ref := range [2]*picture.Picture{e.bPast, e.bFuture} {
			if ref == nil {
				continue
			}
			view, err := picture.FieldView(ref, parity == 1)
			if err != nil {
				return states, past, future, err
			}
			if index == 0 {
				past[parity] = &view
			} else {
				future[parity] = &view
			}
		}
		s, err := channel.newFrameState(&input)
		if err != nil {
			return states, past, future, err
		}
		s.field = true
		s.reference = channel.reference
		if e.encodingB {
			s.reference, s.weights, s.searchReference = past[parity], nil, nil
		}
		states[parity] = s
	}
	return states, past, future, nil
}

// encodeInterlaced按pair光栅、pair内顶/底顺序提交语法。所有宏块均有真实场布局；
// 整帧的QP、CABAC状态和CAVLC skip_run在两个场之间连续，不会变成两个独立slice。
func (e *Encoder) encodeInterlaced(src *picture.Picture, intra, idr bool) ([]byte, *frameState, error) {
	recon := e.planeReconstruction
	ownsRecon := false
	if recon == nil {
		var err error
		if e.picturePool != nil {
			recon, err = e.picturePool.Get(src.MBWidth, src.MBHeight, src.Format)
		} else {
			recon, err = picture.New(src.MBWidth, src.MBHeight, src.Format)
		}
		if err != nil {
			return nil, nil, err
		}
		ownsRecon = true
	}
	var states [2]*frameState
	success := false
	defer func() {
		if !success {
			for _, s := range states {
				releaseFrameMetadata(s)
			}
			if ownsRecon {
				recon.Release()
			}
		}
	}()
	states, past, future, err := e.interlacedStates(src, recon)
	if err != nil {
		return nil, nil, err
	}
	var out bytes.Buffer
	aw := nal.NewAnnexBWriter(&out)
	emit := func(typ nal.UnitType, ref uint8, payload []byte) error {
		return aw.Write(nal.Unit{Header: nal.Header{Type: typ, RefIDC: ref}, Payload: bitstream.RBSPToEBSP(payload)})
	}
	if !e.separateColourPlane || e.colourPlaneID == 0 {
		if idr {
			sps, err := e.sps.MarshalRBSP()
			if err != nil {
				return nil, nil, err
			}
			pps, err := e.pps.MarshalRBSP(e.sps)
			if err != nil {
				return nil, nil, err
			}
			if err = emit(nal.TypeSPS, 3, sps); err != nil {
				return nil, nil, err
			}
			if err = emit(nal.TypePPS, 3, pps); err != nil {
				return nil, nil, err
			}
		}
		// D.1.2/D.2.2：pic_struct=3/4均含两个clock_timestamp_flag=0。
		// payload末尾的1为payload_bit_equal_to_one，不是第三个时间戳。
		picStruct := byte(4)
		if e.topFieldFirst {
			picStruct = 3
		}
		if err = emit(nal.TypeSEI, 0, []byte{1, 1, picStruct<<4 | 2, 0x80}); err != nil {
			return nil, nil, err
		}
	}
	if intra {
		for _, s := range states {
			s.reference, s.searchReference, s.weights = nil, nil, nil
		}
	}
	pairs := len(states[0].info)
	for slice := 0; slice < e.slices; slice++ {
		first, last := pairs*slice/e.slices, pairs*(slice+1)/e.slices
		for _, s := range states {
			s.mv.BeginSlice(first)
		}
		w := bitstream.NewWriter()
		switch {
		case e.encodingB:
			e.bSliceHeader(w, first)
		case intra:
			e.sliceHeader(w, first, e.pcm, idr)
		default:
			e.pSliceHeader(w, first, states[0].weights)
		}
		var c *cabacWriter
		if e.cabac {
			for w.BitsWritten()%8 != 0 {
				w.WriteBit(1)
			}
			ce, err := cabac.NewEncoder(w)
			if err != nil {
				return nil, nil, err
			}
			c, err = newCABACWriter(ce, intra, e.frameQP, states[0].chromaArrayType() == 3)
			if err != nil {
				return nil, nil, err
			}
			c.field = true
		}
		qp, skipRun := e.frameQP, uint32(0)
		for pair := first; pair < last; pair++ {
			topSkipped := false
			for parity, s := range states {
				i := &s.info[pair]
				i.start = first
				// 双Skip仅能沿同slice已编码的左/上pair继承field flag。
				// slice可从上一pair-row末尾开始；换行后的pair虽非slice首项，
				// 仍可能左右/上方都不可用，此时必须写出显式field flag。
				mw := s.pic.MBWidth
				left := pair%mw != 0 && pair-1 >= first
				above := pair-mw >= first
				s.forceNoSkip = parity == 0 && !left && !above
				var res residual
				switch {
				case e.pcm:
					i.pcm = true
				case e.encodingB:
					res, err = s.analyzeB(pair, qp, past[parity], future[parity])
					if s.forceNoSkip {
						i.skip = false
					}
				case intra:
					res, err = s.analyzeIntraRD(pair, qp)
				default:
					res, err = s.analyzeRD(pair, qp, e.searchRange, e.fullSearch)
				}
				if err != nil {
					return nil, nil, err
				}
				fieldFlag := parity == 0 || topSkipped
				if parity == 0 {
					topSkipped = i.skip
				}
				if c == nil {
					if !intra {
						if i.skip {
							i.qp = qp
							skipRun++
							continue
						}
						w.WriteUE(skipRun)
						skipRun = 0
					}
					if fieldFlag {
						w.WriteBit(1)
					}
					switch {
					case i.pcm:
						s.writePCM(w, pair)
					case e.encodingB:
						w.WriteUE(0)
						err = s.writeInterResidual(w, pair, &qp, &res)
					case i.inter:
						err = s.writeInter(w, pair, &qp, &res)
					default:
						err = s.writeMB(w, pair, &qp, &res)
					}
					if err != nil {
						return nil, nil, err
					}
					continue
				}
				clear(s.nz[pair*16 : pair*16+16])
				for comp := range s.cnz {
					clear(s.cnz[comp][pair*16 : pair*16+16])
				}
				if !intra {
					if e.encodingB {
						inc := 0
						for _, n := range s.cabacNeighbors(pair) {
							if n >= 0 && !s.info[n].skip {
								inc++
							}
						}
						c.bin(24+inc, flag(i.skip))
					} else {
						c.skip(s, pair)
					}
				}
				if i.skip {
					i.qp, c.lastDelta = qp, false
				} else {
					if fieldFlag {
						inc := 0
						for _, n := range s.cabacNeighbors(pair) {
							if n >= 0 {
								inc++ // 全部可用邻pair都为field-coded。
							}
						}
						c.bin(70+inc, 1)
					}
					if e.encodingB {
						c.bin(27, 0)
						c.residualMacroblock(s, pair, &qp, &res)
					} else {
						c.mbType(s, pair, intra)
						if i.pcm {
							if err = c.e.Err(); err != nil {
								return nil, nil, err
							}
							s.writePCMSamples(w, pair)
							c.completedBins += c.e.BinCount()
							c.e, err = cabac.NewEncoder(w)
							if err != nil {
								return nil, nil, err
							}
							c.lastDelta = false
						} else {
							c.macroblock(s, pair, &qp, &res)
						}
					}
				}
				if parity == 1 {
					c.e.Terminate(uint8(flag(pair == last-1)))
				}
				if err = c.e.Err(); err != nil {
					return nil, nil, err
				}
			}
		}
		typ, ref := nal.TypeSlice, uint8(3)
		if idr {
			typ = nal.TypeSliceIDR
		} else if e.encodingB {
			ref = 0
		}
		if c != nil {
			payload := cabacZeroWordPayloadFormat(w.Bytes(), c.completedBins+c.e.BinCount(), (last-first)*2, states[0].rawMBBits())
			err = aw.Write(nal.Unit{Header: nal.Header{Type: typ, RefIDC: ref}, Payload: payload})
		} else {
			if skipRun != 0 {
				w.WriteUE(skipRun)
			}
			bitstream.WriteRBSPTrailingBits(w)
			err = emit(typ, ref, w.Bytes())
		}
		if err != nil {
			return nil, nil, err
		}
	}
	for _, s := range states {
		s.mv.BeginSlice(0)
		if !e.pcm {
			s.deblock()
		}
		s.src, s.reference, s.searchReference = nil, nil, nil
	}
	// 最近帧的像素快照必须仍为完整交织帧，不能暴露半高场视图。
	success = true
	return out.Bytes(), &frameState{pic: recon, ownsPic: ownsRecon, fields: states}, nil
}
