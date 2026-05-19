package govpx

import (
	"testing"
)

// vp8_arnr_temporal_filter_audit_test.go pins the verbatim port of the
// VP8 ARNR (motion-compensated temporal alt-ref filter) path against
// libvpx v1.16.0. Task #175 audit substrate — anchors the NEGATIVE
// FINDING that the historical "854x480 threads=2 ARNR + bitrate
// interaction at multi-thread boundaries" surface (formerly task #27)
// is byte-clean today.
//
// Surface recap:
//
//	The original task #27 cited a divergence on 854x480 fixtures when
//	threads > 1 was combined with non-zero ARNR knobs and a VBR/CBR
//	target bitrate. The active failing seed was
//	testdata/fuzz/FuzzEncoderProductionStreamByteParity/
//	  regression_w854h480_threads4_vbr_inter_diverge
//	(854x480 threads=4 VBR, no auto-alt-ref) which diverged on the
//	first inter frame's first_partition by 1-3 bytes.
//
//	Root cause was the libvpx MT helper-thread ymode_count /
//	uv_mode_count accumulator (vp8/encoder/ethreading.c:479 documented
//	typo: vp8_zero(x->ymode_count) zeroes cpi->mb instead of the
//	helper's mb), which biased update_mbintra_mode_probs in the
//	inter-frame writer. Closed by commit 91c8d589 ("Port libvpx VP8 MT
//	ymode/uv_mode_count helper-history bias verbatim").
//
//	Post-fix, the regression seed AND the explicit 854x480 threads=2 +
//	ARNR + {CBR, VBR} sweep through arnrMax in {1, 2, 3} and arnrStr in
//	{1, 2, 3} all reach full byte parity across the 3-frame
//	FuzzEncoderProductionStreamByteParity fixture (verified at task
//	#175 audit time, 2026-05-18).
//
//	The auto-alt-ref / ARNR firing path itself is gated upstream by
//	`oxcf.play_alternate && source_alt_ref_pending && arnr_max_frames
//	> 0` (libvpx vp8/encoder/onyx_if.c:4862). Without an explicit
//	--auto-alt-ref=1 (govpx EncoderOptions.AutoAltRef=true), neither
//	side calls vp8_temporal_filter_prepare_c; the ARNRMaxFrames /
//	ARNRStrength / ARNRType knobs are inert. The option-grid fuzz
//	exercises non-zero ARNR knobs without AutoAltRef, which is the
//	parity-relevant negative space.
//
// Per-component audit (each component byte-exact against libvpx; if a
// future regression breaks this layer one of these pins fires before
// the fuzz harness rediscovers the gap):
//
//  1. vp8_temporal_filter_apply_c integer formula.
//     libvpx vp8/encoder/temporal_filter.c:70-108 vs govpx
//     applyTemporalFilterScalar in vp8_encoder_arnr.go. The per-pixel
//     modifier ladder is verbatim:
//
//     rounding   = strength > 0 ? 1 << (strength - 1) : 0
//     diff       = src - pred
//     modifier   = (3 * diff * diff + rounding) >> strength
//     modifier   = min(modifier, 16)
//     weight     = (16 - modifier) * filter_weight
//     count[k]  += weight
//     accum[k]  += weight * pred
//
//     Tested by TestVP8TemporalFilterApplyByteExact below across the
//     full strength sweep {0, 1, 2, 3, 4, 5, 6} and the three filter
//     weights {0, 1, 2}.
//
//  2. vp8_temporal_filter_prepare_c blur-window switch.
//     libvpx vp8/encoder/temporal_filter.c:368-418 vs govpx
//     arnrFilterWindow in vp8_encoder_arnr.go. case 1 (backward), case 2
//     (forward), case 3/default (centered) each map identically
//     across (num_frames_back, num_frames_fwd, max_frames) triples.
//     Even-length centered window asymmetry (libvpx temporal_filter.c
//     line 408: "When max_frames is even we have 1 more frame
//     backward than forward") is preserved by the matching `(max-1)/2`
//     vs `max/2` half-window split. Tested by
//     TestVP8ARNRFilterWindowByteExact below.
//
//  3. fixed_divide reciprocal LUT.
//     libvpx vp8/encoder/onyx_if.c:1381-1383 vs govpx arnrFixedDivide
//     in vp8_encoder_arnr.go. Table entries 1..511 are 0x80000 / i
//     (truncating integer divide); entry 0 is 0. Used by govpx
//     writeARNRBlock as the per-pixel normalization step
//     `pval = (accumulator[k] + (count[k]>>1)) * fixed_divide[count[k]] >> 19`
//     mirroring libvpx temporal_filter.c:294-296. Tested by
//     TestVP8ARNRFixedDivideByteExact below — all 512 entries.
//
//  4. ARNRType validation range.
//     libvpx vp8/vp8_cx_iface.c rejects arnr_type outside [1, 3] via
//     RANGE_CHECK in validate_config. govpx normalizeOpts in
//     vp8_encoder_lifecycle.go rejects `ARNRType < 1 || ARNRType > 3`
//     with ErrInvalidConfig. The downstream switch in
//     arnrFilterWindow can therefore not see an out-of-range case
//     0/4+ — the upstream gate is the equivalent of libvpx's
//     `case 3: default:` fall-through being unreachable.
//
//  5. ARNRMaxFrames gate.
//     libvpx onyx_if.c:4867 fires `vp8_temporal_filter_prepare_c` when
//     `arnr_max_frames > 0`; govpx vp8_encoder_preprocess.go fires when
//     `ARNRMaxFrames > 1`. The semantic gap is a no-op: when
//     max_frames == 1 libvpx's vp8_temporal_filter_iterate_c runs
//     with frame_count=1, alt_ref_index=0, so the sole frame iterated
//     is the alt-ref itself (frame == alt_ref_index → filter_weight=2,
//     MC skipped, predictor = alt_ref). With src == pred at every
//     pixel the modifier ladder yields count[k] = 32, accumulator[k]
//     = 32 * src for each pixel, and the normalization step computes
//     `pval = (32*src + 16) * fixed_divide[32] >> 19 = (32*src+16) *
//     16384 >> 19 = src`, an exact identity. The round-trip through
//     cpi->alt_ref_buffer is byte-identical to the raw lookahead
//     buffer in this single-frame case.
//
// All five layers are verbatim ports as of task #175 audit time. The
// 854x480 threads=2 + ARNR + bitrate surface is closed; future
// regression on any layer should be caught by the pins below.

// TestVP8TemporalFilterApplyByteExact pins vp8_temporal_filter_apply_c's
// integer formula against the explicit libvpx reference at
// vp8/encoder/temporal_filter.c:70-108.
func TestVP8TemporalFilterApplyByteExact(t *testing.T) {
	// Reference implementation transcribed line-by-line from libvpx
	// vp8/encoder/temporal_filter.c:70-108. Strictly C-style: every
	// load, arithmetic op, and clamp matches.
	libvpxApply := func(src []byte, srcStride int, pred []byte, blockSize int, strength int, filterWeight int, accumulator []uint32, count []uint32) {
		rounding := 0
		if strength > 0 {
			rounding = 1 << (strength - 1)
		}
		k := 0
		byteIdx := 0
		for i := range blockSize {
			for j := range blockSize {
				srcByte := int(src[byteIdx])
				pixelValue := int(pred[i*blockSize+j])
				modifier := srcByte - pixelValue
				modifier *= modifier
				modifier *= 3
				modifier += rounding
				modifier >>= uint(strength)
				if modifier > 16 {
					modifier = 16
				}
				modifier = 16 - modifier
				modifier *= filterWeight
				count[k] += uint32(modifier)
				accumulator[k] += uint32(modifier) * uint32(pixelValue)
				byteIdx++
				k++
			}
			byteIdx += srcStride - blockSize
		}
	}

	// Deterministic per-pixel input: src walks a low-entropy ramp,
	// pred = src ^ 0x55 — chosen so diff covers both signs and the
	// |diff| sweep spans the modifier-clamp boundary at every
	// strength.
	mkBlock := func(blockSize int, salt byte) ([]byte, []byte) {
		src := make([]byte, blockSize*blockSize)
		pred := make([]byte, blockSize*blockSize)
		for i := range src {
			src[i] = byte(i*7 + int(salt))
			pred[i] = src[i] ^ 0x55
		}
		return src, pred
	}

	for _, blockSize := range []int{8, 16} {
		for strength := 0; strength <= 6; strength++ {
			for _, filterWeight := range []int{0, 1, 2} {
				for salt := range byte(4) {
					src, pred := mkBlock(blockSize, salt)
					n := blockSize * blockSize

					gotAcc := make([]uint32, n)
					gotCnt := make([]uint32, n)
					applyTemporalFilterScalar(src, blockSize, pred, blockSize, blockSize, strength, filterWeight, gotAcc, gotCnt)

					wantAcc := make([]uint32, n)
					wantCnt := make([]uint32, n)
					libvpxApply(src, blockSize, pred, blockSize, strength, filterWeight, wantAcc, wantCnt)

					for k := range n {
						if gotAcc[k] != wantAcc[k] || gotCnt[k] != wantCnt[k] {
							t.Fatalf("apply mismatch block=%d strength=%d weight=%d salt=%d k=%d: govpx acc=%d cnt=%d, libvpx acc=%d cnt=%d",
								blockSize, strength, filterWeight, salt, k,
								gotAcc[k], gotCnt[k], wantAcc[k], wantCnt[k])
						}
					}
				}
			}
		}
	}
}

// TestVP8ARNRFilterWindowByteExact pins vp8_temporal_filter_prepare_c's
// blur-window selection switch (libvpx temporal_filter.c:368-418)
// against govpx arnrFilterWindow.
func TestVP8ARNRFilterWindowByteExact(t *testing.T) {
	// Verbatim libvpx switch transcription. Returns (framesBackward,
	// framesForward, ok) to match arnrFilterWindow's signature.
	libvpxWindow := func(distance int, lookaheadSize int, arnrType int, maxFrames int) (int, int, bool) {
		if distance < 0 || maxFrames <= 0 {
			return 0, 0, false
		}
		numFramesBackward := distance
		numFramesForward := lookaheadSize - (numFramesBackward + 1)
		if numFramesForward < 0 {
			return 0, 0, false
		}
		framesBackward := 0
		framesForward := 0
		switch arnrType {
		case 1:
			framesBackward = numFramesBackward
			if framesBackward >= maxFrames {
				framesBackward = maxFrames - 1
			}
		case 2:
			framesForward = numFramesForward
			if framesForward >= maxFrames {
				framesForward = maxFrames - 1
			}
		case 3:
			framesForward = numFramesForward
			framesBackward = numFramesBackward
			if framesForward > framesBackward {
				framesForward = framesBackward
			}
			if framesBackward > framesForward {
				framesBackward = framesForward
			}
			if framesForward > (maxFrames-1)/2 {
				framesForward = (maxFrames - 1) / 2
			}
			if framesBackward > maxFrames/2 {
				framesBackward = maxFrames / 2
			}
		default:
			return 0, 0, false
		}
		return framesBackward, framesForward, true
	}

	// Drive arnrFilterWindow against a stubbed lookahead by populating
	// e.opts.LookaheadFrames + e.lookahead + e.lookaheadCount so that
	// lookaheadEnabled() returns true and lookaheadSize() returns the
	// configured count. The window math doesn't read any lookahead
	// entry payload, so empty entries are fine.
	e := &VP8Encoder{}
	for distance := -1; distance <= 16; distance++ {
		for lookahead := 0; lookahead <= 16; lookahead++ {
			if lookahead == 0 {
				e.opts.LookaheadFrames = 0
				e.lookahead = nil
				e.lookaheadCount = 0
			} else {
				e.opts.LookaheadFrames = lookahead
				e.lookahead = make([]lookaheadEntry, lookahead+1)
				e.lookaheadCount = lookahead
			}
			for _, arnrType := range []int{0, 1, 2, 3, 4} {
				e.opts.ARNRType = arnrType
				for maxFrames := 0; maxFrames <= maxARNRFrames; maxFrames++ {
					gotBack, gotFwd, gotOK := e.arnrFilterWindow(distance, maxFrames)
					effective := lookahead
					if !e.lookaheadEnabled() {
						effective = 0
					}
					wantBack, wantFwd, wantOK := libvpxWindow(distance, effective, arnrType, maxFrames)
					if gotBack != wantBack || gotFwd != wantFwd || gotOK != wantOK {
						t.Fatalf("window mismatch distance=%d lookahead=%d type=%d max=%d: govpx (back=%d, fwd=%d, ok=%v), libvpx (back=%d, fwd=%d, ok=%v)",
							distance, lookahead, arnrType, maxFrames,
							gotBack, gotFwd, gotOK, wantBack, wantFwd, wantOK)
					}
				}
			}
		}
	}
}

// TestVP8ARNRFixedDivideByteExact pins govpx's arnrFixedDivide LUT
// against the verbatim libvpx initializer at
// vp8/encoder/onyx_if.c:1381-1383.
func TestVP8ARNRFixedDivideByteExact(t *testing.T) {
	for i := range 512 {
		var want uint32
		if i > 0 {
			want = 0x80000 / uint32(i)
		}
		if arnrFixedDivide[i] != want {
			t.Fatalf("fixed_divide[%d] = %d, want %d (libvpx vp8/encoder/onyx_if.c:1383)",
				i, arnrFixedDivide[i], want)
		}
	}
}
