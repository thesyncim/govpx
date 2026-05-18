package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// VP9 uncompressed-header writer. Ported from libvpx v1.16.0
// vp9/encoder/vp9_bitstream.c — write_uncompressed_header and its
// sub-helpers. The decoder types in internal/vp9/decoder define the
// canonical wire shape; this file translates them back onto the
// bit stream.
//
// The writer does not allocate; the caller owns the output buffer
// and the BitWriter tracks its bit offset.

// WriteKeyframeUncompressedHeader writes the uncompressed-header
// fragment for a profile-0 keyframe. Returns the byte length of the
// packed header on success. Panics on invalid input — the writer is
// reached after option validation so out-of-range values can't
// occur in the encoder pipeline.
func WriteKeyframeUncompressedHeader(w *BitWriter, h *vp9dec.UncompressedHeader) int {
	return WriteKeyframeUncompressedHeaderWithLoopfilterPrev(w, h, nil, nil)
}

// WriteKeyframeUncompressedHeaderWithLoopfilterPrev writes a keyframe
// uncompressed header while comparing loopfilter deltas against caller-provided
// previous-frame snapshots.
func WriteKeyframeUncompressedHeaderWithLoopfilterPrev(w *BitWriter,
	h *vp9dec.UncompressedHeader,
	prevRef *[vp9dec.MaxRefLfDeltas]int8,
	prevMode *[vp9dec.MaxModeLfDeltas]int8,
) int {
	return writeUncompressedHeader(w, h /*keyframe=*/, true, prevRef, prevMode)
}

// WriteIntraOnlyUncompressedHeader writes the uncompressed-header
// fragment for an intra-only non-key frame. The intra-only path
// reuses the keyframe sync code + frame-size emit but threads the
// 8-bit refresh_frame_flags mask through unchanged and gates the
// per-profile bitdepth/colorspace bits on profile > 0 (profile-0
// defaults to 8-bit 4:2:0).
func WriteIntraOnlyUncompressedHeader(w *BitWriter, h *vp9dec.UncompressedHeader) int {
	return WriteIntraOnlyUncompressedHeaderWithLoopfilterPrev(w, h, nil, nil)
}

// WriteIntraOnlyUncompressedHeaderWithLoopfilterPrev writes an intra-only
// non-key frame header while comparing loopfilter deltas against
// caller-provided previous-frame snapshots.
func WriteIntraOnlyUncompressedHeaderWithLoopfilterPrev(w *BitWriter,
	h *vp9dec.UncompressedHeader,
	prevRef *[vp9dec.MaxRefLfDeltas]int8,
	prevMode *[vp9dec.MaxModeLfDeltas]int8,
) int {
	if h.IntraOnly {
		return writeUncompressedHeader(w, h, false, prevRef, prevMode)
	}
	// Caller should have set IntraOnly; tolerate it being unset by
	// emitting the header verbatim — the parser will set IntraOnly
	// from the wire bit anyway.
	tmp := *h
	tmp.IntraOnly = true
	return writeUncompressedHeader(w, &tmp, false, prevRef, prevMode)
}

// WriteInterUncompressedHeader writes the uncompressed-header
// fragment for an inter (non-intra-only, non-key) frame. The inter
// path adds three (ref_frame_map_idx, ref_sign_bias) tuples, the
// per-ref "found" cascade in write_frame_size_with_refs, the
// allow_high_precision_mv bit, and the interp-filter mode.
//
// `refDims` returns the (width, height) of the reference frame at
// the supplied ring slot. The writer consults it for the "found"
// scan against the current frame size — when a ref matches, the
// writer skips the explicit (width-1, height-1) literals.
func WriteInterUncompressedHeader(w *BitWriter, h *vp9dec.UncompressedHeader,
	refDims func(slot uint8) (uint32, uint32),
) int {
	return WriteInterUncompressedHeaderWithLoopfilterPrev(w, h, refDims, nil, nil)
}

// WriteInterUncompressedHeaderWithLoopfilterPrev writes an inter-frame
// uncompressed header while comparing loopfilter deltas against
// caller-provided previous-frame snapshots.
func WriteInterUncompressedHeaderWithLoopfilterPrev(w *BitWriter,
	h *vp9dec.UncompressedHeader,
	refDims func(slot uint8) (uint32, uint32),
	prevRef *[vp9dec.MaxRefLfDeltas]int8,
	prevMode *[vp9dec.MaxModeLfDeltas]int8,
) int {
	if h.IntraOnly {
		// Caller misuse; clear the flag.
		tmp := *h
		tmp.IntraOnly = false
		return writeUncompressedHeaderInter(w, &tmp, refDims, prevRef, prevMode)
	}
	return writeUncompressedHeaderInter(w, h, refDims, prevRef, prevMode)
}

// WriteShowExistingFrameHeader writes a VP9 show_existing_frame packet
// header. This packet form contains no compressed header or tile payload:
// it displays one previously refreshed reference slot directly.
func WriteShowExistingFrameHeader(w *BitWriter, profile common.BitstreamProfile, slot uint8) int {
	writeFrameMarker(w)
	writeProfile(w, profile)
	w.WriteBit(1)
	w.WriteLiteral(uint32(slot), common.RefFramesLog2)
	return w.BytesWritten()
}

// writeUncompressedHeaderInter is the inter-frame entry. The shape
// is the same as writeUncompressedHeader's else-arm but routes to
// the inter (non-intra-only) sub-branch.
func writeUncompressedHeaderInter(w *BitWriter, h *vp9dec.UncompressedHeader,
	refDims func(slot uint8) (uint32, uint32),
	prevRef *[vp9dec.MaxRefLfDeltas]int8,
	prevMode *[vp9dec.MaxModeLfDeltas]int8,
) int {
	writeFrameMarker(w)
	writeProfile(w, h.Profile)
	w.WriteBit(0) // show_existing_frame = 0
	w.WriteBit(uint32(h.FrameType))
	bit32(w, h.ShowFrame)
	bit32(w, h.ErrorResilientMode)

	if !h.ShowFrame {
		bit32(w, h.IntraOnly) // emit 0 here (caller cleared the flag)
	}
	if !h.ErrorResilientMode {
		w.WriteLiteral(uint32(h.ResetFrameContext), 2)
	}

	// Inter (non-intra-only) branch.
	w.WriteLiteral(uint32(h.RefreshFrameFlags), common.RefFrames)
	for i := range 3 {
		w.WriteLiteral(uint32(h.InterRef.RefIndex[i]), 3)
		w.WriteBit(uint32(h.InterRef.SignBias[i]))
	}
	writeFrameSizeWithRefs(w, h, refDims)
	bit32(w, h.AllowHighPrecisionMv)
	writeInterpFilter(w, h.InterpFilter)

	if !h.ErrorResilientMode {
		bit32(w, h.RefreshFrameContext)
		bit32(w, h.FrameParallelDecoding)
	}
	w.WriteLiteral(uint32(h.FrameContextIdx), 2)

	encodeLoopfilterWithPrev(w, &h.Loopfilter, prevRef, prevMode)
	encodeQuantization(w, &h.Quant)
	encodeSegmentation(w, &h.Seg)
	miCols := int((h.Width + 7) >> 3)
	WriteTileInfo(w, &h.Tile, miCols)

	w.WriteLiteral(uint32(h.FirstPartitionSize), 16)
	return w.BytesWritten()
}

// writeFrameSizeWithRefs mirrors write_frame_size_with_refs
// (libvpx v1.16.0 vp9/encoder/vp9_bitstream.c:1180-1212).
// Walks the three ref slots, writing a 1-bit "found" flag per slot
// (set when the ref dims match the current frame). The first set
// flag wins; subsequent flags emit 0. If no flag sets, the explicit
// (width-1, height-1) literals follow, then the render-flag bit.
//
// Task #167 audit: this routine was confirmed byte-exact with libvpx
// for the RefControl seed #5 (9c3e08e8) byte-4 divergence — the
// fuzz fixture has frame dims = LAST ref dims, so iteration 0 emits
// a single 1 bit and breaks (matching libvpx). The proximate byte-4
// divergence is bit 33 (write_interp_filter, vp9_bitstream.c:855-862,
// filter demoted by fix_interp_filter from per-block switchable_interp
// counts), the same root cause attributed by task #156 for the
// RuntimeControls seed #8 case. The bit cascades are pinned by
// TestWriteFrameSizeWithRefsCascadeBitExact in header_writer_test.go.
//
// Differences from libvpx that are intentional / structurally
// equivalent (no observable wire effect under any sane encode):
//
//   - The SVC-fallback branch (vp9_bitstream.c:1189-1195) is omitted
//     because govpx does not yet support SVC; when SVC lands the
//     branch must be ported verbatim.
//
//   - The refDims==nil path treats every slot as "no buffer" (mirrors
//     libvpx's cfg==NULL case where found retains its prior 0 value);
//     all three found bits are 0 and the explicit literal follows.
//     Production callers always supply a non-nil refDims so the
//     branch is exercised only by unit tests / safety fallbacks.
func writeFrameSizeWithRefs(w *BitWriter, h *vp9dec.UncompressedHeader,
	refDims func(slot uint8) (uint32, uint32),
) {
	found := false
	for i := range 3 {
		match := false
		if !found && refDims != nil {
			rw, rh := refDims(h.InterRef.RefIndex[i])
			match = rw == h.Width && rh == h.Height
		}
		bit32(w, match)
		if match {
			found = true
			// libvpx breaks here; the remaining "found" bits are
			// never emitted, so we follow suit.
			break
		}
	}
	if !found {
		// Emit the remaining "found=0" flags libvpx skipped via break.
		// Walking the indices forward: we emitted bits 0..(i_break-1)
		// already (with i_break being whichever index broke). When no
		// match happens we always emit 3 flags, so we're done.
		w.WriteLiteral(h.Width-1, 16)
		w.WriteLiteral(h.Height-1, 16)
	}
	writeRenderSize(w, h)
}

// writeInterpFilter mirrors write_interp_filter. Bit 0 = "switchable";
// when not switchable, 2 bits select the per-filter literal.
func writeInterpFilter(w *BitWriter, f vp9dec.InterpFilter) {
	if f == vp9dec.InterpSwitchable {
		w.WriteBit(1)
		return
	}
	w.WriteBit(0)
	// libvpx's filter_to_literal[]: EIGHTTAP=0 → 1, EIGHTTAP_SMOOTH=1 → 0,
	// EIGHTTAP_SHARP=2 → 2, BILINEAR=3 → 3.
	var lit uint32
	switch f {
	case vp9dec.InterpEighttap:
		lit = 1
	case vp9dec.InterpEighttapSmooth:
		lit = 0
	case vp9dec.InterpEighttapSharp:
		lit = 2
	case vp9dec.InterpBilinear:
		lit = 3
	}
	w.WriteLiteral(lit, 2)
}

func writeUncompressedHeader(w *BitWriter, h *vp9dec.UncompressedHeader,
	keyframe bool,
	prevRef *[vp9dec.MaxRefLfDeltas]int8,
	prevMode *[vp9dec.MaxModeLfDeltas]int8,
) int {
	writeFrameMarker(w)
	writeProfile(w, h.Profile)
	w.WriteBit(0) // show_existing_frame = 0
	w.WriteBit(uint32(h.FrameType))
	bit32(w, h.ShowFrame)
	bit32(w, h.ErrorResilientMode)

	if keyframe {
		writeSyncCode(w)
		writeBitdepthColorspaceSampling(w, h)
		writeFrameSize(w, h)
	} else {
		// Inter / intra-only branch. The default-frame-type bit is
		// already emitted (cm->frame_type=1 → wrote 1 above), so
		// here we replay libvpx's else-arm.
		if !h.ShowFrame {
			bit32(w, h.IntraOnly)
		}
		if !h.ErrorResilientMode {
			w.WriteLiteral(uint32(h.ResetFrameContext), 2)
		}
		if h.IntraOnly {
			writeSyncCode(w)
			if h.Profile > common.Profile0 {
				writeBitdepthColorspaceSampling(w, h)
			}
			w.WriteLiteral(uint32(h.RefreshFrameFlags), common.RefFrames)
			writeFrameSize(w, h)
		}
		// Inter (non-intra-only) callers use WriteInterUncompressedHeader,
		// which carries the required reference-index and dimension inputs.
	}

	if !h.ErrorResilientMode {
		bit32(w, h.RefreshFrameContext)
		bit32(w, h.FrameParallelDecoding)
	}
	w.WriteLiteral(uint32(h.FrameContextIdx), 2)

	encodeLoopfilterWithPrev(w, &h.Loopfilter, prevRef, prevMode)
	encodeQuantization(w, &h.Quant)
	encodeSegmentation(w, &h.Seg)
	miCols := int((h.Width + 7) >> 3)
	WriteTileInfo(w, &h.Tile, miCols)

	w.WriteLiteral(uint32(h.FirstPartitionSize), 16)
	return w.BytesWritten()
}

func bit32(w *BitWriter, v bool) {
	if v {
		w.WriteBit(1)
	} else {
		w.WriteBit(0)
	}
}

// writeFrameMarker emits the 0b10 frame marker mirroring
// VP9_FRAME_MARKER.
func writeFrameMarker(w *BitWriter) {
	w.WriteLiteral(uint32(common.VP9FrameMarker), 2)
}

// writeProfile mirrors libvpx's write_profile. Encodes 0..3 in 2 or 3
// bits depending on the profile.
func writeProfile(w *BitWriter, p common.BitstreamProfile) {
	switch p {
	case common.Profile0:
		w.WriteLiteral(0, 2)
	case common.Profile1:
		w.WriteLiteral(2, 2)
	case common.Profile2:
		w.WriteLiteral(1, 2)
	default: // Profile3
		w.WriteLiteral(6, 3)
	}
}

// writeSyncCode mirrors write_sync_code.
func writeSyncCode(w *BitWriter) {
	w.WriteLiteral(uint32(common.VP9SyncCode0), 8)
	w.WriteLiteral(uint32(common.VP9SyncCode1), 8)
	w.WriteLiteral(uint32(common.VP9SyncCode2), 8)
}

// writeBitdepthColorspaceSampling mirrors the profile-0 fast path of
// libvpx's write_bitdepth_colorspace_sampling. Profile-0 always
// emits color_space (3 bits) and the color_range bit.
func writeBitdepthColorspaceSampling(w *BitWriter, h *vp9dec.UncompressedHeader) {
	if h.Profile >= common.Profile2 {
		// Bit depth bit (10 → 0, 12 → 1).
		if h.BitDepthColor.BitDepth == vp9dec.Bits12 {
			w.WriteBit(1)
		} else {
			w.WriteBit(0)
		}
	}
	w.WriteLiteral(uint32(h.BitDepthColor.ColorSpace), 3)
	if h.BitDepthColor.ColorSpace != common.CSSRGB {
		w.WriteBit(uint32(h.BitDepthColor.ColorRange))
	}
}

// writeFrameSize mirrors libvpx's write_frame_size: 16-bit width-1,
// 16-bit height-1, then the render_and_frame_size_different bit. When
// h.Render differs from the coded (Width, Height), the bit is set and
// the writer emits the 32-bit (render_width-1, render_height-1) field
// that follows. Zero / matching values keep the render bit at 0 and
// reuse the coded dimensions on the decoder side.
func writeFrameSize(w *BitWriter, h *vp9dec.UncompressedHeader) {
	w.WriteLiteral(h.Width-1, 16)
	w.WriteLiteral(h.Height-1, 16)
	writeRenderSize(w, h)
}

// writeRenderSize mirrors libvpx's write_render_size — write a single
// "render_and_frame_size_different" bit, optionally followed by the
// 32-bit (width-1, height-1) literal.
func writeRenderSize(w *BitWriter, h *vp9dec.UncompressedHeader) {
	rw, rh := h.Render.Width, h.Render.Height
	if rw == 0 || rh == 0 || (rw == h.Width && rh == h.Height) {
		w.WriteBit(0)
		return
	}
	w.WriteBit(1)
	w.WriteLiteral(rw-1, 16)
	w.WriteLiteral(rh-1, 16)
}

// encodeLoopfilter mirrors the reset-state path through libvpx's
// encode_loopfilter. vp9_setup_past_independence zeroes last_ref_deltas /
// last_mode_deltas before default deltas are compared, so zero-valued slots
// emit changed=0 rather than a redundant signed zero.
func encodeLoopfilter(w *BitWriter, lf *vp9dec.LoopfilterParams) {
	encodeLoopfilterWithPrev(w, lf, nil, nil)
}

func encodeLoopfilterWithPrev(w *BitWriter, lf *vp9dec.LoopfilterParams,
	prevRef *[vp9dec.MaxRefLfDeltas]int8,
	prevMode *[vp9dec.MaxModeLfDeltas]int8,
) {
	var zeroRef [vp9dec.MaxRefLfDeltas]int8
	var zeroMode [vp9dec.MaxModeLfDeltas]int8
	if prevRef == nil {
		prevRef = &zeroRef
	}
	if prevMode == nil {
		prevMode = &zeroMode
	}
	EncodeLoopfilterWithPrev(w, lf, prevRef, prevMode)
}

// EncodeLoopfilterWithPrev mirrors libvpx's encode_loopfilter
// exactly: per-slot "changed" bit against the previous frame's
// last_ref_deltas / last_mode_deltas, plus the new 6-bit
// magnitude + sign when changed. `prevRef` / `prevMode`, when
// non-nil, supply the previous-frame snapshot; passing nil forces
// every slot to emit a changed bit and signed value.
func EncodeLoopfilterWithPrev(w *BitWriter, lf *vp9dec.LoopfilterParams,
	prevRef *[vp9dec.MaxRefLfDeltas]int8,
	prevMode *[vp9dec.MaxModeLfDeltas]int8,
) {
	w.WriteLiteral(uint32(lf.FilterLevel), 6)
	w.WriteLiteral(uint32(lf.SharpnessLevel), 3)
	bit32(w, lf.ModeRefDeltaEnabled)
	if !lf.ModeRefDeltaEnabled {
		return
	}
	bit32(w, lf.ModeRefDeltaUpdate)
	if !lf.ModeRefDeltaUpdate {
		return
	}
	for i := range vp9dec.MaxRefLfDeltas {
		changed := prevRef == nil || prevRef[i] != lf.RefDeltas[i]
		bit32(w, changed)
		if changed {
			writeAbsSigned6(w, int32(lf.RefDeltas[i]))
		}
	}
	for i := range vp9dec.MaxModeLfDeltas {
		changed := prevMode == nil || prevMode[i] != lf.ModeDeltas[i]
		bit32(w, changed)
		if changed {
			writeAbsSigned6(w, int32(lf.ModeDeltas[i]))
		}
	}
}

func writeAbsSigned6(w *BitWriter, v int32) {
	mag := v
	sign := uint32(0)
	if mag < 0 {
		mag = -mag
		sign = 1
	}
	w.WriteLiteral(uint32(mag)&0x3F, 6)
	w.WriteBit(sign)
}

// encodeQuantization mirrors encode_quantization + write_delta_q.
func encodeQuantization(w *BitWriter, q *vp9dec.QuantizationParams) {
	w.WriteLiteral(uint32(q.BaseQindex), 8)
	writeDeltaQ(w, int32(q.YDcDeltaQ))
	writeDeltaQ(w, int32(q.UvDcDeltaQ))
	writeDeltaQ(w, int32(q.UvAcDeltaQ))
}

func writeDeltaQ(w *BitWriter, delta int32) {
	if delta == 0 {
		w.WriteBit(0)
		return
	}
	w.WriteBit(1)
	mag := delta
	sign := uint32(0)
	if mag < 0 {
		mag = -mag
		sign = 1
	}
	w.WriteLiteral(uint32(mag), 4)
	w.WriteBit(sign)
}

// encodeSegmentation mirrors libvpx v1.16.0 encode_segmentation
// in vp9/encoder/vp9_bitstream.c. Walks the map / data update
// gates: TreeProbs / PredProbs encode "no update" via prob =
// MAX_PROB so the encoder emits the 0 bit; otherwise emits 1 +
// 8-bit literal. The per-feature data block walks
// MaxSegments × SegLvlMax, gated by FeatureMask, encoding data
// with get_unsigned_bits(max) bits + an optional sign bit for the
// signed features (SEG_LVL_ALT_Q, SEG_LVL_ALT_LF).
func encodeSegmentation(w *BitWriter, seg *vp9dec.SegmentationParams) {
	bit32(w, seg.Enabled)
	if !seg.Enabled {
		return
	}

	bit32(w, seg.UpdateMap)
	if seg.UpdateMap {
		for i := range vp9dec.SegTreeProbs {
			p := seg.TreeProbs[i]
			if p == vp9dec.MaxProb {
				w.WriteBit(0)
				continue
			}
			w.WriteBit(1)
			w.WriteLiteral(uint32(p), 8)
		}
		bit32(w, seg.TemporalUpdate)
		if seg.TemporalUpdate {
			for i := range vp9dec.PredictionProbs {
				p := seg.PredProbs[i]
				if p == vp9dec.MaxProb {
					w.WriteBit(0)
					continue
				}
				w.WriteBit(1)
				w.WriteLiteral(uint32(p), 8)
			}
		}
	}

	bit32(w, seg.UpdateData)
	if !seg.UpdateData {
		return
	}
	bit32(w, seg.AbsDelta)
	for i := range vp9dec.MaxSegments {
		for j := range vp9dec.SegLvlMax {
			active := seg.FeatureMask[i]&(1<<uint(j)) != 0
			bit32(w, active)
			if !active {
				continue
			}
			data := int(seg.FeatureData[i][j])
			dataMax := segFeatureDataMax(j)
			signed := segFeatureDataSigned(j)
			mag := data
			if mag < 0 {
				mag = -mag
			}
			encodeUnsignedMax(w, mag, dataMax)
			if signed {
				if data < 0 {
					w.WriteBit(1)
				} else {
					w.WriteBit(0)
				}
			}
		}
	}
}

// encodeUnsignedMax mirrors libvpx's encode_unsigned_max — write
// `data` as a fixed-width literal sized by get_unsigned_bits(max).
// The decoder side's decodeUnsignedMax reads the same width and
// saturates at max, so out-of-range values aren't expressible.
func encodeUnsignedMax(w *BitWriter, data, max int) {
	bits := getUnsignedBits(max)
	if bits == 0 {
		return
	}
	w.WriteLiteral(uint32(data), bits)
}

// getUnsignedBits mirrors libvpx's get_unsigned_bits — number of
// bits needed to represent any value in [0, n].
func getUnsignedBits(n int) int {
	if n <= 0 {
		return 0
	}
	bits := 0
	for v := uint(n); v > 0; v >>= 1 {
		bits++
	}
	return bits
}

// segFeatureDataMax mirrors libvpx's seg_feature_data_max table.
// Indexed by feature slot (SegLvlAltQ / AltLf / RefFrame / Skip).
func segFeatureDataMax(j int) int {
	switch j {
	case vp9dec.SegLvlAltQ:
		return 255 // MAXQ
	case vp9dec.SegLvlAltLf:
		return 63 // MAX_LOOP_FILTER
	case vp9dec.SegLvlRefFrame:
		return 3
	default:
		return 0
	}
}

// segFeatureDataSigned mirrors libvpx's seg_feature_data_signed
// table — only the alt-q and alt-lf features carry a sign bit.
func segFeatureDataSigned(j int) bool {
	return j == vp9dec.SegLvlAltQ || j == vp9dec.SegLvlAltLf
}

// WriteTileInfo mirrors libvpx's write_tile_info inline in
// vp9/encoder/vp9_bitstream.c. Computes (min, max) log2 tile-cols
// from the frame's mi_cols (via vp9dec.TileNBits, the canonical
// source already shared with the decoder), emits (current - min)
// one-bits + a zero terminator if we're not at max, then 1 bit for
// log2_tile_rows != 0 and (if so) a second bit for log2_tile_rows
// != 1. The bit pattern round-trips through ReadTileInfo.
func WriteTileInfo(w *BitWriter, t *vp9dec.TileInfo, miCols int) {
	minLog2, maxLog2 := vp9dec.TileNBits(miCols)
	ones := t.Log2TileCols - minLog2
	for range ones {
		w.WriteBit(1)
	}
	if t.Log2TileCols < maxLog2 {
		w.WriteBit(0)
	}
	if t.Log2TileRows != 0 {
		w.WriteBit(1)
		if t.Log2TileRows != 1 {
			w.WriteBit(1)
		} else {
			w.WriteBit(0)
		}
	} else {
		w.WriteBit(0)
	}
}
