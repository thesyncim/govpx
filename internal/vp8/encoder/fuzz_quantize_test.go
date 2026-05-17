package encoder

import (
	"encoding/binary"
	"testing"
)

// FuzzVP8DSPQuantize is a differential SIMD-vs-scalar fuzz harness for
// the VP8 fast-quantize family. Mirrors libvpx test/quantize_test.cc.
//
// govpx currently only ships vp8_fast_quantize_b (regular path is not
// yet ported). When the regular kernel lands this can be extended via
// op selector. For now we exercise:
//
//	0  FastQuantizeBlock      -> fastQuantizeBlockSIMD vs fastQuantizeBlockScalar
//	                              (qcoeff, dqcoeff, eob byte-equal)
//
// The fuzz callback builds coeff[16] from the payload (clamped to the
// nominal post-DCT coefficient range |v|<=2048), a per-block dequant
// table from the payload (each lane in [4,255] like real VP8 dequants),
// then asserts byte-identical qcoeff/dqcoeff/eob across the two impls.

func FuzzVP8DSPQuantize(f *testing.F) {
	seeds := [][]byte{
		make([]byte, 128),
		bytes255(128),
		bytesAlt(128),
		bytesRamp(128, 0),
		bytesRamp(128, 11),
		bytesPattern(128, 0x37, 0x6B),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Need 16 int16 coeffs (32 bytes) + 16 int16 dequants (32 bytes) = 64.
		if len(data) < 64 {
			return
		}

		var coeff [16]int16
		for i := range 16 {
			v := int16(binary.LittleEndian.Uint16(data[i*2:]))
			if v > 2048 {
				v = 2048
			} else if v < -2048 {
				v = -2048
			}
			coeff[i] = v
		}

		var dequant [16]int16
		for i := range 16 {
			// Map fuzz byte into [4, 255] — matches real VP8 dequant
			// table range (quant_simd_test.go:75 uses 4..256).
			b := data[32+i*2]
			d := max(int16(b), 4)
			dequant[i] = d
		}

		var quant BlockQuant
		InitFastBlockQuant(&dequant, &quant)

		var qSim, dqSim, qScalar, dqScalar [16]int16
		eobSim := fastQuantizeBlockSIMD(&coeff, &quant, &qSim, &dqSim)
		eobScl := fastQuantizeBlockScalar(&coeff, &quant, &qScalar, &dqScalar)

		if eobSim != eobScl {
			t.Fatalf("eob simd=%d scalar=%d coeff=%v dq=%v", eobSim, eobScl, coeff, dequant)
		}
		if qSim != qScalar {
			t.Fatalf("qcoeff simd=%v scalar=%v coeff=%v dq=%v", qSim, qScalar, coeff, dequant)
		}
		if dqSim != dqScalar {
			t.Fatalf("dqcoeff simd=%v scalar=%v coeff=%v dq=%v", dqSim, dqScalar, coeff, dequant)
		}
	})
}
