package decoder

import (
	"encoding/json"
	"os"
	"runtime"
	"runtime/metrics"
	"testing"

	"github.com/cry5tallize/goavc/internal/dpb"
)

// C05实验：每张图的两列表共享字典。令牌只在所属字典内比较，0表示无参考。
// 字典保存原身份指针而非快照，长期参考标记更新和派生场关系仍可见。
type referenceTokenExperiment struct {
	indices map[*dpb.Identity]uint32
	records []*dpb.Identity
}

// 仅由显式环境变量启用的独立进程留存探针；强制GC计时不混入吞吐基准。
// 八份1080p两列表模拟仍被DPB/共位块持有的元数据，不代表完整decoder live heap。
func TestReferenceTokens_RetainedHeapProbe(t *testing.T) {
	layout := os.Getenv("GOAVC_IDENTITY_LAYOUT")
	if layout == "" {
		t.Skip("需显式设置GOAVC_IDENTITY_LAYOUT运行独立留存实验")
	}
	if layout != "pointer" && layout != "token" {
		t.Fatal("未知身份布局")
	}
	type retained struct {
		pointers   [2][]*dpb.Identity
		tokens     [2][]uint32
		dictionary referenceTokenExperiment
	}
	const blocks, pictures = 120 * 68 * 16, 8
	scanBytes := func() uint64 {
		s := []metrics.Sample{{Name: "/gc/scan/heap:bytes"}}
		metrics.Read(s)
		return s[0].Value.Uint64()
	}
	runtime.GC()
	var before, live, released runtime.MemStats
	runtime.ReadMemStats(&before)
	baseScan := scanBytes()
	frames := make([]retained, pictures)
	for frame := range frames {
		var ids [16]dpb.Identity
		if layout == "token" {
			frames[frame].dictionary.indices = make(map[*dpb.Identity]uint32, len(ids))
		}
		for list := range 2 {
			if layout == "pointer" {
				frames[frame].pointers[list] = make([]*dpb.Identity, blocks)
			} else {
				frames[frame].tokens[list] = make([]uint32, blocks)
			}
			for i := range blocks {
				id := &ids[(i/16+list)&15]
				if layout == "pointer" {
					frames[frame].pointers[list][i] = id
				} else {
					frames[frame].tokens[list][i] = frames[frame].dictionary.intern(id)
				}
			}
		}
		// 模拟建表结束后不再保留查找map，稳定字典仍随图像存活。
		frames[frame].dictionary.indices = nil
	}
	runtime.GC()
	runtime.ReadMemStats(&live)
	liveScan := scanBytes()
	runtime.KeepAlive(frames)
	frames = nil
	runtime.GC()
	runtime.ReadMemStats(&released)
	releasedScan := scanBytes()
	// 有符号差值保留GC统计噪声，不把负数钳成伪造的零。
	result := struct {
		Layout            string `json:"layout"`
		Pictures          int    `json:"pictures"`
		LiveHeapDelta     int64  `json:"live_heap_delta"`
		LiveScanDelta     int64  `json:"live_scan_delta"`
		ReleasedHeapDelta int64  `json:"released_heap_delta"`
		ReleasedScanDelta int64  `json:"released_scan_delta"`
	}{layout, pictures, int64(live.HeapAlloc) - int64(before.HeapAlloc), int64(liveScan) - int64(baseScan), int64(released.HeapAlloc) - int64(before.HeapAlloc), int64(releasedScan) - int64(baseScan)}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("C05_HEAP %s", data)
}

func (d *referenceTokenExperiment) intern(id *dpb.Identity) uint32 {
	if id == nil {
		return 0
	}
	if token := d.indices[id]; token != 0 {
		return token
	}
	d.records = append(d.records, id)
	token := uint32(len(d.records))
	d.indices[id] = token
	return token
}

func (d *referenceTokenExperiment) resolve(token uint32) *dpb.Identity {
	if token == 0 {
		return nil
	}
	return d.records[token-1]
}

func TestReferenceTokens_PictureLocalIdentity(t *testing.T) {
	frame := &dpb.Identity{ID: 7, POC: 12}
	top := &dpb.Identity{ID: 7, POC: 12, Frame: frame}
	bottom := &dpb.Identity{ID: 7, POC: 12, Frame: frame, BottomField: true}
	other := &dpb.Identity{ID: 8, POC: 12}
	// 即便ID/POC完全相同，原指针语义不同也不能悄悄合并。
	aliasNumbers := &dpb.Identity{ID: 7, POC: 12}
	// 两个 slice 的相同 ref_idx 指向不同图，同图也可跨列表出现。
	writes := []*dpb.Identity{frame, other, top, bottom, nil, other, frame, aliasNumbers}
	old := referenceTokenExperiment{indices: make(map[*dpb.Identity]uint32)}
	tokens := make([]uint32, len(writes))
	for i, id := range writes {
		tokens[i] = old.intern(id)
	}
	for i, id := range writes {
		if got := old.resolve(tokens[i]); got != id {
			t.Fatalf("块%d身份改变：%p != %p", i, got, id)
		}
		for j, second := range writes {
			if (tokens[i] == tokens[j]) != (id == second) {
				t.Fatalf("块%d/%d 身份比较改变", i, j)
			}
		}
	}
	// 模拟 MMCO 更新及后续图像反复复用令牌值；旧共位块仍由旧字典解释。
	frame.LongTerm, frame.LongTermIdx = true, 3
	for i := range 4096 {
		next := referenceTokenExperiment{indices: make(map[*dpb.Identity]uint32)}
		next.intern(&dpb.Identity{ID: uint64(i + 9)})
		if got := old.resolve(tokens[3]); got != bottom || !got.Frame.LongTerm || got.Frame.LongTermIdx != 3 {
			t.Fatal("后续图像覆盖了仍被共位块借用的身份")
		}
	}
	// 图像释放时字典所有者一起解除，不遗留全局索引；容量计费须在生产迁移时另接入。
	old.indices = nil
	old.records = nil
	if old.resolve(0) != nil || old.indices != nil || old.records != nil {
		t.Fatal("空身份或字典释放状态不符")
	}
}

var referenceTokenBenchSink *dpb.Identity
var referenceTokenBenchMatches int

func BenchmarkReferenceIdentity_1080p_Layout(b *testing.B) {
	const blocks = 120 * 68 * 16
	var ids [16]dpb.Identity
	for _, compact := range []bool{false, true} {
		name := "pointer"
		if compact {
			name = "token"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var id *dpb.Identity
				matches := 0
				if compact {
					d := referenceTokenExperiment{indices: make(map[*dpb.Identity]uint32, len(ids))}
					var list [2][16]uint32
					for i := range ids {
						list[0][i] = d.intern(&ids[i])
						list[1][i] = d.intern(&ids[len(ids)-1-i])
					}
					a, c := make([]uint32, blocks), make([]uint32, blocks)
					for i := range a {
						j := (i / 16) & 15
						a[i], c[i] = list[0][j], list[1][j]
					}
					for i := 1; i < blocks; i++ {
						if a[i] == a[i-1] && c[i] == c[i-1] {
							matches++
						}
						if i&15 == 0 {
							id = d.resolve(a[i])
						}
					}
				} else {
					a, c := make([]*dpb.Identity, blocks), make([]*dpb.Identity, blocks)
					for i := range a {
						j := (i / 16) & 15
						a[i], c[i] = &ids[j], &ids[15-j]
					}
					for i := 1; i < blocks; i++ {
						if a[i] == a[i-1] && c[i] == c[i-1] {
							matches++
						}
						if i&15 == 0 {
							id = a[i]
						}
					}
				}
				referenceTokenBenchSink, referenceTokenBenchMatches = id, matches
			}
		})
	}
}
