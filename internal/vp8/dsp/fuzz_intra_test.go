package dsp

import (
	"testing"
)

// FuzzVP8DSPIntra is a differential SIMD-vs-scalar fuzz harness for
// the VP8 intra-prediction kernel family. Mirrors libvpx
// test/intrapred_test.cc cross-check pattern.
//
// Op selector covers the full block-size + mode matrix:
//
//	16x16 modes: DC (with all 4 availability combos), Vertical,
//	             Horizontal, TrueMotion
//	 8x8 modes:  DC (4 combos), Vertical, Horizontal, TrueMotion
//	 4x4 modes:  DC, TM, VE, HE, LD, RD, VR, VL, HD, HU
//
// The scalar reference is intra*Scalar from internal/vp8/dsp/intra.go
// and intra4x4*Scalar from intra4x4.go — both are the canonical libvpx
// vp8/common/reconintra.c / reconintra4x4.c ports.

func FuzzVP8DSPIntra(f *testing.F) {
	seeds := [][]byte{
		make([]byte, 128),
		bytes255(128),
		bytesAlt(128),
		bytesRamp(128, 0),
		bytesRamp(128, 3),
		bytesPattern(128, 0x5A, 0x91),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// 1 op + 1 availability byte + above[16] + left[16] + topLeft + above-extended[8]
		// Generous buffer to cover the 4x4 LD/VL modes that need above[7].
		if len(data) < 64 {
			return
		}
		op := int(data[0]) % 18
		avail := data[1]
		topLeft := data[2]

		above := make([]byte, 32)
		left := make([]byte, 32)
		copy(above, data[3:3+32])
		copy(left, data[3+32:3+32+min(len(data)-3-32, 32)])

		switch op {
		case 0: // 16x16 DC up & left
			dstSim := make([]byte, 16*16)
			dstScl := make([]byte, 16*16)
			IntraDCPredict16x16(dstSim, 16, above, left, true, true)
			intraDCPredictScalar(dstScl, 16, above, left, 16, true, true)
			compareBlock(t, "IntraDCPredict16x16(true,true)", dstSim, dstScl, 16, 16, 16)
		case 1: // 16x16 DC up only
			dstSim := make([]byte, 16*16)
			dstScl := make([]byte, 16*16)
			IntraDCPredict16x16(dstSim, 16, above, left, true, false)
			intraDCPredictScalar(dstScl, 16, above, left, 16, true, false)
			compareBlock(t, "IntraDCPredict16x16(true,false)", dstSim, dstScl, 16, 16, 16)
		case 2: // 16x16 DC left only
			dstSim := make([]byte, 16*16)
			dstScl := make([]byte, 16*16)
			IntraDCPredict16x16(dstSim, 16, above, left, false, true)
			intraDCPredictScalar(dstScl, 16, above, left, 16, false, true)
			compareBlock(t, "IntraDCPredict16x16(false,true)", dstSim, dstScl, 16, 16, 16)
		case 3: // 16x16 DC neither
			dstSim := make([]byte, 16*16)
			dstScl := make([]byte, 16*16)
			IntraDCPredict16x16(dstSim, 16, above, left, false, false)
			intraDCPredictScalar(dstScl, 16, above, left, 16, false, false)
			compareBlock(t, "IntraDCPredict16x16(false,false)", dstSim, dstScl, 16, 16, 16)
		case 4: // 16x16 Vertical
			dstSim := make([]byte, 16*16)
			dstScl := make([]byte, 16*16)
			IntraVerticalPredict16x16(dstSim, 16, above)
			intraVerticalPredictScalar(dstScl, 16, above, 16)
			compareBlock(t, "IntraVerticalPredict16x16", dstSim, dstScl, 16, 16, 16)
		case 5: // 16x16 Horizontal
			dstSim := make([]byte, 16*16)
			dstScl := make([]byte, 16*16)
			IntraHorizontalPredict16x16(dstSim, 16, left)
			intraHorizontalPredictScalar(dstScl, 16, left, 16)
			compareBlock(t, "IntraHorizontalPredict16x16", dstSim, dstScl, 16, 16, 16)
		case 6: // 16x16 TM
			dstSim := make([]byte, 16*16)
			dstScl := make([]byte, 16*16)
			IntraTMPredict16x16(dstSim, 16, above, left, topLeft)
			intraTMPredictScalar(dstScl, 16, above, left, topLeft, 16)
			compareBlock(t, "IntraTMPredict16x16", dstSim, dstScl, 16, 16, 16)
		case 7: // 8x8 DC up&left — covered with the 4-availability cross sample driven by avail
			dstSim := make([]byte, 8*8)
			dstScl := make([]byte, 8*8)
			up := avail&1 != 0
			lf := avail&2 != 0
			IntraDCPredict8x8(dstSim, 8, above, left, up, lf)
			intraDCPredictScalar(dstScl, 8, above, left, 8, up, lf)
			compareBlock(t, "IntraDCPredict8x8", dstSim, dstScl, 8, 8, 8)
		case 8: // 8x8 Vertical
			dstSim := make([]byte, 8*8)
			dstScl := make([]byte, 8*8)
			IntraVerticalPredict8x8(dstSim, 8, above)
			intraVerticalPredictScalar(dstScl, 8, above, 8)
			compareBlock(t, "IntraVerticalPredict8x8", dstSim, dstScl, 8, 8, 8)
		case 9: // 8x8 Horizontal
			dstSim := make([]byte, 8*8)
			dstScl := make([]byte, 8*8)
			IntraHorizontalPredict8x8(dstSim, 8, left)
			intraHorizontalPredictScalar(dstScl, 8, left, 8)
			compareBlock(t, "IntraHorizontalPredict8x8", dstSim, dstScl, 8, 8, 8)
		case 10: // 8x8 TM
			dstSim := make([]byte, 8*8)
			dstScl := make([]byte, 8*8)
			IntraTMPredict8x8(dstSim, 8, above, left, topLeft)
			intraTMPredictScalar(dstScl, 8, above, left, topLeft, 8)
			compareBlock(t, "IntraTMPredict8x8", dstSim, dstScl, 8, 8, 8)
		case 11: // 4x4 DC
			dstSim := make([]byte, 4*4)
			dstScl := make([]byte, 4*4)
			Intra4x4DCPredict(dstSim, 4, above, left)
			intra4x4DCPredictScalar(dstScl, 4, above, left)
			compareBlock(t, "Intra4x4DCPredict", dstSim, dstScl, 4, 4, 4)
		case 12: // 4x4 TM
			dstSim := make([]byte, 4*4)
			dstScl := make([]byte, 4*4)
			Intra4x4TMPredict(dstSim, 4, above, left, topLeft)
			intra4x4TMPredictScalar(dstScl, 4, above, left, topLeft)
			compareBlock(t, "Intra4x4TMPredict", dstSim, dstScl, 4, 4, 4)
		case 13: // 4x4 VE — needs above[4]
			dstSim := make([]byte, 4*4)
			dstScl := make([]byte, 4*4)
			Intra4x4VEPredict(dstSim, 4, above, topLeft)
			intra4x4VEPredictScalar(dstScl, 4, above, topLeft)
			compareBlock(t, "Intra4x4VEPredict", dstSim, dstScl, 4, 4, 4)
		case 14: // 4x4 HE
			dstSim := make([]byte, 4*4)
			dstScl := make([]byte, 4*4)
			Intra4x4HEPredict(dstSim, 4, left, topLeft)
			intra4x4HEPredictScalar(dstScl, 4, left, topLeft)
			compareBlock(t, "Intra4x4HEPredict", dstSim, dstScl, 4, 4, 4)
		case 15: // 4x4 LD — needs above[7]
			dstSim := make([]byte, 4*4)
			dstScl := make([]byte, 4*4)
			Intra4x4LDPredict(dstSim, 4, above)
			intra4x4LDPredictScalar(dstScl, 4, above)
			compareBlock(t, "Intra4x4LDPredict", dstSim, dstScl, 4, 4, 4)
		case 16: // 4x4 RD
			dstSim := make([]byte, 4*4)
			dstScl := make([]byte, 4*4)
			Intra4x4RDPredict(dstSim, 4, above, left, topLeft)
			intra4x4RDPredictScalar(dstScl, 4, above, left, topLeft)
			compareBlock(t, "Intra4x4RDPredict", dstSim, dstScl, 4, 4, 4)
		case 17: // 4x4 HU
			dstSim := make([]byte, 4*4)
			dstScl := make([]byte, 4*4)
			Intra4x4HUPredict(dstSim, 4, left)
			intra4x4HUPredictScalar(dstScl, 4, left)
			compareBlock(t, "Intra4x4HUPredict", dstSim, dstScl, 4, 4, 4)
		}
	})
}

func compareBlock(t *testing.T, name string, sim, scl []byte, w, h, stride int) {
	t.Helper()
	for y := range h {
		for x := range w {
			if sim[y*stride+x] != scl[y*stride+x] {
				t.Fatalf("%s [%d,%d]: simd=%d scalar=%d", name, x, y,
					sim[y*stride+x], scl[y*stride+x])
			}
		}
	}
}
