package govpx

var libvpxKeyFrameBoostQAdjustment = [128]int{
	128, 129, 130, 131, 132, 133, 134, 135,
	136, 137, 138, 139, 140, 141, 142, 143,
	144, 145, 146, 147, 148, 149, 150, 151,
	152, 153, 154, 155, 156, 157, 158, 159,
	160, 161, 162, 163, 164, 165, 166, 167,
	168, 169, 170, 171, 172, 173, 174, 175,
	176, 177, 178, 179, 180, 181, 182, 183,
	184, 185, 186, 187, 188, 189, 190, 191,
	192, 193, 194, 195, 196, 197, 198, 199,
	200, 200, 201, 201, 202, 203, 203, 203,
	204, 204, 205, 205, 206, 206, 207, 207,
	208, 208, 209, 209, 210, 210, 211, 211,
	212, 212, 213, 213, 214, 214, 215, 215,
	216, 216, 217, 217, 218, 218, 219, 219,
	220, 220, 220, 220, 220, 220, 220, 220,
	220, 220, 220, 220, 220, 220, 220, 220,
}

// libvpxKeyFrameHighMotionMinQ, libvpxGoldenFrameHighMotionMinQ, and
// libvpxInterMinQ port the one-pass conservative active-min-Q tables from
// vp8/encoder/onyx_if.c. The matching low- and mid-motion tables
// (libvpxKeyFrameLowMotionMinQ, libvpxGoldenFrameLowMotionMinQ,
// libvpxGoldenFrameMidMotionMinQ) are libvpx's two-pass alternates for the
// same QINDEX_RANGE; one-pass `vp8_regulate_q` always selects the
// conservative high-motion variant. They are ported here so that ARF/GF
// oracle traces can be cross-checked against libvpx without re-reading the C
// source, and so future two-pass work has the libvpx-faithful tables already
// available.
var libvpxKeyFrameHighMotionMinQ = [128]int{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3,
	3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 5, 5, 6, 6,
	6, 6, 7, 7, 8, 8, 8, 8, 9, 9, 10, 10, 10, 10, 11, 11,
	11, 11, 12, 12, 13, 13, 13, 13, 14, 14, 15, 15, 15, 15, 16, 16,
	16, 16, 17, 17, 18, 18, 18, 18, 19, 19, 20, 20, 20, 20, 21, 21,
	21, 21, 22, 22, 23, 23, 24, 25, 25, 26, 26, 27, 28, 28, 29, 30,
}

var libvpxGoldenFrameHighMotionMinQ = [128]int{
	0, 0, 0, 0, 1, 1, 1, 1, 1, 2, 2, 2, 3, 3, 3, 4,
	4, 4, 5, 5, 5, 6, 6, 6, 7, 7, 7, 8, 8, 8, 9, 9,
	9, 10, 10, 10, 11, 11, 12, 12, 13, 13, 14, 14, 15, 15, 16, 16,
	17, 17, 18, 18, 19, 19, 20, 20, 21, 21, 22, 22, 23, 23, 24, 24,
	25, 25, 26, 26, 27, 27, 28, 28, 29, 29, 30, 30, 31, 31, 32, 32,
	33, 33, 34, 34, 35, 35, 36, 36, 37, 37, 38, 38, 39, 39, 40, 40,
	41, 41, 42, 42, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54,
	55, 56, 57, 58, 59, 60, 62, 64, 66, 68, 70, 72, 74, 76, 78, 80,
}

var libvpxInterMinQ = [128]int{
	0, 0, 1, 1, 2, 3, 3, 4, 4, 5, 6, 6, 7, 8, 8, 9,
	9, 10, 11, 11, 12, 13, 13, 14, 15, 15, 16, 17, 17, 18, 19, 20,
	20, 21, 22, 22, 23, 24, 24, 25, 26, 27, 27, 28, 29, 30, 30, 31,
	32, 33, 33, 34, 35, 36, 36, 37, 38, 39, 39, 40, 41, 42, 42, 43,
	44, 45, 46, 46, 47, 48, 49, 50, 50, 51, 52, 53, 54, 55, 55, 56,
	57, 58, 59, 60, 60, 61, 62, 63, 64, 65, 66, 67, 67, 68, 69, 70,
	71, 72, 73, 74, 75, 75, 76, 77, 78, 79, 80, 81, 82, 83, 84, 85,
	86, 86, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 97, 98, 99, 100,
}

// libvpxKeyFrameLowMotionMinQ ports kf_low_motion_minq from
// vp8/encoder/onyx_if.c. libvpx selects this two-pass variant when
// `cpi->gfu_boost > 600` for a key frame.
var libvpxKeyFrameLowMotionMinQ = [128]int{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2,
	3, 3, 3, 3, 3, 3, 4, 4, 4, 5, 5, 5, 5, 5, 6, 6,
	6, 6, 7, 7, 8, 8, 8, 8, 9, 9, 10, 10, 10, 10, 11, 11,
	11, 11, 12, 12, 13, 13, 13, 13, 14, 14, 15, 15, 15, 15, 16, 16,
	16, 16, 17, 17, 18, 18, 18, 18, 19, 20, 20, 21, 21, 22, 23, 23,
}

// libvpxGoldenFrameLowMotionMinQ ports gf_low_motion_minq from
// vp8/encoder/onyx_if.c. libvpx selects this two-pass variant when
// `cpi->gfu_boost > 1000` for a GF/ARF refresh.
var libvpxGoldenFrameLowMotionMinQ = [128]int{
	0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2,
	3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 6, 6, 6, 6,
	7, 7, 7, 7, 8, 8, 8, 8, 9, 9, 9, 9, 10, 10, 10, 10,
	11, 11, 12, 12, 13, 13, 14, 14, 15, 15, 16, 16, 17, 17, 18, 18,
	19, 19, 20, 20, 21, 21, 22, 22, 23, 23, 24, 24, 25, 25, 26, 26,
	27, 27, 28, 28, 29, 29, 30, 30, 31, 31, 32, 32, 33, 33, 34, 34,
	35, 35, 36, 36, 37, 37, 38, 38, 39, 39, 40, 40, 41, 41, 42, 42,
	43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58,
}

// libvpxGoldenFrameMidMotionMinQ ports gf_mid_motion_minq from
// vp8/encoder/onyx_if.c. libvpx selects this two-pass variant for a GF/ARF
// refresh when `cpi->gfu_boost` falls between the high-motion (<400) and
// low-motion (>1000) cutoffs.
var libvpxGoldenFrameMidMotionMinQ = [128]int{
	0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 2, 2, 3, 3, 3, 4,
	4, 4, 5, 5, 5, 6, 6, 6, 7, 7, 7, 8, 8, 8, 9, 9,
	9, 10, 10, 10, 10, 11, 11, 11, 12, 12, 12, 12, 13, 13, 13, 14,
	14, 14, 15, 15, 16, 16, 17, 17, 18, 18, 19, 19, 20, 20, 21, 21,
	22, 22, 23, 23, 24, 24, 25, 25, 26, 26, 27, 27, 28, 28, 29, 29,
	30, 30, 31, 31, 32, 32, 33, 33, 34, 34, 35, 35, 36, 36, 37, 37,
	38, 39, 39, 40, 40, 41, 41, 42, 42, 43, 43, 44, 45, 46, 47, 48,
	49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61, 62, 63, 64,
}

// libvpxBitsPerMB ports vp8/encoder/ratectrl.c vp8_bits_per_mb. Values are
// bits per macroblock multiplied by 1<<libvpxBPerMBNormBits.
var libvpxBitsPerMB = [2][128]int{
	{
		1125000, 900000, 750000, 642857, 562500, 500000, 450000, 450000,
		409090, 375000, 346153, 321428, 300000, 281250, 264705, 264705,
		250000, 236842, 225000, 225000, 214285, 214285, 204545, 204545,
		195652, 195652, 187500, 180000, 180000, 173076, 166666, 160714,
		155172, 150000, 145161, 140625, 136363, 132352, 128571, 125000,
		121621, 121621, 118421, 115384, 112500, 109756, 107142, 104651,
		102272, 100000, 97826, 97826, 95744, 93750, 91836, 90000,
		88235, 86538, 84905, 83333, 81818, 80357, 78947, 77586,
		76271, 75000, 73770, 72580, 71428, 70312, 69230, 68181,
		67164, 66176, 65217, 64285, 63380, 62500, 61643, 60810,
		60000, 59210, 59210, 58441, 57692, 56962, 56250, 55555,
		54878, 54216, 53571, 52941, 52325, 51724, 51136, 50561,
		49450, 48387, 47368, 46875, 45918, 45000, 44554, 44117,
		43269, 42452, 41666, 40909, 40178, 39473, 38793, 38135,
		36885, 36290, 35714, 35156, 34615, 34090, 33582, 33088,
		32608, 32142, 31468, 31034, 30405, 29801, 29220, 28662,
	},
	{
		712500, 570000, 475000, 407142, 356250, 316666, 285000, 259090,
		237500, 219230, 203571, 190000, 178125, 167647, 158333, 150000,
		142500, 135714, 129545, 123913, 118750, 114000, 109615, 105555,
		101785, 98275, 95000, 91935, 89062, 86363, 83823, 81428,
		79166, 77027, 75000, 73076, 71250, 69512, 67857, 66279,
		64772, 63333, 61956, 60638, 59375, 58163, 57000, 55882,
		54807, 53773, 52777, 51818, 50892, 50000, 49137, 47500,
		45967, 44531, 43181, 41911, 40714, 39583, 38513, 37500,
		36538, 35625, 34756, 33928, 33139, 32386, 31666, 30978,
		30319, 29687, 29081, 28500, 27941, 27403, 26886, 26388,
		25909, 25446, 25000, 24568, 23949, 23360, 22800, 22265,
		21755, 21268, 20802, 20357, 19930, 19520, 19127, 18750,
		18387, 18037, 17701, 17378, 17065, 16764, 16473, 16101,
		15745, 15405, 15079, 14766, 14467, 14179, 13902, 13636,
		13380, 13133, 12895, 12666, 12445, 12179, 11924, 11632,
		11445, 11220, 11003, 10795, 10594, 10401, 10215, 10035,
	},
}

func libvpxRegulatedQuantizer(keyFrame bool, targetBitsPerFrame int, macroblocks int, minQ int, maxQ int, correctionFactor float64) int {
	q, _ := libvpxRegulatedQuantizerWithZbin(keyFrame, false, targetBitsPerFrame, macroblocks, minQ, maxQ, correctionFactor)
	return q
}

func libvpxRegulatedQuantizerWithZbin(keyFrame bool, goldenFrame bool, targetBitsPerFrame int, macroblocks int, minQ int, maxQ int, correctionFactor float64) (int, int) {
	return libvpxRegulatedQuantizerWithZbinAltRef(keyFrame, goldenFrame, false, targetBitsPerFrame, macroblocks, minQ, maxQ, correctionFactor)
}

// libvpxRegulatedQuantizerWithZbinAltRef extends
// libvpxRegulatedQuantizerWithZbin with an ARF-refresh flag so the
// `zbin_oq_high` cap matches libvpx's `cm->refresh_alt_ref_frame` branch in
// `onyx_if.c:3760-3766`. ARF refresh shares the GF cap of 16; the regulation
// loop itself is unchanged.
func libvpxRegulatedQuantizerWithZbinAltRef(keyFrame bool, goldenFrame bool, altRefFrame bool, targetBitsPerFrame int, macroblocks int, minQ int, maxQ int, correctionFactor float64) (int, int) {
	if macroblocks <= 0 || targetBitsPerFrame <= 0 {
		return clampQuantizerValue(minQ, minQ, maxQ), 0
	}
	if correctionFactor <= 0 {
		correctionFactor = 1.0
	}
	targetBitsPerMB := libvpxTargetBitsPerMB(targetBitsPerFrame, macroblocks)
	frameType := 1
	if keyFrame {
		frameType = 0
	}
	q := maxQ
	lastError := libvpxIntMax
	bitsAtSelectedQ := 0
	for i := minQ; i <= maxQ && i < len(libvpxBitsPerMB[frameType]); i++ {
		bitsAtQ := int(0.5 + correctionFactor*float64(libvpxBitsPerMB[frameType][i]))
		bitsAtSelectedQ = bitsAtQ
		if bitsAtQ <= targetBitsPerMB {
			if targetBitsPerMB-bitsAtQ <= lastError {
				q = i
			} else {
				q = i - 1
			}
			break
		}
		lastError = bitsAtQ - targetBitsPerMB
	}
	q = clampQuantizerValue(q, minQ, maxQ)
	zbinOverQuant := 0
	if q >= vp8MaxQIndex {
		zbinOverQuant = libvpxZbinOverQuantForTargetAltRef(keyFrame, goldenFrame, altRefFrame, bitsAtSelectedQ, targetBitsPerMB)
	}
	return q, zbinOverQuant
}

func libvpxTargetBitsPerMB(targetBitsPerFrame int, macroblocks int) int {
	if targetBitsPerFrame > libvpxIntMax>>libvpxBPerMBNormBits {
		temp := targetBitsPerFrame / macroblocks
		if temp > libvpxIntMax>>libvpxBPerMBNormBits {
			return libvpxIntMax
		}
		return temp << libvpxBPerMBNormBits
	}
	return (targetBitsPerFrame << libvpxBPerMBNormBits) / macroblocks
}

// libvpxZbinOverQuantForTargetAltRef walks libvpx's iterative
// `vp8/encoder/onyx_if.c` zbin-over-quant adjustment loop
// (`while(zbin_oq < zbin_oq_max && bits_at_q > target_bits_per_mb)`) with
// an ARF-refresh flag. The 0.99-walk-toward-0.999 scaling loop is the
// same regardless of frame kind; only the `zbin_oq_high` cap differs
// (see libvpxZbinOverQuantHighAltRef).
func libvpxZbinOverQuantForTargetAltRef(keyFrame bool, goldenFrame bool, altRefFrame bool, bitsAtQ int, targetBitsPerMB int) int {
	zbinOQMax := libvpxZbinOverQuantHighAltRef(keyFrame, goldenFrame, altRefFrame)
	if zbinOQMax <= 0 || bitsAtQ <= 0 {
		return 0
	}
	zbinOverQuant := 0
	factor := 0.99
	factorAdjustment := 0.01 / 256.0
	for zbinOverQuant < zbinOQMax {
		zbinOverQuant++
		if zbinOverQuant > zbinOQMax {
			zbinOverQuant = zbinOQMax
		}
		bitsAtQ = int(factor * float64(bitsAtQ))
		factor += factorAdjustment
		if factor >= 0.999 {
			factor = 0.999
		}
		if bitsAtQ <= targetBitsPerMB {
			break
		}
	}
	return zbinOverQuant
}

// libvpxZbinOverQuantHighAltRef ports the libvpx
// `vp8/encoder/onyx_if.c:3758-3766` zbin_oq_high cap, including the ARF
// refresh branch:
//
//	if (cm->frame_type == KEY_FRAME)                  zbin_oq_high = 0;
//	else if (number_of_layers == 1 &&
//	         (cm->refresh_alt_ref_frame ||
//	          (cm->refresh_golden_frame && !source_alt_ref_active)))
//	                                                 zbin_oq_high = 16;
//	else                                              zbin_oq_high = ZBIN_OQ_MAX;
//
// govpx does not yet model `source_alt_ref_active`; for an explicit ARF
// refresh (altRefFrame=true) the cap is 16, matching libvpx.
func libvpxZbinOverQuantHighAltRef(keyFrame bool, goldenFrame bool, altRefFrame bool) int {
	if keyFrame {
		return 0
	}
	if altRefFrame || goldenFrame {
		return 16
	}
	return libvpxZbinOverQuantMax
}

func libvpxEstimatedBitsAtQuantizer(frameType int, q int, macroblocks int, correctionFactor float64) int {
	if frameType < 0 || frameType >= len(libvpxBitsPerMB) || q < 0 || q >= len(libvpxBitsPerMB[frameType]) || macroblocks <= 0 {
		return 0
	}
	if correctionFactor <= 0 {
		correctionFactor = 1.0
	}
	bitsPerMB := int(0.5 + correctionFactor*float64(libvpxBitsPerMB[frameType][q]))
	if macroblocks > 1<<11 {
		return (bitsPerMB >> libvpxBPerMBNormBits) * macroblocks
	}
	return (bitsPerMB * macroblocks) >> libvpxBPerMBNormBits
}

// libvpxEstimatedBitsAtQuantizerWithZbin mirrors the post-encode projection in
// libvpx's vp8_update_rate_correction_factors (vp8/encoder/ratectrl.c): when
// zbin_over_quant > 0, project the frame size at this Q and then iteratively
// scale it down by a starting factor of 0.99 that walks toward 0.999 over
// `zbinOverQuant` steps. Without this scaling, frames encoded with non-zero
// zbin_oq look much larger than expected, the rate correction factor is
// damped toward 1.0, and the next frame's regulated Q is set too low.
func libvpxEstimatedBitsAtQuantizerWithZbin(frameType int, q int, macroblocks int, correctionFactor float64, zbinOverQuant int) int {
	bits := libvpxEstimatedBitsAtQuantizer(frameType, q, macroblocks, correctionFactor)
	if bits <= 0 || zbinOverQuant <= 0 {
		return bits
	}
	factor := 0.99
	const factorAdjustment = 0.01 / 256.0
	for z := zbinOverQuant; z > 0; z-- {
		bits = int(factor * float64(bits))
		factor += factorAdjustment
		if factor >= 0.999 {
			factor = 0.999
		}
		if bits <= 0 {
			return 0
		}
	}
	return bits
}

func clampQuantizerValue(q int, minQ int, maxQ int) int {
	if q < minQ {
		return minQ
	}
	if q > maxQ {
		return maxQ
	}
	return q
}

// libvpxGFBoostQAdjustment ports vp8_gf_boost_qadjustment from
// vp8/encoder/ratectrl.c. It is the GFQ_ADJUSTMENT lookup that seeds the
