package decoder

import (
	"testing"
)

func TestReferenceLists_MultiReferenceOracle(t *testing.T) {
	ff := ffmpegPath(t)
	for _, path := range []string{"../testdata/main/main_128x96_yuv420p_cabac_multiref_005.264", "../testdata/baseline/baseline_96x64_yuv420p_cavlc_multiref_006.264", "../testdata/main/main_16x16_yuv420p_cavlc_mmco_006.264", "../testdata/main/main_16x16_yuv420p_cavlc_poc1_007.264"} {
		t.Run(path, func(t *testing.T) {
			got, want := decodeFile(t, path), ffmpegRaw(t, ff, path)
			if len(got) != len(want) {
				t.Fatalf("length %d want %d", len(got), len(want))
			}
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("first difference byte %d: got %d want %d", i, got[i], want[i])
				}
			}
		})
	}
}
