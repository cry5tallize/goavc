package picture

import (
	"fmt"
	"sync"

	h264 "github.com/goavc/h264"
)

// Pool 是按编码尺寸和像素格式分桶的图像缓冲池。
//
// 设计目标：
//   - 解码一帧只分配一次工作缓冲，后续帧复用；
//   - 池本身并发安全，允许未来 slice/帧并行直接从池取图；
//   - 归还时不清理像素，调用方（解码器）保证整帧重建覆盖所有有效像素。
//
// 不清理是有意为之：1080p 每帧清零约 3 MiB，会成为纯开销；而解码器在
// 出错时会丢弃整帧，不会把未写区域当作有效图像输出。
type Pool struct {
	mu   sync.Mutex
	free map[key][]*Picture

	gets    uint64
	puts    uint64
	allocs  uint64
	maxFree int
}

type key struct {
	w, h   int
	format h264.PixelFormat
}

// NewPool 创建一个空池。maxFreePerSize 是每种尺寸保留的空闲图像上限；
// <=0 表示使用默认值 4（足够覆盖 IDR 前后的参考帧切换）。
func NewPool(maxFreePerSize int) *Pool {
	if maxFreePerSize <= 0 {
		maxFreePerSize = 4
	}
	return &Pool{
		free:    make(map[key][]*Picture),
		maxFree: maxFreePerSize,
	}
}

// DefaultPool 是包级共享池，供普通解码器使用。
//
// 用包级池而不是每个 Decoder 一个池，可以让先后创建的解码器复用同一批
// 缓冲；代价是尺寸不同的流各占一个桶，桶上限由 maxFree 控制。
var DefaultPool = NewPool(4)

// Get 按 format 取出一张 mbWidth×mbHeight 的图像。
//
// 命中的缓冲不做清零；未命中时新分配。返回的 Picture 已绑定本池，
// 用完必须调用 Release。
func (p *Pool) Get(mbWidth, mbHeight int, format h264.PixelFormat) (*Picture, error) {
	if err := validateLayout(mbWidth, mbHeight, format); err != nil {
		return nil, err
	}
	if p == nil {
		return New(mbWidth, mbHeight, format)
	}
	k := key{mbWidth, mbHeight, format}
	p.mu.Lock()
	if list := p.free[k]; len(list) > 0 {
		pic := list[len(list)-1]
		p.free[k] = list[:len(list)-1]
		// Release 会把 pool 置空以标记“已归还”，复用时必须重新绑定，
		// 否则第二次 Release 会被误判为非池化图像而泄漏。
		pic.pool = p
		p.gets++
		p.mu.Unlock()
		return pic, nil
	}
	p.allocs++
	p.gets++
	p.mu.Unlock()

	pic, err := New(mbWidth, mbHeight, format)
	if err != nil {
		return nil, err
	}
	pic.pool = p
	return pic, nil
}

func (p *Pool) put(pic *Picture) {
	if pic == nil {
		return
	}
	k := key{pic.MBWidth, pic.MBHeight, pic.Format}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.puts++
	list := p.free[k]
	if len(list) >= p.maxFree {
		return
	}
	p.free[k] = append(list, pic)
}

// Stats 返回池的累计计数快照，用于测试与诊断。
//
// gets = puts + 当前借出数；allocs 是真实分配次数。
func (p *Pool) Stats() (gets, puts, allocs uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gets, p.puts, p.allocs
}

// Trim 释放所有空闲缓冲。池仍然可用。
func (p *Pool) Trim() {
	p.mu.Lock()
	p.free = make(map[key][]*Picture)
	p.mu.Unlock()
}

// String 返回便于日志阅读的池状态。
func (p *Pool) String() string {
	gets, puts, allocs := p.Stats()
	return fmt.Sprintf("picture.Pool{gets:%d puts:%d allocs:%d}", gets, puts, allocs)
}
