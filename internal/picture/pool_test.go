package picture

import (
	"testing"

	h264 "github.com/goavc/h264"
)

func TestPool_ReuseAndStats(t *testing.T) {
	pool := NewPool(2)
	a, err := pool.Get(2, 2, h264.YUV420P)
	if err != nil {
		t.Fatal(err)
	}
	if a.pool != pool {
		t.Fatal("picture not bound to pool")
	}
	gets, puts, allocs := pool.Stats()
	if gets != 1 || puts != 0 || allocs != 1 {
		t.Fatalf("stats = %d/%d/%d, want 1/0/1", gets, puts, allocs)
	}
	a.Release()
	a.Release() // 幂等
	gets, puts, allocs = pool.Stats()
	if gets != 1 || puts != 1 || allocs != 1 {
		t.Fatalf("stats after release = %d/%d/%d, want 1/1/1", gets, puts, allocs)
	}

	b, err := pool.Get(2, 2, h264.YUV420P)
	if err != nil {
		t.Fatal(err)
	}
	if b != a {
		t.Fatal("pool did not reuse released picture")
	}
	gets, _, allocs = pool.Stats()
	if gets != 2 || allocs != 1 {
		t.Fatalf("reuse stats = %d/%d, want gets=2 allocs=1", gets, allocs)
	}
	b.Release()

	// 不同尺寸不能复用。
	c, err := pool.Get(3, 2, h264.YUV420P)
	if err != nil {
		t.Fatal(err)
	}
	if c == a {
		t.Fatal("pool reused picture across different dimensions")
	}
	c.Release()

	// 桶上限：超过 maxFree 的归还直接丢弃，不报错。
	pool.Trim()
	pics := make([]*Picture, 0, 4)
	for i := 0; i < 4; i++ {
		p, err := pool.Get(2, 2, h264.YUV420P)
		if err != nil {
			t.Fatal(err)
		}
		pics = append(pics, p)
	}
	for _, p := range pics {
		p.Release()
	}
	pool.mu.Lock()
	free := len(pool.free[key{2, 2, h264.YUV420P}])
	pool.mu.Unlock()
	if free != 2 {
		t.Fatalf("free list length = %d, want 2", free)
	}
}
func TestPool_GetClearsReleasedSlotAndEmptyBucket(t *testing.T) {
	pool := NewPool(1)
	pic, err := pool.Get(1, 1, h264.YUV420P)
	if err != nil {
		t.Fatal(err)
	}
	pic.Release()

	k := key{1, 1, h264.YUV420P}
	pool.mu.Lock()
	list := pool.free[k]
	if len(list) != 1 || list[0] != pic {
		pool.mu.Unlock()
		t.Fatalf("free list = %#v, want released picture", list)
	}
	pool.mu.Unlock()

	reused, err := pool.Get(1, 1, h264.YUV420P)
	if err != nil {
		t.Fatal(err)
	}
	if reused != pic {
		t.Fatal("pool did not reuse released picture")
	}
	if list[0] != nil {
		t.Fatal("popped free-list slot still retains picture")
	}
	pool.mu.Lock()
	_, exists := pool.free[k]
	pool.mu.Unlock()
	if exists {
		t.Fatal("empty free-list bucket was retained")
	}
	reused.Release()
}

func TestPool_BoundsFreeStorageAndCacheStats(t *testing.T) {
	pool := NewPool(4)
	pic, err := pool.Get(1, 1, h264.YUV420P)
	if err != nil {
		t.Fatal(err)
	}
	bytes := pictureStorageBytes(pic)
	if bytes == 0 {
		t.Fatal("picture storage size is zero")
	}
	pool.maxFreeBytes = bytes
	pic.Release()
	freeBytes, buckets, pictures := pool.CacheStats()
	if freeBytes != bytes || buckets != 1 || pictures != 1 {
		t.Fatalf("cache stats = %d/%d/%d, want %d/1/1", freeBytes, buckets, pictures, bytes)
	}

	reused, err := pool.Get(1, 1, h264.YUV420P)
	if err != nil {
		t.Fatal(err)
	}
	freeBytes, buckets, pictures = pool.CacheStats()
	if freeBytes != 0 || buckets != 0 || pictures != 0 {
		t.Fatalf("cache stats after Get = %d/%d/%d, want 0/0/0", freeBytes, buckets, pictures)
	}

	pool.maxFreeBytes = bytes - 1
	reused.Release()
	freeBytes, buckets, pictures = pool.CacheStats()
	if freeBytes != 0 || buckets != 0 || pictures != 0 {
		t.Fatalf("over-budget cache stats = %d/%d/%d, want 0/0/0", freeBytes, buckets, pictures)
	}
	fresh, err := pool.Get(1, 1, h264.YUV420P)
	if err != nil {
		t.Fatal(err)
	}
	if fresh == reused {
		t.Fatal("over-budget release was reused")
	}
	fresh.Release()
	_, _, allocs := pool.Stats()
	if allocs != 2 {
		t.Fatalf("allocs = %d, want 2 after rejected reuse", allocs)
	}
}

func TestPool_ConcurrentGetPut(t *testing.T) {
	pool := NewPool(8)
	const goroutines = 8
	const iterations = 50
	done := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			for i := 0; i < iterations; i++ {
				p, err := pool.Get(1, 1, h264.YUV420P)
				if err != nil {
					done <- err
					return
				}
				if err := p.Validate(); err != nil {
					done <- err
					return
				}
				p.Planes[0].SetPix(0, 0, 1)
				p.Release()
			}
			done <- nil
		}()
	}
	for g := 0; g < goroutines; g++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	gets, puts, _ := pool.Stats()
	if gets != puts {
		t.Fatalf("gets %d != puts %d", gets, puts)
	}
}
