package cabac

import (
	"github.com/cry5tallize/goavc/bitstream"
	"github.com/cry5tallize/goavc/internal/transform"
	"math/rand"
	"testing"
)

func TestResidualEncoder_AllCategoriesAndEscapes(t *testing.T) {
	scans := [][]int{transform.LumaDCScan[:], transform.Zigzag4x4[1:], transform.Zigzag4x4[:], transform.DC2x2Scan[:], transform.Zigzag4x4[1:], transform.Zigzag8x8[:]}
	rng := rand.New(rand.NewSource(426))
	for cat, scan := range scans {
		for trial := 0; trial < 200; trial++ {
			var src, dst [64]int32
			count := 0
			for _, index := range scan {
				if rng.Intn(3) == 0 {
					src[index] = int32(rng.Intn(63) - 31)
					if src[index] != 0 {
						count++
					}
				}
			}
			if count == 0 {
				src[scan[len(scan)-1]] = 1
				count = 1
			}
			if trial == 0 {
				clear(src[:])
				src[scan[len(scan)-1]] = -1000000
				count = 1
			}
			contexts, _ := InitContexts(true, 0, trial%52)
			decoded := contexts
			w := bitstream.NewWriter()
			e, _ := NewEncoder(w)
			got, err := e.Residual(&contexts, cat, src[:], scan)
			if err != nil || got != count {
				t.Fatal(cat, trial, got, count, err)
			}
			e.Terminate(1)
			r := bitstream.NewReader(w.Bytes())
			d, err := New(r)
			if err != nil {
				t.Fatal(err)
			}
			got, err = d.Residual(&decoded, cat, dst[:], scan)
			if err != nil || got != count || dst != src || contexts != decoded || d.Terminate() != 1 || d.Err() != nil {
				t.Fatalf("cat%d trial%d count%d/%d error%v arithmetic%v", cat, trial, got, count, err, d.Err())
			}
		}
	}
}
