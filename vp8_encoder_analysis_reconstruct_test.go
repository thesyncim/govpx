package govpx

import (
	"fmt"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// fillQuantizerContractBlock writes a coefficient block that honors the
// encoder quantizer output contract: every one of the 16 slots is written,
// with nonzero values only at zigzag positions < eob. This mirrors what
// QuantizeBlockWithZbinAndActivity / FastQuantizeBlock leave behind (all
// slots stored, zeros at and beyond the EOB in zigzag order). skipDC
// mirrors the second-order invariant: Y blocks of a Y2-active macroblock
// always carry qcoeff[0] == 0 (the builder zeroes the DC because the real
// DC lives in the Y2 block — vp8_encoder_inter_coefficients.go).
func fillQuantizerContractBlock(block *[16]int16, eob int, seed int16, skipDC bool) {
	*block = [16]int16{}
	for pos := range eob {
		if pos == 0 && skipDC {
			continue
		}
		rc := int(vp8tables.DefaultZigZag1D[pos])
		v := seed + int16(pos)
		if pos%2 == 1 {
			v = -v
		}
		block[rc] = v
	}
}

// buildDirtyStateTestCoefficients builds a MacroblockCoefficients whose EOB
// layout exercises every fused pair-kernel dispatch class: (0,0), (1,1),
// (0,>1), (1,>1), (>1,0), (>1,>1), (0,1) and (1,0) luma pairs, plus mixed
// chroma pairs and a configurable Y2 EOB.
func buildDirtyStateTestCoefficients(is4x4 bool, y2EOB int) *vp8enc.MacroblockCoefficients {
	var coeffs vp8enc.MacroblockCoefficients
	yEOBs := [16]int{0, 0, 1, 1, 0, 3, 1, 5, 16, 0, 2, 2, 0, 1, 1, 0}
	uvEOBs := [8]int{0, 0, 1, 4, 3, 1, 2, 2}
	// Y blocks of a Y2-active (non-4x4) macroblock never carry a DC
	// coefficient; the eob==1 entries above then behave like the
	// production "eob promoted, qcoeff[0]==0" shape.
	skipDC := !is4x4
	for i, eob := range yEOBs {
		fillQuantizerContractBlock(&coeffs.QCoeff[i], eob, int16(3+i), skipDC)
		coeffs.SetBlockEOB(i, eob)
	}
	for i, eob := range uvEOBs {
		fillQuantizerContractBlock(&coeffs.QCoeff[16+i], eob, int16(2+i), false)
		coeffs.SetBlockEOB(16+i, eob)
	}
	fillQuantizerContractBlock(&coeffs.QCoeff[24], y2EOB, 4, false)
	coeffs.SetBlockEOB(24, y2EOB)
	return &coeffs
}

func newDirtyStateTestImage(t *testing.T) *vp8common.FrameBuffer {
	t.Helper()
	fb, err := vp8common.NewFrameBuffer(32, 32, 32, 16)
	if err != nil {
		t.Fatalf("NewFrameBuffer: %v", err)
	}
	img := &fb.Img
	for i := range img.Y {
		img.Y[i] = byte(64 + (i*7)%128)
	}
	for i := range img.U {
		img.U[i] = byte(80 + (i*5)%96)
	}
	for i := range img.V {
		img.V[i] = byte(96 + (i*3)%64)
	}
	return fb
}

// TestAddInterResidualFusedTokensDirtyStateParity pins the dirty-buffer
// contract of the fused encoder recon path (vp8_inverse_transform_mby +
// vp8_dequant_idct_add_uv_block, encodeframe.c:1288-1291): the encoder's
// decoder-shaped token buffers are reused across macroblocks and
// ConvertMacroblockCoefficients only maintains the slots a block owns
// (nothing for EOB==0, the DC for EOB==1, the full quantizer array for
// EOB>=2). Reconstruction from a garbage-saturated reused buffer must be
// byte-identical to reconstruction from a pristine zero buffer, and the
// fused path must match the legacy unfused Transform+Add reference chain.
func TestAddInterResidualFusedTokensDirtyStateParity(t *testing.T) {
	var dequantTables vp8common.FrameDequantTables
	vp8common.BuildFrameDequantTables(vp8common.QuantDeltas{}, &dequantTables)
	var dequant vp8common.MacroblockDequant
	vp8common.InitMacroblockDequant(&dequantTables, 40, &dequant)

	cases := []struct {
		name  string
		is4x4 bool
		y2EOB int
	}{
		{name: "wholeMV_y2_eob0", is4x4: false, y2EOB: 0},
		{name: "wholeMV_y2_eob1", is4x4: false, y2EOB: 1},
		{name: "wholeMV_y2_eob4", is4x4: false, y2EOB: 4},
		{name: "wholeMV_y2_eob16", is4x4: false, y2EOB: 16},
		{name: "splitMV_4x4", is4x4: true, y2EOB: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			coeffs := buildDirtyStateTestCoefficients(tc.is4x4, tc.y2EOB)

			mode := vp8dec.MacroblockMode{
				RefFrame: vp8common.LastFrame,
				Mode:     vp8common.NewMV,
				Is4x4:    tc.is4x4,
			}
			if tc.is4x4 {
				mode.Mode = vp8common.SplitMV
			}

			run := func(tokens *vp8dec.MacroblockTokens) *vp8common.FrameBuffer {
				fb := newDirtyStateTestImage(t)
				var scratch vp8dec.IntraReconstructionScratch
				if !addInterResidualToAnalysisMacroblock(&fb.Img, 1, 1, &mode, tokens, &dequant, &scratch) {
					t.Fatalf("addInterResidualToAnalysisMacroblock returned false")
				}
				return fb
			}

			// Clean buffer: pristine zero-state destination.
			var cleanTokens vp8dec.MacroblockTokens
			vp8enc.ConvertMacroblockCoefficients(coeffs, tc.is4x4, &cleanTokens)
			cleanFB := run(&cleanTokens)

			// Dirty buffer: every coefficient slot pre-saturated with garbage,
			// mimicking reuse after a macroblock whose blocks all carried 16
			// coefficients. Convert only overwrites the slots each block owns.
			var dirtyTokens vp8dec.MacroblockTokens
			for b := range dirtyTokens.QCoeff {
				for i := range dirtyTokens.QCoeff[b] {
					dirtyTokens.QCoeff[b][i] = 0x2f2f
				}
			}
			vp8enc.ConvertMacroblockCoefficients(coeffs, tc.is4x4, &dirtyTokens)
			dirtyFB := run(&dirtyTokens)

			comparePlanes := func(label string, a, b []byte) {
				t.Helper()
				if len(a) != len(b) {
					t.Fatalf("%s plane length mismatch: %d vs %d", label, len(a), len(b))
				}
				for i := range a {
					if a[i] != b[i] {
						t.Fatalf("%s plane diverges at offset %d: clean=%d dirty=%d", label, i, a[i], b[i])
					}
				}
			}
			comparePlanes("Y", cleanFB.Img.Y, dirtyFB.Img.Y)
			comparePlanes("U", cleanFB.Img.U, dirtyFB.Img.U)
			comparePlanes("V", cleanFB.Img.V, dirtyFB.Img.V)

			// Reference chain: the pre-fusion unfused Transform+Add pipeline
			// must produce the same bytes for encoder-range coefficients.
			var refTokens vp8dec.MacroblockTokens
			vp8enc.ConvertMacroblockCoefficients(coeffs, tc.is4x4, &refTokens)
			refFB := newDirtyStateTestImage(t)
			img := &refFB.Img
			yOff := 16*img.YStride + 16
			uOff := 8*img.UStride + 8
			vOff := 8*img.VStride + 8
			var residual vp8dec.MacroblockResidual
			vp8dec.TransformMacroblockTokens(&refTokens, &dequant, tc.is4x4, &residual)
			vp8dec.AddMacroblockResidual(&refTokens, &residual, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride)
			applyLibvpxY2EobAdjustToAnalysisMacroblock(&refTokens, tc.is4x4, &residual, img.Y[yOff:], img.YStride)

			comparePlanes("Y-vs-unfused", cleanFB.Img.Y, refFB.Img.Y)
			comparePlanes("U-vs-unfused", cleanFB.Img.U, refFB.Img.U)
			comparePlanes("V-vs-unfused", cleanFB.Img.V, refFB.Img.V)
		})
	}
}

// TestAddInterResidualFusedTokensSequentialReuseParity drives the realistic
// staleness pattern: the same MacroblockTokens struct converted for a
// high-EOB macroblock and then reused for a low-EOB macroblock, exactly how
// e.reconstructTokens entries are recycled across frames.
func TestAddInterResidualFusedTokensSequentialReuseParity(t *testing.T) {
	var dequantTables vp8common.FrameDequantTables
	vp8common.BuildFrameDequantTables(vp8common.QuantDeltas{}, &dequantTables)
	var dequant vp8common.MacroblockDequant
	vp8common.InitMacroblockDequant(&dequantTables, 12, &dequant)

	for _, is4x4 := range []bool{false, true} {
		t.Run(fmt.Sprintf("is4x4=%v", is4x4), func(t *testing.T) {
			// Predecessor MB: everything at full EOB so every slot is nonzero.
			full := buildDirtyStateTestCoefficients(is4x4, 16)
			skipDC := !is4x4
			for i := range 24 {
				fillQuantizerContractBlock(&full.QCoeff[i], 16, int16(5+i), skipDC && i < 16)
				full.SetBlockEOB(i, 16)
			}

			// Successor MB: sparse EOBs.
			sparse := buildDirtyStateTestCoefficients(is4x4, 1)

			mode := vp8dec.MacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, Is4x4: is4x4}
			if is4x4 {
				mode.Mode = vp8common.SplitMV
			}

			var reused vp8dec.MacroblockTokens
			vp8enc.ConvertMacroblockCoefficients(full, is4x4, &reused)
			// Simulate the predecessor reconstruction consuming the tokens.
			warmFB := newDirtyStateTestImage(t)
			var scratch vp8dec.IntraReconstructionScratch
			if !addInterResidualToAnalysisMacroblock(&warmFB.Img, 0, 0, &mode, &reused, &dequant, &scratch) {
				t.Fatalf("warm-up reconstruction failed")
			}
			// Reuse for the sparse MB.
			vp8enc.ConvertMacroblockCoefficients(sparse, is4x4, &reused)
			reusedFB := newDirtyStateTestImage(t)
			if !addInterResidualToAnalysisMacroblock(&reusedFB.Img, 1, 1, &mode, &reused, &dequant, &scratch) {
				t.Fatalf("reused reconstruction failed")
			}

			var pristine vp8dec.MacroblockTokens
			vp8enc.ConvertMacroblockCoefficients(sparse, is4x4, &pristine)
			pristineFB := newDirtyStateTestImage(t)
			if !addInterResidualToAnalysisMacroblock(&pristineFB.Img, 1, 1, &mode, &pristine, &dequant, &scratch) {
				t.Fatalf("pristine reconstruction failed")
			}

			for i := range pristineFB.Img.Y {
				if pristineFB.Img.Y[i] != reusedFB.Img.Y[i] {
					t.Fatalf("Y diverges at %d: pristine=%d reused=%d", i, pristineFB.Img.Y[i], reusedFB.Img.Y[i])
				}
			}
			for i := range pristineFB.Img.U {
				if pristineFB.Img.U[i] != reusedFB.Img.U[i] {
					t.Fatalf("U diverges at %d", i)
				}
			}
			for i := range pristineFB.Img.V {
				if pristineFB.Img.V[i] != reusedFB.Img.V[i] {
					t.Fatalf("V diverges at %d", i)
				}
			}
		})
	}
}
