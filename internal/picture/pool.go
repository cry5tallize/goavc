package picture

import (
	"fmt"
	"sync"

	h264 "github.com/cry5tallize/goavc"
)

// Pool 是按编码尺寸和像素格式分桶的图像缓冲池。
//
// 设计目标：
//   - 解码一帧只分配一次工作缓冲，后续帧复用；
//   - 池本身并发安全，允许未来 slice/帧并行直接从池取图；
//   - 空闲缓存同时受每桶图像数和全池 backing 字节预算限制；
//   - 归还时不清理像素，调用方（解码器）保证整帧重建覆盖所有有效像素。
//
// 不清理是有意为之：1080p 每帧清零约 3 MiB，会成为纯开销；而解码器在
// 出错时会丢弃整帧，不会把未写区域当作有效图像输出。
const defaultMaxFreeBytes uint64 = 64 << 20

type Pool struct {
	mu   sync.Mutex
	free map[key][]*Picture

	gets         uint64
	puts         uint64
	allocs       uint64
	maxFree      int
	maxFreeBytes uint64
	freeBytes    uint64
}

type key struct {
	w, h   int
	format h264.PixelFormat
}

func pictureStorageBytes(pic *Picture) uint64 {
	if pic == nil {
		return 0
	}
	var total uint64
	for i := range pic.Planes {
		plane := &pic.Planes[i]
		total += uint64(cap(plane.Data))
		total += uint64(cap(plane.Data16)) * 2
	}
	return total
}

// NewPool 创建一个空池。maxFreePerSize 是每种尺寸保留的空闲图像上限；
// <=0 表示使用默认值 4（足够覆盖 IDR 前后的参考帧切换）。空闲 backing 的
// 全池预算固定为 64 MiB；超过预算的合法图像仍可使用，但归还时不缓存。
func NewPool(maxFreePerSize int) *Pool {
	if maxFreePerSize <= 0 {
		maxFreePerSize = 4
	}
	return &Pool{
		free:         make(map[key][]*Picture),
		maxFree:      maxFreePerSize,
		maxFreeBytes: defaultMaxFreeBytes,
	}
}

// DefaultPool 是包级共享池，供普通解码器使用。
//
// 用包级池而不是每个 Decoder 一个池，可以让先后创建的解码器复用同一批
// 缓冲；代价是尺寸不同的流各占一个桶，桶上限和全池字节预算共同生效。
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
		last := len(list) - 1
		pic := list[last]
		bytes := pictureStorageBytes(pic)
		if bytes > p.freeBytes {
			p.freeBytes = 0
		} else {
			p.freeBytes -= bytes
		}
		list[last] = nil
		list = list[:last]
		if len(list) == 0 {
			delete(p.free, k)
		} else {
			p.free[k] = list
		}
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
	bytes := pictureStorageBytes(pic)
	k := key{pic.MBWidth, pic.MBHeight, pic.Format}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.puts++
	list := p.free[k]
	if len(list) >= p.maxFree || bytes > p.maxFreeBytes || p.freeBytes > p.maxFreeBytes-bytes {
		return
	}
	p.free[k] = append(list, pic)
	p.freeBytes += bytes
}

// CacheStats 返回空闲缓存的实际 backing 字节数、非空桶数和图像数。
func (p *Pool) CacheStats() (freeBytes uint64, buckets, pictures int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, list := range p.free {
		if len(list) > 0 {
			buckets++
			pictures += len(list)
		}
	}
	return p.freeBytes, buckets, pictures
}

// Stats 返回池的累计计数快照，用于测试与诊断。
//
// gets = puts + 当前借出数；allocs 是真实分配次数。
func (p *Pool) Stats() (gets, puts, allocs uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gets, p.puts, p.allocs
}

// Trim 释放所有空闲缓冲并重置空闲字节账本。池仍然可用。
func (p *Pool) Trim() {
	p.mu.Lock()
	p.free = make(map[key][]*Picture)
	p.freeBytes = 0
	p.mu.Unlock()
}

// String 返回便于日志阅读的池状态。
func (p *Pool) String() string {
	gets, puts, allocs := p.Stats()
	return fmt.Sprintf("picture.Pool{gets:%d puts:%d allocs:%d}", gets, puts, allocs)
}
