package search

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/picture"
)

// 这些指纹在 G01 改造前的 4ec4ccc 上采集；同时保护重复候选、严格小于
// 和两个阶段各自的初始化。像素/插值正确性另由原有解析值与 Oracle 证明。
func TestWorkspace_FrozenSearchTrace(t *testing.T) {
	data, err := os.ReadFile("testdata/workspace_trace.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected map[string]struct {
		Calls int
		Hash  string
		X, Y  int32
		SAD   int
	}
	if err := json.Unmarshal(data, &expected); err != nil || len(expected) != 32 {
		t.Fatalf("32个改造前指纹: %v", err)
	}
	for _, depth := range []int{8, 10} {
		format := h264.YUV420P
		if depth == 10 {
			format = h264.YUV420P10LE
		}
		source, _ := picture.New(3, 4, format)
		reference, _ := picture.New(3, 4, format)
		for y := range 64 {
			for x := range 48 {
				reference.Planes[0].SetSample(x, y, uint16((x*x*29+y*y*47+x*y*13+3*x+1)&((1<<depth)-1)))
				source.Planes[0].SetSample(x, y, uint16((x*x*11+y*y*31+x*y*7+3*y+2)&((1<<depth)-1)))
			}
		}
		for _, field := range []bool{false, true} {
			src, ref := source, reference
			if field {
				a, _ := picture.FieldView(source, true)
				b, _ := picture.FieldView(reference, true)
				src, ref = &a, &b
			}
			for _, full := range []bool{false, true} {
				for _, edge := range []bool{false, true} {
					for _, ties := range []bool{false, true} {
						name := fmt.Sprintf("%d/field=%v/full=%v/edge=%v/ties=%v", depth, field, full, edge, ties)
						t.Run(name, func(t *testing.T) {
							x, y := 13, 7
							if edge {
								x, y = 0, 0
							}
							calls, hash := 0, uint64(14695981039346656037)
							scorer := func(m MV, sad int) uint64 {
								for _, v := range [...]uint64{uint64(uint32(m.X)), uint64(uint32(m.Y)), uint64(sad)} {
									hash = (hash ^ v) * 1099511628211
								}
								calls++
								if ties {
									return 7
								}
								return uint64(sad)*256 + uint64(absTrace(int(m.X)-5)+absTrace(int(m.Y)+3))*67
							}
							var workspace Workspace
							mv, sad := EstimateWithCost(&src.Planes[0], &ref.Planes[0], x, y, 7, 13, MV{X: 5, Y: -3}, 3, full, scorer, &workspace)
							want, ok := expected[name]
							if !ok || calls != want.Calls || fmt.Sprintf("%016x", hash) != want.Hash || mv != (MV{X: want.X, Y: want.Y}) || sad != want.SAD {
								t.Fatalf("got calls=%d hash=%016x MV=%v SAD=%d; want %+v", calls, hash, mv, sad, want)
							}
						})
					}
				}
			}
		}
	}
}

func absTrace(v int) int { return max(v, -v) }
