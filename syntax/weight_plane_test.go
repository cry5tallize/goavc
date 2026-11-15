package syntax

import (
	"testing"

	"github.com/cry5tallize/goavc/bitstream"
)

func TestPredWeightTable_SeparatePlanesUseMonochromeSyntax(t *testing.T) {
	// Spec: 7.3.3.2，ChromaArrayType=0：denom=1，luma flag=1，weight=2，offset=-3。
	// 固定比特010 1 00100 00111，末尾10属于后续语法，不能被当作色度字段消费。
	r := bitstream.NewReader([]byte{0x52, 0x1e})
	s := &SPS{ProfileIDC: 244, ChromaFormatIDC: 3, SeparateColourPlaneFlag: true}
	weights, err := parsePredWeightTable(r, s, SliceTypeP, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if weights.LumaDenom != 1 || weights.Lists[0][0].Weight[0] != 2 || weights.Lists[0][0].Offset[0] != -3 {
		t.Fatalf("wrong monochrome weight: %+v", weights)
	}
	v, err := r.ReadBits(2)
	if err != nil || v != 2 || r.BitsRemaining() != 0 {
		t.Fatalf("weight parser consumed following syntax: %d, %v", v, err)
	}
}
