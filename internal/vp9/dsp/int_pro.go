package dsp

// VP9 integer-projection DSP kernels. Ported from libvpx v1.16.0
// vpx_dsp/avg.c:
//   - vpx_int_pro_row_c   (vpx_dsp/avg.c:345-360)
//   - vpx_int_pro_col_c   (vpx_dsp/avg.c:362-369)
//   - vpx_vector_var_c    (vpx_dsp/avg.c:371-388)
//
// These three helpers underpin vp9_int_pro_motion_estimation
// (vp9/encoder/vp9_mcomp.c:2264) — the integer-projection (1-D)
// motion-search used by the realtime / ML_BASED_PARTITION path before
// committing to subpel refinement.
//
// All three are pure functions over the source / reference buffers; the
// govpx callers feed them with the same byte / int16 storage classes
// libvpx does (per-frame uint8 frame plane, per-call int16 scratch).

// VpxIntProRow mirrors vpx_int_pro_row_c (vpx_dsp/avg.c:345-360).
// Projects |height| rows of |ref| onto a 16-element horizontal vector,
// dividing each accumulator by (height/2) to normalise the dynamic
// range from 14 bits down to 9 bits. |hbuf| must have room for at
// least 16 int16 entries. |refOff| is the starting offset into |ref|;
// the inner loop strides through |ref[refOff + i*refStride + idx]|.
//
// Dynamic range (per libvpx commentary):
//   - hbuf[idx] pre-divide: [0, 16320] (14 bits, height=64 max).
//   - hbuf[idx] post-divide: [0, 510]  (9 bits).
func VpxIntProRow(hbuf []int16, ref []uint8, refOff, refStride, height int) {
	// libvpx: assert(height >= 2).
	var s0, s1, s2, s3 int
	var s4, s5, s6, s7 int
	var s8, s9, s10, s11 int
	var s12, s13, s14, s15 int
	for i := range height {
		row := refOff + i*refStride
		s0 += int(ref[row+0])
		s1 += int(ref[row+1])
		s2 += int(ref[row+2])
		s3 += int(ref[row+3])
		s4 += int(ref[row+4])
		s5 += int(ref[row+5])
		s6 += int(ref[row+6])
		s7 += int(ref[row+7])
		s8 += int(ref[row+8])
		s9 += int(ref[row+9])
		s10 += int(ref[row+10])
		s11 += int(ref[row+11])
		s12 += int(ref[row+12])
		s13 += int(ref[row+13])
		s14 += int(ref[row+14])
		s15 += int(ref[row+15])
	}
	normFactor := height >> 1
	hbuf[0] = int16(s0 / normFactor)
	hbuf[1] = int16(s1 / normFactor)
	hbuf[2] = int16(s2 / normFactor)
	hbuf[3] = int16(s3 / normFactor)
	hbuf[4] = int16(s4 / normFactor)
	hbuf[5] = int16(s5 / normFactor)
	hbuf[6] = int16(s6 / normFactor)
	hbuf[7] = int16(s7 / normFactor)
	hbuf[8] = int16(s8 / normFactor)
	hbuf[9] = int16(s9 / normFactor)
	hbuf[10] = int16(s10 / normFactor)
	hbuf[11] = int16(s11 / normFactor)
	hbuf[12] = int16(s12 / normFactor)
	hbuf[13] = int16(s13 / normFactor)
	hbuf[14] = int16(s14 / normFactor)
	hbuf[15] = int16(s15 / normFactor)
}

// VpxIntProCol mirrors vpx_int_pro_col_c (vpx_dsp/avg.c:362-369).
// Sums |width| consecutive bytes of |ref| starting at |refOff| and
// returns the 14-bit accumulator as int16. The encoder normalises this
// further by `>> norm_factor` at the call site (3 + bw/32), bringing
// the dynamic range into [0, 510].
func VpxIntProCol(ref []uint8, refOff, width int) int16 {
	row := ref[refOff:]
	switch width {
	case 16:
		return int16(intProSum16(row))
	case 32:
		return int16(intProSum16(row) + intProSum16(row[16:]))
	case 64:
		return int16(intProSum16(row) + intProSum16(row[16:]) +
			intProSum16(row[32:]) + intProSum16(row[48:]))
	}
	var sum int16
	for idx := range width {
		sum += int16(ref[refOff+idx])
	}
	return sum
}

func intProSum16(row []uint8) int {
	return int(row[0]) + int(row[1]) + int(row[2]) + int(row[3]) +
		int(row[4]) + int(row[5]) + int(row[6]) + int(row[7]) +
		int(row[8]) + int(row[9]) + int(row[10]) + int(row[11]) +
		int(row[12]) + int(row[13]) + int(row[14]) + int(row[15])
}

// VpxVectorVar mirrors vpx_vector_var_c (vpx_dsp/avg.c:371-388). Given
// two |width| = (4 << bwl) vectors (ref and src) each with entries in
// [0, 510], it returns the bias-adjusted variance:
//
//	var = sse - ((mean * mean) >> (bwl + 2))
//
// where mean and sse are accumulated over the per-element differences.
// bwl is the block-width-log2 in 4-pel units: 2 → width 16, 3 → 32,
// 4 → 64.
func VpxVectorVar(ref, src []int16, bwl int) int {
	width := 4 << bwl
	switch width {
	case 16:
		sse, mean := intProVectorStats16(ref, src)
		return sse - ((mean * mean) >> (bwl + 2))
	case 32:
		sse0, mean0 := intProVectorStats16(ref, src)
		sse1, mean1 := intProVectorStats16(ref[16:], src[16:])
		sse := sse0 + sse1
		mean := mean0 + mean1
		return sse - ((mean * mean) >> (bwl + 2))
	case 64:
		sse0, mean0 := intProVectorStats16(ref, src)
		sse1, mean1 := intProVectorStats16(ref[16:], src[16:])
		sse2, mean2 := intProVectorStats16(ref[32:], src[32:])
		sse3, mean3 := intProVectorStats16(ref[48:], src[48:])
		sse := sse0 + sse1 + sse2 + sse3
		mean := mean0 + mean1 + mean2 + mean3
		return sse - ((mean * mean) >> (bwl + 2))
	}
	var sse, mean int
	for i := range width {
		diff := int(ref[i]) - int(src[i])
		mean += diff
		sse += diff * diff
	}
	return sse - ((mean * mean) >> (bwl + 2))
}

func intProVectorStats16(ref, src []int16) (sse, mean int) {
	d0 := int(ref[0]) - int(src[0])
	d1 := int(ref[1]) - int(src[1])
	d2 := int(ref[2]) - int(src[2])
	d3 := int(ref[3]) - int(src[3])
	d4 := int(ref[4]) - int(src[4])
	d5 := int(ref[5]) - int(src[5])
	d6 := int(ref[6]) - int(src[6])
	d7 := int(ref[7]) - int(src[7])
	d8 := int(ref[8]) - int(src[8])
	d9 := int(ref[9]) - int(src[9])
	d10 := int(ref[10]) - int(src[10])
	d11 := int(ref[11]) - int(src[11])
	d12 := int(ref[12]) - int(src[12])
	d13 := int(ref[13]) - int(src[13])
	d14 := int(ref[14]) - int(src[14])
	d15 := int(ref[15]) - int(src[15])
	mean = d0 + d1 + d2 + d3 + d4 + d5 + d6 + d7 +
		d8 + d9 + d10 + d11 + d12 + d13 + d14 + d15
	sse = d0*d0 + d1*d1 + d2*d2 + d3*d3 +
		d4*d4 + d5*d5 + d6*d6 + d7*d7 +
		d8*d8 + d9*d9 + d10*d10 + d11*d11 +
		d12*d12 + d13*d13 + d14*d14 + d15*d15
	return sse, mean
}
