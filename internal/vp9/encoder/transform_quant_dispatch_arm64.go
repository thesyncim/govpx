//go:build arm64 && !purego

package encoder

import "unsafe"

// ARMv8 NEON dispatchers for the VP9 forward transforms and quantizer.
// Each entry point either routes to a hand-coded NEON kernel or falls
// back to the canonical scalar reference. Pending kernels are documented
// inline with pointers to the libvpx v1.16.0 source that must be ported
// verbatim and to the NEON encoder helpers (neon_encoder_test.go) that
// the port should use for instruction-word generation.

func forwardDCT4x4Dispatch(input []int16, stride int, output []int16) {
	if stride < 4 || len(input) < 3*stride+4 || len(output) < 16 {
		forwardDCT4x4Scalar(input, stride, output)
		return
	}
	forwardDCT4x4NEON(unsafe.SliceData(input), unsafe.SliceData(output), stride)
}

func forwardDCT8x8Dispatch(input []int16, stride int, output []int16) {
	if stride < 8 || len(input) < 7*stride+8 || len(output) < 64 {
		forwardDCT8x8Scalar(input, stride, output)
		return
	}
	forwardDCT8x8NEON(unsafe.SliceData(input), unsafe.SliceData(output), stride)
}

func forwardDCT16x16Dispatch(input []int16, stride int, output []int16) {
	if stride < 16 || len(input) < 15*stride+16 || len(output) < 256 {
		forwardDCT16x16Scalar(input, stride, output)
		return
	}
	forwardDCT16x16NEON(unsafe.SliceData(input), unsafe.SliceData(output), stride)
}

func forwardDCT32x32Dispatch(input []int16, stride int, output []int16) {
	// PENDING: port libvpx v1.16.0 vpx_fdct32x32_neon
	//   - kernel:  vpx_dsp/arm/fdct32x32_neon.c::vpx_fdct32x32_neon
	//   - helpers: vpx_dsp/arm/fdct32x32_neon.h (~2900 LOC of macros)
	// The largest kernel; ~1500 LOC of NEON intrinsics expanding to
	// thousands of raw NEON instructions. Best ported via a generator
	// (compute every WORD by calling the enc_* helpers) rather than by
	// hand transcription.
	forwardDCT32x32Scalar(input, stride, output)
}

func forwardDCT32x32RDDispatch(input []int16, stride int, output []int16) {
	// PENDING: port libvpx v1.16.0 vpx_fdct32x32_rd_neon
	//   - kernel:  vpx_dsp/arm/fdct32x32_neon.c::vpx_fdct32x32_rd_neon
	//   - helpers: vpx_dsp/arm/fdct32x32_neon.h (shared with vpx_fdct32x32_neon)
	// Shares the same column/row pipeline as the precision variant; the
	// row pass invokes the `1<<round` halving stage rather than the
	// (out + 1 + (out<0)) >> 2 final shift. Will land alongside the
	// precision variant in the eventual NEON port.
	forwardDCT32x32RDScalar(input, stride, output)
}

func forwardWHT4x4Dispatch(input []int16, stride int, output []int16) {
	if len(input) < 3*stride+4 || len(output) < 16 || stride < 4 {
		forwardWHT4x4Scalar(input, stride, output)
		return
	}
	forwardWHT4x4NEON(unsafe.SliceData(input), stride, unsafe.SliceData(output))
}

func quantizeFPDispatch(coeff []int16, dequant [2]int16, scan []int16, dqcoeff []int16) int {
	return QuantizeFPWithQ(coeff, dequant, scan, nil, dqcoeff)
}

func quantizeFPLibvpxDispatch(coeff []int16, nCoeffs int, roundFP, quantFP, dequant [2]int16,
	scan, iscan []int16, qcoeff, dqcoeff []int16,
) int {
	if !quantizeFPLibvpxNEONOK(coeff, nCoeffs, roundFP, quantFP, dequant,
		iscan, qcoeff, dqcoeff) {
		return quantizeFPLibvpxScalar(coeff, nCoeffs, roundFP, quantFP, dequant,
			scan, iscan, qcoeff, dqcoeff)
	}

	roundDC, roundAC := int(roundFP[0]), int(roundFP[1])
	quantDC, quantAC := int(quantFP[0]), int(quantFP[1])
	deqDC, deqAC := int(dequant[0]), int(dequant[1])

	eob := 0
	c := int(coeff[0])
	absCoeff := c
	if absCoeff < 0 {
		absCoeff = -absCoeff
	}
	sum := absCoeff + roundDC
	if sum >= deqDC {
		tmp := clampInt16(sum)
		tmp = (tmp * quantDC) >> 16
		q := tmp
		if c < 0 {
			q = -q
		}
		qcoeff[0] = int16(q)
		dqcoeff[0] = int16(q * deqDC)
		if tmp != 0 {
			eob = int(iscan[0])
		}
	} else {
		qcoeff[0] = 0
		dqcoeff[0] = 0
	}

	acCount := ((nCoeffs - 1) / 8) * 8
	if acCount > 0 {
		acEOB := int(quantizeFPACNEON(unsafe.SliceData(coeff[1:]),
			unsafe.SliceData(iscan[1:]), unsafe.SliceData(qcoeff[1:]),
			unsafe.SliceData(dqcoeff[1:]), acCount, roundAC, quantAC, deqAC))
		if acEOB > eob {
			eob = acEOB
		}
	}

	for rc := 1 + acCount; rc < nCoeffs; rc++ {
		c := int(coeff[rc])
		absCoeff := c
		if absCoeff < 0 {
			absCoeff = -absCoeff
		}
		sum := absCoeff + roundAC
		if sum < deqAC {
			qcoeff[rc] = 0
			dqcoeff[rc] = 0
			continue
		}
		tmp := clampInt16(sum)
		tmp = (tmp * quantAC) >> 16
		q := tmp
		if c < 0 {
			q = -q
		}
		qcoeff[rc] = int16(q)
		dqcoeff[rc] = int16(q * deqAC)
		if tmp != 0 && int(iscan[rc]) > eob {
			eob = int(iscan[rc])
		}
	}
	return eob
}

func quantizeFPLibvpxNEONOK(coeff []int16, nCoeffs int, roundFP, quantFP, dequant [2]int16,
	iscan []int16, qcoeff, dqcoeff []int16,
) bool {
	if nCoeffs <= 8 || len(coeff) < nCoeffs || len(iscan) < nCoeffs ||
		len(qcoeff) < nCoeffs || len(dqcoeff) < nCoeffs {
		return false
	}
	// The NEON kernel mirrors libvpx's normal positive FP quant tables.
	// Unusual externally supplied tables stay on the scalar contract path.
	return roundFP[0] >= 0 && roundFP[1] >= 0 &&
		quantFP[0] >= 0 && quantFP[1] >= 0 &&
		dequant[0] > 0 && dequant[1] > 0
}

//go:noescape
func forwardWHT4x4NEON(input *int16, stride int, output *int16)

//go:noescape
func forwardDCT8x8NEON(input *int16, output *int16, stride int)

//go:noescape
func forwardDCT4x4NEON(input *int16, output *int16, stride int)

//go:noescape
func forwardDCT16x16NEON(input *int16, output *int16, stride int)

//go:noescape
func quantizeFPACNEON(coeff *int16, iscan *int16, qcoeff *int16, dqcoeff *int16,
	count int, roundAC int, quantAC int, deqAC int) int32
