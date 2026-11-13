package decoder

import "testing"

func TestHigh_PixelOracle(t *testing.T) {
	ff := ffmpegPath(t)
	for _, path := range []string{
		"../testdata/high/high_96x64_yuv420p_cabac_default_001.264",
		"../testdata/high/high_176x144_yuv420p_cabac_default_002.264",
		"../testdata/high/high_320x240_yuv420p_cabac_default_003.264",
		"../testdata/high/high_160x96_yuv420p_cabac_jvt_004.264",
		"../testdata/high/high_128x96_yuv420p_cavlc_jvt_005.264",
		"../testdata/high/high_64x48_yuv420p_cabac_default_006.264",
		"../testdata/high/high_192x128_yuv420p_cabac_multislice_007.264",
	} {
		t.Run(path, func(t *testing.T) {
			got, want := decodeFile(t, path), ffmpegRaw(t, ff, path)
			if len(got) != len(want) {
				t.Fatalf("length %d want %d", len(got), len(want))
			}
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("first difference byte %d got %d want %d", i, got[i], want[i])
				}
			}
		})
	}
}
