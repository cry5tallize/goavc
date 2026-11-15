package encoder

import (
	"fmt"
	"unsafe"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/motion"
	"github.com/goavc/h264/internal/motion/search"
	"github.com/goavc/h264/internal/picture"
)

const frameStorageBudget = 16 << 20

// frameStoragePool 由一个实例拥有；场/平面通道只借用此指针。
// 只缓存已归还的独占存储，不限制合法活跃工作所需内存。
type frameStoragePool struct {
	idle        [3]*frameStorage
	count       int
	idleBytes   int
	leasedBytes int
}

type frameStorage struct {
	owner   *frameStoragePool
	info    []mbInfo
	nz      []uint8
	cnz     [2][]uint8
	dcCoded [3][]bool
	mvd     []motion.MV
	mv      *motion.Grid
	bytes   int
}

func (e *Encoder) ensureFrameStorage() {
	if e.frameStorage == nil {
		e.frameStorage = new(frameStoragePool)
	}
}

func (p *frameStoragePool) get(width, height int) *frameStorage {
	var s *frameStorage
	for i := range p.count {
		candidate := p.idle[i]
		if candidate.mv.MBWidth == width && candidate.mv.MBHeight == height {
			s = candidate
			p.count--
			p.idle[i] = p.idle[p.count]
			p.idle[p.count] = nil
			p.idleBytes -= s.bytes
			break
		}
	}
	if s == nil {
		total := width * height
		s = &frameStorage{owner: p, info: make([]mbInfo, total), nz: make([]uint8, total*16), mvd: make([]motion.MV, total*16), mv: motion.NewGrid(width, height)}
		for c := range s.cnz {
			s.cnz[c] = make([]uint8, total*16)
		}
		for c := range s.dcCoded {
			s.dcCoded[c] = make([]bool, total)
		}
		// Sizeof仅用于编译期类型大小记账，不转换指针、不访问原始内存。
		// NewGrid的私有valid与MV等长且之后不扩容，因此可按元素数计费。
		s.bytes = cap(s.info)*int(unsafe.Sizeof(mbInfo{})) + cap(s.nz) + cap(s.mvd)*int(unsafe.Sizeof(motion.MV{})) + cap(s.mv.MV)*int(unsafe.Sizeof(motion.MV{})) + cap(s.mv.Ref) + len(s.mv.MV)*int(unsafe.Sizeof(bool(false)))
		for _, v := range s.cnz {
			s.bytes += cap(v)
		}
		for _, v := range s.dcCoded {
			s.bytes += cap(v) * int(unsafe.Sizeof(bool(false)))
		}
	} else {
		clear(s.info)
		clear(s.nz)
		clear(s.mvd)
		for _, v := range s.cnz {
			clear(v)
		}
		for _, v := range s.dcCoded {
			clear(v)
		}
		s.mv.Reset()
	}
	p.leasedBytes += s.bytes
	return s
}

func (p *frameStoragePool) put(s *frameStorage) {
	p.leasedBytes -= s.bytes
	if p.count < len(p.idle) && s.bytes+p.idleBytes <= frameStorageBudget {
		p.idle[p.count] = s
		p.count++
		p.idleBytes += s.bytes
	}
}

func (e *Encoder) newFrameState(src *picture.Picture) (*frameState, error) {
	if e.searchWorkspace == nil {
		e.searchWorkspace = new(search.Workspace)
	}
	if e.trialWriters == nil {
		e.trialWriters = newTrialWriters()
	}
	pic := e.planeReconstruction
	ownsPic := false
	if pic == nil {
		var err error
		if e.picturePool != nil {
			pic, err = e.picturePool.Get(src.MBWidth, src.MBHeight, src.Format)
		} else {
			pic, err = picture.New(src.MBWidth, src.MBHeight, src.Format)
		}
		if err != nil {
			return nil, err
		}
		ownsPic = true
	}
	e.ensureFrameStorage()
	storage := e.frameStorage.get(src.MBWidth, src.MBHeight)
	s := &frameState{src: src, pic: pic, ownsPic: ownsPic, storage: storage, info: storage.info, nz: storage.nz, cnz: storage.cnz, dcCoded: storage.dcCoded, mvd: storage.mvd, mv: storage.mv}
	s.searchWorkspace = e.searchWorkspace
	s.trialWriters = e.trialWriters
	s.allowZeroPCM = e.cfg.Profile != h264.ProfileBaseline && e.cfg.Profile != h264.ProfileMain
	s.separate, s.plane = e.separateColourPlane, e.colourPlaneID
	s.independent = e.separateColourPlane
	s.allow8 = e.transform8
	s.scaling8 = e.scaling8
	s.scaling4 = e.scaling4
	if !e.encodingB && e.reference != nil && (e.autoWeights || e.fixedWeights != nil) {
		weights := e.fixedWeights
		if e.autoWeights {
			w := e.predictionWeights(src, e.reference, s.planeCount())
			weights = &w
		}
		s.weights = weights
		// 该副本只用于整数/亚像素搜索启发式。真正 MC 必须先在原参考上插值，
		// 再逐采样加权；裁剪与插值不可交换（Spec: 8.4.3.2）。
		s.searchReference = e.searchReference(e.reference, weights, s.planeCount())
	}
	s.maxLevelPrefix = 15
	if e.cabac || s.allowZeroPCM {
		s.maxLevelPrefix = 28
	}
	// 所有回调值在写入之前校验；失败不得更新参考帧、帧号和输出槽。
	activities := e.activities(src)
	for addr := range s.info {
		qp := e.frameQP
		if e.qpAt != nil {
			qp = e.qpAt(addr%src.MBWidth, addr/src.MBWidth)
		}
		if qp < -6*(src.Planes[0].BitDepth-8) || qp > 51 {
			releaseFrameState(s)
			return nil, fmt.Errorf("encoder: macroblock qp %d: %w", qp, h264.ErrInvalidData)
		}
		if activities != nil {
			qp = activityAdjustedQP(qp, e.adaptiveQP, activities[addr], src.Planes[0].BitDepth)
		} else {
			qp = activityQP(&src.Planes[0], addr%src.MBWidth*16, addr/src.MBWidth*16, qp, e.adaptiveQP)
		}
		s.info[addr].qp = qp
	}
	return s, nil
}
