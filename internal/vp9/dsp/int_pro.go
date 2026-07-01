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
	normFactor := height >> 1
	var a0, a1, a2, a3, a4, a5, a6, a7 int
	var a8, a9, a10, a11, a12, a13, a14, a15 int
	_ = hbuf[15]
	for i := range height {
		row := ref[refOff+i*refStride:]
		_ = row[15]
		a0 += int(row[0])
		a1 += int(row[1])
		a2 += int(row[2])
		a3 += int(row[3])
		a4 += int(row[4])
		a5 += int(row[5])
		a6 += int(row[6])
		a7 += int(row[7])
		a8 += int(row[8])
		a9 += int(row[9])
		a10 += int(row[10])
		a11 += int(row[11])
		a12 += int(row[12])
		a13 += int(row[13])
		a14 += int(row[14])
		a15 += int(row[15])
	}
	hbuf[0] = int16(a0 / normFactor)
	hbuf[1] = int16(a1 / normFactor)
	hbuf[2] = int16(a2 / normFactor)
	hbuf[3] = int16(a3 / normFactor)
	hbuf[4] = int16(a4 / normFactor)
	hbuf[5] = int16(a5 / normFactor)
	hbuf[6] = int16(a6 / normFactor)
	hbuf[7] = int16(a7 / normFactor)
	hbuf[8] = int16(a8 / normFactor)
	hbuf[9] = int16(a9 / normFactor)
	hbuf[10] = int16(a10 / normFactor)
	hbuf[11] = int16(a11 / normFactor)
	hbuf[12] = int16(a12 / normFactor)
	hbuf[13] = int16(a13 / normFactor)
	hbuf[14] = int16(a14 / normFactor)
	hbuf[15] = int16(a15 / normFactor)
}

// IntProRowStrips fills hbuf[16*s : 16*s+16] for each of the `strips`
// consecutive 16-column strips starting at refOff, mirroring the
// caller loop in vp9_int_pro_motion_estimation
// (vp9/encoder/vp9_mcomp.c:2317-2321):
//
//	for (idx = 0; idx < search_width; idx += 16) {
//	  vpx_int_pro_row(&hbuf[idx], ref_buf, ref_stride, bh);
//	  ref_buf += 16;
//	}
//
// On arm64 the whole batch runs in one NEON kernel
// (vpx_dsp/arm/avg_neon.c vpx_int_pro_row_neon per strip); elsewhere
// it falls back to the scalar VpxIntProRow per strip.
func IntProRowStrips(hbuf []int16, ref []uint8, refOff, refStride, height, strips int) {
	if intProRowStripsAsm(hbuf, ref, refOff, refStride, height, strips) {
		return
	}
	for s := range strips {
		VpxIntProRow(hbuf[s*16:s*16+16], ref, refOff+s*16, refStride, height)
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

// IntProCols fills vbuf[0 : rows] with the per-row column projections
// (VpxIntProCol >> normFactor), mirroring the caller loop in
// vp9_int_pro_motion_estimation (vp9/encoder/vp9_mcomp.c:2323-2327):
//
//	for (idx = 0; idx < search_height; ++idx) {
//	  vbuf[idx] = vpx_int_pro_col(ref_buf, bw) >> norm_factor;
//	  ref_buf += ref_stride;
//	}
//
// On arm64 the whole batch runs in one NEON kernel
// (vpx_dsp/arm/avg_neon.c vpx_int_pro_col_neon per row); elsewhere it
// falls back to the scalar VpxIntProCol per row.
func IntProCols(vbuf []int16, ref []uint8, refOff, refStride, width, rows, normFactor int) {
	if intProColsAsm(vbuf, ref, refOff, refStride, width, rows, normFactor) {
		return
	}
	for idx := range rows {
		vbuf[idx] = VpxIntProCol(ref, refOff+idx*refStride, width) >> uint(normFactor)
	}
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
