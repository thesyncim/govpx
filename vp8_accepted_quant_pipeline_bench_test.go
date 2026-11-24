package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

var vp8AcceptedQuantSink uint8

// The accepted path consumes QCoeff/EOB for tokenization and reconstruction;
// unlike RD candidate scoring, it has no dequantized distortion consumer.
func BenchmarkVP8AcceptedQuantPipeline(b *testing.B) {
	for _, pattern := range []string{"zero", "dc", "dense"} {
		b.Run(pattern, func(b *testing.B) {
			src := testImage(16, 16)
			fillImage(src, 96, 90, 170)
			pred := testVP8Frame(b, 16, 16, 96, 90, 170)
			if pattern == "dc" {
				fillImage(src, 110, 100, 160)
			}
			if pattern == "dense" {
				for i := range src.Y {
					src.Y[i] = byte(i*53 + i/7)
				}
				for i := range src.U {
					src.U[i] = byte(i * 71)
					src.V[i] = byte(i * 131)
				}
			}
			var tables vp8common.FrameDequantTables
			var dq vp8common.MacroblockDequant
			var quant vp8enc.MacroblockQuant
			vp8common.BuildFrameDequantTables(vp8common.QuantDeltas{}, &tables)
			vp8common.InitMacroblockDequant(&tables, 50, &dq)
			vp8enc.InitFastMacroblockQuant(&dq, &quant)
			var coeffs vp8enc.MacroblockCoefficients
			args := predictedMacroblockCoefficientArgs{coefProbs: &vp8tables.DefaultCoefProbs, src: sourceImageFromPublic(src), pred: &pred.Img, quant: &quant, qIndex: 50, fastQuant: true, coeffs: &coeffs}
			buildPredictedMacroblockCoefficients(args)
			totalEOB := 0
			for _, e := range coeffs.EOB {
				totalEOB += int(e)
			}
			if pattern != "zero" && totalEOB == 0 {
				b.Fatal("coefficient path did not produce live nonzero output")
			}
			b.ReportAllocs()
			for b.Loop() {
				buildPredictedMacroblockCoefficients(args)
			}
			vp8AcceptedQuantSink = coeffs.EOB[0]
		})
	}
}
