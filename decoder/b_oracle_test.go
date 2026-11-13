package decoder

import "testing"

func TestBFrames_PixelOracle(t *testing.T) {
	ff := ffmpegPath(t)
	for _, path := range []string{
		"../testdata/main/main_64x48_yuv420p_cabac_ipb_001.264",
		"../testdata/main/main_96x64_yuv420p_cabac_default_008.264",
		"../testdata/main/main_176x144_yuv420p_cabac_default_009.264",
		"../testdata/main/main_320x240_yuv420p_cabac_default_010.264",
		"../testdata/bframes/main_128x96_yuv420p_cavlc_temporal_001.264",
		"../testdata/main/main_160x96_yuv420p_cabac_weighted_011.264",
		"../testdata/main/main_16x16_yuv420p_cavlc_weightedb_012.264",
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

func TestBFrames_ExplicitWeightAnalyticSamples(t *testing.T) {
	got := decodeFile(t, "../testdata/main/main_16x16_yuv420p_cavlc_weightedb_012.264")
	if len(got) != 3*384 || got[384] != 32 || got[384+256] != 66 || got[384+320] != 101 {
		t.Fatal("explicit B plane weights or display order are wrong")
	}
}
