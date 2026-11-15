package buffer

import "sync"

// Slot 是一组可一起租借的原始 byte backing。
//
// 本包只管理原始存储，不认识 Frame、PixelFormat 或 codec 生命周期。
type Slot struct {
	Planes [3][]byte
}

// Pool 是按调用方提供的稳定 key 分桶的有界原始存储池。
//
// maxBytes 限制空闲 backing 的总容量，maxPerKey 限制单桶槽数，
// maxBuckets 限制同时保留的非空桶数；超过任一限制的归还对象直接丢弃。
type Pool struct {
	mu sync.Mutex

	free map[uint64][]Slot

	maxBytes   uint64
	maxPerKey  int
	maxBuckets int
	freeBytes  uint64
}

// NewPool 创建有界原始存储池。
func NewPool(maxBytes uint64, maxPerKey, maxBuckets int) *Pool {
	if maxPerKey <= 0 {
		maxPerKey = 4
	}
	if maxBuckets <= 0 {
		maxBuckets = 8
	}
	return &Pool{
		free:       make(map[uint64][]Slot),
		maxBytes:   maxBytes,
		maxPerKey:  maxPerKey,
		maxBuckets: maxBuckets,
	}
}

// Get 取出与 key 和 sizes 匹配的空闲 backing。
//
// 找不到足够容量的槽时返回 false；调用方负责分配。返回的槽已按 sizes
// 截短，cap 仍保留原 backing 容量。
func (p *Pool) Get(key uint64, sizes [3]int) (Slot, bool) {
	if p == nil {
		return Slot{}, false
	}
	for _, size := range sizes {
		if size <= 0 {
			return Slot{}, false
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	list := p.free[key]
	if len(list) == 0 {
		return Slot{}, false
	}
	last := len(list) - 1
	slot := list[last]
	for i, size := range sizes {
		if cap(slot.Planes[i]) < size {
			return Slot{}, false
		}
	}
	list[last] = Slot{}
	list = list[:last]
	if len(list) == 0 {
		delete(p.free, key)
	} else {
		p.free[key] = list
	}
	bytes := slotBytes(slot)
	if bytes > p.freeBytes {
		p.freeBytes = 0
	} else {
		p.freeBytes -= bytes
	}
	for i, size := range sizes {
		slot.Planes[i] = slot.Planes[i][:size]
	}
	return slot, true
}

// Put 归还一组 backing；超过预算时不缓存但仍安全返回。
func (p *Pool) Put(key uint64, slot Slot) {
	if p == nil {
		return
	}
	bytes := slotBytes(slot)
	p.mu.Lock()
	defer p.mu.Unlock()
	list := p.free[key]
	if len(list) >= p.maxPerKey || bytes > p.maxBytes || p.freeBytes > p.maxBytes-bytes {
		return
	}
	if len(list) == 0 && len(p.free) >= p.maxBuckets {
		return
	}
	p.free[key] = append(list, slot)
	p.freeBytes += bytes
}

// Stats 返回空闲 backing 字节数、非空桶数和槽数。
func (p *Pool) Stats() (freeBytes uint64, buckets, slots int) {
	if p == nil {
		return 0, 0, 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, list := range p.free {
		if len(list) > 0 {
			buckets++
			slots += len(list)
		}
	}
	return p.freeBytes, buckets, slots
}

// Trim 释放所有空闲 backing。
func (p *Pool) Trim() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.free = make(map[uint64][]Slot)
	p.freeBytes = 0
	p.mu.Unlock()
}

func slotBytes(slot Slot) uint64 {
	var total uint64
	for _, plane := range slot.Planes {
		total += uint64(cap(plane))
	}
	return total
}
