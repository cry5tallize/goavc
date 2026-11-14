package decoder

import (
	"github.com/goavc/h264/bitstream"
	"github.com/goavc/h264/internal/entropy/cabac"
	"github.com/goavc/h264/internal/entropy/cavlc"
	"github.com/goavc/h264/internal/transform"
)

func (fs *frameState) scan4() []int {
	if fs.field {
		return transform.FieldScan4x4[:]
	}
	return transform.Zigzag4x4[:]
}

func (fs *frameState) scan8() []int {
	if fs.field {
		return transform.FieldScan8x8[:]
	}
	return transform.Zigzag8x8[:]
}

func (fs *frameState) scanDC() []int {
	if fs.field {
		return transform.FieldLumaDCScan[:]
	}
	return transform.LumaDCScan[:]
}

func (fs *frameState) decodeBlock(r *bitstream.Reader, dst *[16]int32, typ cavlc.BlockType, nC int) (int, error) {
	if !fs.field || typ == cavlc.ChromaDC || typ == cavlc.ChromaDC422 {
		return cavlc.DecodeBlock(r, dst, typ, nC)
	}
	scan := fs.scan4()
	if typ == cavlc.LumaDC {
		scan = fs.scanDC()
	} else if typ == cavlc.LumaAC {
		scan = scan[1:]
	}
	return cavlc.DecodeBlockScan(r, dst, typ, nC, scan)
}

func (fs *frameState) cabacResidual(c *cabacSyntax, cat int, dst []int32, scan []int) (int, error) {
	if fs.sps.ChromaFormatIDC == 3 && !fs.sps.SeparateColourPlaneFlag {
		return c.d.Residual444(&c.ctx, cat, dst, scan, fs.field)
	}
	ctx := (*[cabac.ContextCount]cabac.Context)(c.ctx[:cabac.ContextCount])
	if fs.field {
		return c.d.ResidualField(ctx, cat, dst, scan)
	}
	return c.d.Residual(ctx, cat, dst, scan)
}
