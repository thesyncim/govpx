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
// Coverage: keyframe path only. The intra-only and inter paths
// require ref-frame map indices and ref dimension lookups that
// the public API doesn't surface yet; they land in subsequent
// commits.
//
// The writer does not allocate; the caller owns the output buffer
// and the BitWriter tracks its bit offset.

// WriteKeyframeUncompressedHeader writes the uncompressed-header
// fragment for a profile-0 keyframe. Returns the byte length of the
// packed header on success. Panics on invalid input — the writer is
// reached after option validation so out-of-range values can't
// occur in the encoder pipeline.
func WriteKeyframeUncompressedHeader(w *BitWriter, h *vp9dec.UncompressedHeader) int {
	return writeUncompressedHeader(w, h /*keyframe=*/, true)
}

// WriteIntraOnlyUncompressedHeader writes the uncompressed-header
// fragment for an intra-only non-key frame. The intra-only path
// reuses the keyframe sync code + frame-size emit but threads the
// 8-bit refresh_frame_flags mask through unchanged and gates the
// per-profile bitdepth/colorspace bits on profile > 0 (profile-0
// defaults to 8-bit 4:2:0).
func WriteIntraOnlyUncompressedHeader(w *BitWriter, h *vp9dec.UncompressedHeader) int {
	if h.IntraOnly {
		return writeUncompressedHeader(w, h, false)
	}
	// Caller should have set IntraOnly; tolerate it being unset by
	// emitting the header verbatim — the parser will set IntraOnly
	// from the wire bit anyway.
	tmp := *h
	tmp.IntraOnly = true
	return writeUncompressedHeader(w, &tmp, false)
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
	if h.IntraOnly {
		// Caller misuse; clear the flag.
		tmp := *h
		tmp.IntraOnly = false
		return writeUncompressedHeaderInter(w, &tmp, refDims)
	}
	return writeUncompressedHeaderInter(w, h, refDims)
}

// writeUncompressedHeaderInter is the inter-frame entry. The shape
// is the same as writeUncompressedHeader's else-arm but routes to
// the inter (non-intra-only) sub-branch.
func writeUncompressedHeaderInter(w *BitWriter, h *vp9dec.UncompressedHeader,
	refDims func(slot uint8) (uint32, uint32),
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
	for i := 0; i < 3; i++ {
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

	encodeLoopfilter(w, &h.Loopfilter)
	encodeQuantization(w, &h.Quant)
	encodeSegmentation(w, &h.Seg)
	miCols := int((h.Width + 7) >> 3)
	writeTileInfo(w, &h.Tile, miCols)

	w.WriteLiteral(uint32(h.FirstPartitionSize), 16)
	return w.BytesWritten()
}

// writeFrameSizeWithRefs mirrors write_frame_size_with_refs.
// Walks the three ref slots, writing a 1-bit "found" flag per slot
// (set when the ref dims match the current frame). The first set
// flag wins; subsequent flags emit 0. If no flag sets, the explicit
// (width-1, height-1) literals follow, then the render-flag bit.
func writeFrameSizeWithRefs(w *BitWriter, h *vp9dec.UncompressedHeader,
	refDims func(slot uint8) (uint32, uint32),
) {
	found := false
	for i := 0; i < 3; i++ {
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
	w.WriteBit(0) // render_flag = 0
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

func writeUncompressedHeader(w *BitWriter, h *vp9dec.UncompressedHeader, keyframe bool) int {
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
		// The inter (non-intra-only) branch lands when ref-frame
		// management is wired up.
	}

	if !h.ErrorResilientMode {
		bit32(w, h.RefreshFrameContext)
		bit32(w, h.FrameParallelDecoding)
	}
	w.WriteLiteral(uint32(h.FrameContextIdx), 2)

	encodeLoopfilter(w, &h.Loopfilter)
	encodeQuantization(w, &h.Quant)
	encodeSegmentation(w, &h.Seg)
	miCols := int((h.Width + 7) >> 3)
	writeTileInfo(w, &h.Tile, miCols)

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

// writeFrameSize mirrors libvpx's write_frame_size — 16-bit width-1
// then 16-bit height-1 then a 1-bit render flag (always 0 for now;
// the render_size full path lands when the encoder exposes a custom
// render size).
func writeFrameSize(w *BitWriter, h *vp9dec.UncompressedHeader) {
	w.WriteLiteral(h.Width-1, 16)
	w.WriteLiteral(h.Height-1, 16)
	// render_flag = 0; no per-frame render size.
	w.WriteBit(0)
}

// encodeLoopfilter mirrors encode_loopfilter. The mode_ref_delta
// update path emits change-mask bits + the per-slot magnitude/sign;
// when delta_update is off we emit just the enabled bit.
func encodeLoopfilter(w *BitWriter, lf *vp9dec.LoopfilterParams) {
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
	for i := 0; i < vp9dec.MaxRefLfDeltas; i++ {
		// libvpx emits a per-slot "changed" bit + the new value;
		// we don't track last_ref_deltas yet so emit unchanged.
		w.WriteBit(1)
		writeAbsSigned6(w, int32(lf.RefDeltas[i]))
	}
	for i := 0; i < vp9dec.MaxModeLfDeltas; i++ {
		w.WriteBit(1)
		writeAbsSigned6(w, int32(lf.ModeDeltas[i]))
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

// encodeSegmentation mirrors the disabled-fast-path of
// libvpx's encode_segmentation. Full implementation (map updates,
// per-feature data, temporal predictor probs) lands when the encoder
// exposes the segmentation pass.
func encodeSegmentation(w *BitWriter, seg *vp9dec.SegmentationParams) {
	bit32(w, seg.Enabled)
	if !seg.Enabled {
		return
	}
	// Stub for non-zero seg: emit (update_map=0, update_data=0) so
	// the decoder preserves the previous-frame state. Full path
	// lands in a later commit.
	w.WriteBit(0) // update_map
	w.WriteBit(0) // update_data
}

// writeTileInfo mirrors write_tile_info. Compute (min, max) log2
// tile-cols from mi_cols, emit (current - min) one-bits + a zero if
// we're not at max, then 1 bit for log2_tile_rows != 0 and (if so) a
// second bit for log2_tile_rows != 1.
func writeTileInfo(w *BitWriter, t *vp9dec.TileInfo, miCols int) {
	minLog2, maxLog2 := tileNBits(miCols)
	ones := t.Log2TileCols - minLog2
	for i := 0; i < ones; i++ {
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

func tileNBits(miCols int) (minLog2, maxLog2 int) {
	const (
		miBlockSizeLog2 = common.MiBlockSizeLog2
		minTileWidthB64 = 4
		maxTileWidthB64 = 64
	)
	sb64Cols := alignPowerOfTwo(miCols, miBlockSizeLog2) >> miBlockSizeLog2
	for (maxTileWidthB64 << minLog2) < sb64Cols {
		minLog2++
	}
	maxLog2 = 1
	for (sb64Cols >> maxLog2) >= minTileWidthB64 {
		maxLog2++
	}
	maxLog2--
	return
}

func alignPowerOfTwo(value, n int) int {
	return (value + (1 << uint(n)) - 1) &^ ((1 << uint(n)) - 1)
}
