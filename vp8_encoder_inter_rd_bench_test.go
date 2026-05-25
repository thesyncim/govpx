package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

func BenchmarkEstimateInterResidualRDAccounting(b *testing.B) {
	e := newSizedTestEncoder(b, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		b.Fatalf("SetDeadline returned error: %v", err)
	}
	var decSeg vp8dec.SegmentationHeader
	vp8dec.InitSegmentDequants(vp8dec.QuantHeader{BaseQIndex: 20}, &decSeg, &e.dequantTables, &e.dequants)
	src := testImage(16, 16)
	fillImage(src, 96, 90, 170)
	for i := range src.Y {
		src.Y[i] = byte(64 + (i*17)%96)
	}
	for i := range src.U {
		src.U[i] = byte(80 + (i*11)%48)
		src.V[i] = byte(144 + (i*7)%48)
	}
	ref := testVP8Frame(b, 16, 16, 96, 90, 170)
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	quant := testRegularMacroblockQuant(b, 20)
	source := sourceImageFromPublic(src)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		acct, ok := e.estimateInterResidualRDAccounting(source, &ref.Img, 0, 0, 1, 1, &mode, nil, nil, nil, nil, nil, &quant, 20, 0, 0)
		if !ok || acct.distortion2 == 0 {
			b.Fatalf("estimateInterResidualRDAccounting returned ok=%t acct=%+v", ok, acct)
		}
	}
}
