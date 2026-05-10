package encoder_test

import vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"

func setAllMacroblockEOBs(coeffs *vp8enc.MacroblockCoefficients, is4x4 bool) {
	if !is4x4 {
		coeffs.SetBlockEOB(24, vp8enc.BlockCoeffEOB(&coeffs.QCoeff[24], 0))
		for i := range 16 {
			coeffs.SetBlockEOB(i, vp8enc.BlockCoeffEOB(&coeffs.QCoeff[i], 1))
		}
	} else {
		for i := range 16 {
			coeffs.SetBlockEOB(i, vp8enc.BlockCoeffEOB(&coeffs.QCoeff[i], 0))
		}
	}
	for i := 16; i < 24; i++ {
		coeffs.SetBlockEOB(i, vp8enc.BlockCoeffEOB(&coeffs.QCoeff[i], 0))
	}
}
