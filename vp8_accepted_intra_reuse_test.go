package govpx

import (
	"bytes"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

var acceptedIntraSink int

func acceptedIntraLegacy(img *vp8common.Image, row, col int, mode *vp8dec.MacroblockMode, c *vp8enc.MacroblockCoefficients, d *vp8common.MacroblockDequant) bool {
	var tok vp8dec.MacroblockTokens
	vp8enc.ConvertMacroblockCoefficients(c, false, &tok)
	var scratch vp8dec.IntraReconstructionScratch
	return reconstructAnalysisMacroblock(img, row, col, mode, &tok, d, &scratch)
}
func TestAcceptedWholeIntraReuse(t *testing.T) {
	var tables vp8common.FrameDequantTables
	vp8common.BuildFrameDequantTables(vp8common.QuantDeltas{}, &tables)
	for _, q := range []int{0, 40, 127} {
		var dequant vp8common.MacroblockDequant
		vp8common.InitMacroblockDequant(&tables, q, &dequant)
		for _, pos := range [][2]int{{0, 0}, {0, 1}, {1, 0}, {1, 1}} {
			for ym := vp8common.DCPred; ym <= vp8common.TMPred; ym++ {
				for um := vp8common.DCPred; um <= vp8common.TMPred; um++ {
					for _, y2 := range []int{0, 1, 4, 16} {
						a, z := newDirtyStateTestImage(t), newDirtyStateTestImage(t)
						mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: ym, UVMode: um}
						c := buildDirtyStateTestCoefficients(false, y2)
						original := *c
						var scratch vp8dec.IntraReconstructionScratch
						if !predictAnalysisMacroblock(&a.Img, pos[0], pos[1], &mode, &scratch) || !predictAnalysisMacroblock(&z.Img, pos[0], pos[1], &mode, &scratch) {
							t.Fatal("predict")
						}
						if !addWholeIntraResidualToAnalysisMacroblockCoeffs(&a.Img, pos[0], pos[1], &mode, c, &dequant) || !acceptedIntraLegacy(&z.Img, pos[0], pos[1], &mode, &original, &dequant) {
							t.Fatal("reconstruct")
						}
						if !bytes.Equal(a.Img.YFull, z.Img.YFull) || !bytes.Equal(a.Img.UFull, z.Img.UFull) || !bytes.Equal(a.Img.VFull, z.Img.VFull) {
							t.Fatalf("q=%d pos=%v mode=%d/%d y2=%d", q, pos, ym, um, y2)
						}
						if *c != original {
							t.Fatal("coefficient mutation")
						}
					}
				}
			}
		}
	}
}
func BenchmarkAcceptedWholeIntraReuse(b *testing.B) {
	for _, kind := range []string{"dc", "mixed", "skip"} {
		b.Run(kind, func(b *testing.B) {
			fb, err := vp8common.NewFrameBuffer(32, 32, 32, 16)
			if err != nil {
				b.Fatal(err)
			}
			for i := range fb.Img.YFull {
				fb.Img.YFull[i] = 100
			}
			for i := range fb.Img.UFull {
				fb.Img.UFull[i] = 110
				fb.Img.VFull[i] = 120
			}
			var tables vp8common.FrameDequantTables
			vp8common.BuildFrameDequantTables(vp8common.QuantDeltas{}, &tables)
			var dq vp8common.MacroblockDequant
			vp8common.InitMacroblockDequant(&tables, 40, &dq)
			mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred, UVMode: vp8common.DCPred}
			c := buildDirtyStateTestCoefficients(false, 4)
			if kind == "dc" {
				c = &vp8enc.MacroblockCoefficients{}
				c.QCoeff[24][0] = 10
				c.SetBlockEOB(24, 1)
			}
			if kind == "skip" {
				mode.MBSkipCoeff = true
			}
			var scratch vp8dec.IntraReconstructionScratch
			b.ReportAllocs()
			for b.Loop() {
				if !predictAnalysisMacroblock(&fb.Img, 1, 1, &mode, &scratch) || !addWholeIntraResidualToAnalysisMacroblockCoeffs(&fb.Img, 1, 1, &mode, c, &dq) {
					b.Fatal("reconstruct")
				}
			}
			acceptedIntraSink = int(fb.Img.Y[16*fb.Img.YStride+16]) + int(fb.Img.U[8*fb.Img.UStride+8])
		})
	}
}

func TestAcceptedWholeIntraReuseSkipped(t *testing.T) {
	var tables vp8common.FrameDequantTables
	vp8common.BuildFrameDequantTables(vp8common.QuantDeltas{}, &tables)
	var dq vp8common.MacroblockDequant
	vp8common.InitMacroblockDequant(&tables, 40, &dq)
	for _, pos := range [][2]int{{0, 0}, {0, 1}, {1, 0}, {1, 1}} {
		for ym := vp8common.DCPred; ym <= vp8common.TMPred; ym++ {
			for um := vp8common.DCPred; um <= vp8common.TMPred; um++ {
				a, z := newDirtyStateTestImage(t), newDirtyStateTestImage(t)
				mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: ym, UVMode: um, MBSkipCoeff: true}
				c := buildDirtyStateTestCoefficients(false, 16)
				original := *c
				var scratch vp8dec.IntraReconstructionScratch
				if !predictAnalysisMacroblock(&a.Img, pos[0], pos[1], &mode, &scratch) || !predictAnalysisMacroblock(&z.Img, pos[0], pos[1], &mode, &scratch) {
					t.Fatal("predict")
				}
				if !addWholeIntraResidualToAnalysisMacroblockCoeffs(&a.Img, pos[0], pos[1], &mode, c, &dq) || !acceptedIntraLegacy(&z.Img, pos[0], pos[1], &mode, &original, &dq) {
					t.Fatal("skip")
				}
				if !bytes.Equal(a.Img.YFull, z.Img.YFull) || !bytes.Equal(a.Img.UFull, z.Img.UFull) || !bytes.Equal(a.Img.VFull, z.Img.VFull) || *c != original {
					t.Fatalf("skip pos=%v mode=%d/%d", pos, ym, um)
				}
			}
		}
	}
}
