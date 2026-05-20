package govpx

import (
	"testing"

	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// Task #252 — deep audit of vp8_temporal_filter_iterate_c outer-loop
// plumbing. Complements the per-component pins in
// vp8_arnr_temporal_filter_parity_test.go (apply, window, fixed_divide)
// with a byte-exact whole-iterate assertion against a libvpx-verbatim
// transcription of vp8/encoder/temporal_filter.c:188-346.
//
// Scope:
//   - mb_row / mb_col outer loop ordering (temporal_filter.c:210-227).
//   - Per-MB accumulator/count zero-init (memset 384 entries,
//     temporal_filter.c:231-232).
//   - frame == alt_ref_index filter_weight=2 fast path
//     (temporal_filter.c:245-246).
//   - Per-frame Y → U → V apply ordering (temporal_filter.c:274-284).
//   - Y plane normalization scan order
//     (temporal_filter.c:289-305) and UV interleaved normalization
//     (temporal_filter.c:307-332).
//   - Per-pixel libvpx integer formula
//     `pval = (accumulator + count>>1) * fixed_divide[count] >> 19`.
//
// Out of scope (covered by oracle_arnr_buffer_test.go scoreboard):
//   - vp8_hex_search variance + mv_err_cost path; govpx uses pure SAD.
//   - vp8_find_best_sub_pixel_step_iteratively half/quarter sequence
//     with vfp->svf; govpx uses sixtap-SAD diamond walk.
//
// To isolate iterate plumbing from MC, the fixture uses uniform
// adjacent-frame planes so every candidate MV yields the same 16x16
// SAD. libvpx's vp8_hex_search starts at (0,0); no neighbour is
// strictly better; MV stays at (0,0). govpx's arnrFindMatchingMB +
// arnrSubpelRefine likewise halt at (0,0).
//
// Three strengths (1, 3, 5) bracket the rounding-bit spectrum (1<<0,
// 1<<2, 1<<4) and span the libvpx-validated range. strength > 6 is
// rejected by vp8/vp8_cx_iface.c validate_config.
//
// Per the task brief these are the "3 ARNR-armed seeds (different
// arnr strength settings) asserting byte parity through the alt-ref-
// emit frame".

// vp8encSourceImageForARNRIteration constructs the minimal vp8enc.SourceImage
// the ARNR iterate path consumes. iterateTemporalFilter only reads
// Width/Height from this struct; the other fields are populated to keep
// the helper reusable.
func vp8encSourceImageForARNRIteration(width, height int, y, u, v []byte) vp8enc.SourceImage {
	return vp8enc.SourceImage{
		Y:        y,
		U:        u,
		V:        v,
		Width:    width,
		Height:   height,
		UVWidth:  (width + 1) >> 1,
		UVHeight: (height + 1) >> 1,
		YStride:  width,
		UStride:  (width + 1) >> 1,
		VStride:  (width + 1) >> 1,
	}
}

// libvpxIterateReferenceARNR is a byte-for-byte transcription of
// libvpx vp8_temporal_filter_iterate_c (vp8/encoder/temporal_filter.c
// :188-346). It uses govpx's primitive predictor (arnrPredictLuma16x16,
// arnrPredictChroma8x8) and apply (applyTemporalFilter) which are
// themselves verbatim ports pinned by TestVP8TemporalFilterApplyByteExact.
// The remaining surface — outer loop ordering, accumulator zero-init,
// filter_weight selection, apply call order, and the writeARNRBlock
// normalization math — is what this reference pins.
func libvpxIterateReferenceARNR(t *testing.T, dst *arnrFrameView, refs []arnrFrameView, centerIdx int, strength int) {
	t.Helper()
	mbCols := (dst.width + 15) >> 4
	mbRows := (dst.height + 15) >> 4
	for mbRow := range mbRows {
		mbY := mbRow * 16
		for mbCol := range mbCols {
			mbX := mbCol * 16
			mbUVX := mbX >> 1
			mbUVY := mbY >> 1
			uvW := (dst.width + 1) >> 1
			uvH := (dst.height + 1) >> 1

			// libvpx temporal_filter.c:231-232.
			var accumulator [384]uint32
			var count [384]uint32

			var srcY [256]byte
			gatherBlock(srcY[:], 16, dst.y, dst.yStride, mbX, mbY, dst.width, dst.height, 16)
			var srcU, srcV [64]byte
			gatherBlock(srcU[:], 8, dst.u, dst.uStride, mbUVX, mbUVY, uvW, uvH, 8)
			gatherBlock(srcV[:], 8, dst.v, dst.vStride, mbUVX, mbUVY, uvW, uvH, 8)

			// libvpx temporal_filter.c:239-286. Per-frame filter.
			for fi, ref := range refs {
				filterWeight := 0
				switch {
				case fi == centerIdx:
					filterWeight = 2
				default:
					// MC is forced to MV=(0,0) by the uniform-adjacent
					// fixture. Compute the zero-MV 16x16 SAD directly
					// so this reference is self-contained.
					sad := 0
					for j := range 16 {
						for i := range 16 {
							src := int(srcY[j*16+i])
							pred := int(ref.y[(mbY+j)*ref.yStride+(mbX+i)])
							if d := src - pred; d >= 0 {
								sad += d
							} else {
								sad -= d
							}
						}
					}
					switch {
					case sad < arnrThreshLow:
						filterWeight = 2
					case sad < arnrThreshHigh:
						filterWeight = 1
					default:
						filterWeight = 0
					}
				}
				if filterWeight == 0 {
					continue
				}

				// libvpx temporal_filter.c:265-271. Predictor build at
				// MV=(0,0) collapses to a copy.
				var predY [256]byte
				arnrPredictLuma16x16(predY[:], 16, ref, mbX, mbY, 0, 0)
				var predU, predV [64]byte
				arnrPredictChroma8x8(predU[:], 8, ref.u, ref.uStride, (ref.width+1)>>1, (ref.height+1)>>1, mbUVX, mbUVY, 0, 0)
				arnrPredictChroma8x8(predV[:], 8, ref.v, ref.vStride, (ref.width+1)>>1, (ref.height+1)>>1, mbUVX, mbUVY, 0, 0)

				// libvpx temporal_filter.c:274-284. Y → U → V apply.
				applyTemporalFilter(srcY[:], 16, predY[:], 16, strength, filterWeight, accumulator[:256:256], count[:256:256])
				applyTemporalFilter(srcU[:], 8, predU[:], 8, strength, filterWeight, accumulator[256:320:320], count[256:320:320])
				applyTemporalFilter(srcV[:], 8, predV[:], 8, strength, filterWeight, accumulator[320:384:384], count[320:384:384])
			}

			// libvpx temporal_filter.c:289-305. Y plane normalize.
			for j := range 16 {
				yy := mbY + j
				if yy < 0 || yy >= dst.height {
					continue
				}
				for i := range 16 {
					xx := mbX + i
					if xx < 0 || xx >= dst.width {
						continue
					}
					k := j*16 + i
					c := count[k]
					if c == 0 {
						continue
					}
					pval := min((accumulator[k]+c>>1)*arnrFixedDivide[c]>>19, 255)
					dst.y[yy*dst.yStride+xx] = byte(pval)
				}
			}
			// libvpx temporal_filter.c:307-332. UV interleaved normalize.
			for j := range 8 {
				yy := mbUVY + j
				if yy < 0 || yy >= (dst.height+1)>>1 {
					continue
				}
				for i := range 8 {
					xx := mbUVX + i
					if xx < 0 || xx >= (dst.width+1)>>1 {
						continue
					}
					ku := 256 + j*8 + i
					kv := ku + 64
					if cu := count[ku]; cu != 0 {
						pval := min((accumulator[ku]+cu>>1)*arnrFixedDivide[cu]>>19, 255)
						dst.u[yy*dst.uStride+xx] = byte(pval)
					}
					if cv := count[kv]; cv != 0 {
						pval := min((accumulator[kv]+cv>>1)*arnrFixedDivide[cv]>>19, 255)
						dst.v[yy*dst.vStride+xx] = byte(pval)
					}
				}
			}
		}
	}
}

// TestVP8ARNRIterateByteExact pins the VP8 ARNR iterate plumbing
// against a libvpx-verbatim transcription of
// vp8/encoder/temporal_filter.c:188-346.
func TestVP8ARNRIterateByteExact(t *testing.T) {
	const (
		width  = 48
		height = 32
	)

	mkCenter := func() (luma, chromaU, chromaV []byte) {
		luma = make([]byte, width*height)
		uvW := (width + 1) >> 1
		uvH := (height + 1) >> 1
		chromaU = make([]byte, uvW*uvH)
		chromaV = make([]byte, uvW*uvH)
		for y := range height {
			for x := range width {
				luma[y*width+x] = byte((y*5 + x*3 + 32) & 0xff)
			}
		}
		for y := range uvH {
			for x := range uvW {
				chromaU[y*uvW+x] = byte((y*7 + x + 96) & 0xff)
				chromaV[y*uvW+x] = byte((x*7 + y + 144) & 0xff)
			}
		}
		return
	}
	mkFlatRef := func(y, u, v byte) arnrFrameView {
		uvW := (width + 1) >> 1
		uvH := (height + 1) >> 1
		view := arnrFrameView{
			width:   width,
			height:  height,
			y:       make([]byte, width*height),
			u:       make([]byte, uvW*uvH),
			v:       make([]byte, uvW*uvH),
			yStride: width,
			uStride: uvW,
			vStride: uvW,
		}
		for i := range view.y {
			view.y[i] = y
		}
		for i := range view.u {
			view.u[i] = u
		}
		for i := range view.v {
			view.v[i] = v
		}
		return view
	}

	for _, strength := range []int{1, 3, 5} {
		t.Run("strength_"+task252Itoa(strength), func(t *testing.T) {
			cy, cu, cv := mkCenter()
			centerView := arnrFrameView{
				width:   width,
				height:  height,
				y:       append([]byte(nil), cy...),
				u:       append([]byte(nil), cu...),
				v:       append([]byte(nil), cv...),
				yStride: width,
				uStride: (width + 1) >> 1,
				vStride: (width + 1) >> 1,
			}
			backward := mkFlatRef(64, 96, 144)
			forward := mkFlatRef(192, 160, 112)
			refs := []arnrFrameView{backward, centerView, forward}
			centerIdx := 1

			opts := EncoderOptions{
				Width:             width,
				Height:            height,
				FPS:               30,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: 200,
				KeyFrameInterval:  999,
				Deadline:          DeadlineGoodQuality,
			}
			e, err := NewVP8Encoder(opts)
			if err != nil {
				t.Fatalf("NewVP8Encoder: %v", err)
			}
			t.Cleanup(func() { _ = e.Close() })

			copyARNRIterationPlane(e.arnrScratch.Img.Y, e.arnrScratch.Img.YStride, cy, width, width, height)
			copyARNRIterationPlane(e.arnrScratch.Img.U, e.arnrScratch.Img.UStride, cu, (width+1)>>1, (width+1)>>1, (height+1)>>1)
			copyARNRIterationPlane(e.arnrScratch.Img.V, e.arnrScratch.Img.VStride, cv, (width+1)>>1, (width+1)>>1, (height+1)>>1)

			centerSrc := vp8encSourceImageForARNRIteration(width, height, cy, cu, cv)
			e.iterateTemporalFilter(centerSrc, strength, refs, centerIdx)

			refDst := arnrFrameView{
				width:   width,
				height:  height,
				y:       append([]byte(nil), cy...),
				u:       append([]byte(nil), cu...),
				v:       append([]byte(nil), cv...),
				yStride: width,
				uStride: (width + 1) >> 1,
				vStride: (width + 1) >> 1,
			}
			libvpxIterateReferenceARNR(t, &refDst, refs, centerIdx, strength)

			compareARNRIterationBytes(t, "Y", e.arnrScratch.Img.Y, e.arnrScratch.Img.YStride, refDst.y, refDst.yStride, width, height)
			uvW := (width + 1) >> 1
			uvH := (height + 1) >> 1
			compareARNRIterationBytes(t, "U", e.arnrScratch.Img.U, e.arnrScratch.Img.UStride, refDst.u, refDst.uStride, uvW, uvH)
			compareARNRIterationBytes(t, "V", e.arnrScratch.Img.V, e.arnrScratch.Img.VStride, refDst.v, refDst.vStride, uvW, uvH)
		})
	}
}

func copyARNRIterationPlane(dst []byte, dstStride int, src []byte, srcStride int, w int, h int) {
	for y := range h {
		copy(dst[y*dstStride:y*dstStride+w], src[y*srcStride:y*srcStride+w])
	}
}

func compareARNRIterationBytes(t *testing.T, plane string, got []byte, gotStride int, want []byte, wantStride int, w int, h int) {
	t.Helper()
	for y := range h {
		for x := range w {
			g := got[y*gotStride+x]
			r := want[y*wantStride+x]
			if g != r {
				t.Fatalf("plane %s (%d,%d) mismatch: govpx=%d libvpx=%d", plane, x, y, g, r)
			}
		}
	}
}

func task252Itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
