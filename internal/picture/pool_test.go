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
