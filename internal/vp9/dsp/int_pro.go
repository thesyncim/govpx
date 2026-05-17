package dsp

// VP9 integer-projection DSP kernels. Ported verbatim from libvpx v1.16.0
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
	normFactor := int16(height >> 1)
	for idx := range 16 {
		var acc int16
		for i := range height {
			acc += int16(ref[refOff+i*refStride])
		}
		hbuf[idx] = acc / normFactor
		refOff++
	}
}

// VpxIntProCol mirrors vpx_int_pro_col_c (vpx_dsp/avg.c:362-369).
// Sums |width| consecutive bytes of |ref| starting at |refOff| and
// returns the 14-bit accumulator as int16. The encoder normalises this
// further by `>> norm_factor` at the call site (3 + bw/32), bringing
// the dynamic range into [0, 510].
func VpxIntProCol(ref []uint8, refOff, width int) int16 {
	var sum int16
	for idx := range width {
		sum += int16(ref[refOff+idx])
	}
	return sum
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
	var sse, mean int
	for i := range width {
		diff := int(ref[i]) - int(src[i])
		mean += diff
		sse += diff * diff
	}
	return sse - ((mean * mean) >> (bwl + 2))
}
