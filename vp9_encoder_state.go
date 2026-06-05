package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

const (
	vp9LastRefSlot   = 0
	vp9GoldenRefSlot = 1
	vp9AltRefSlot    = 2
)

const (
	vp9NoUpdateRefFlags        = EncodeNoUpdateLast | EncodeNoUpdateGolden | EncodeNoUpdateAltRef
	vp9ExternalRefreshCtlFlags = vp9NoUpdateRefFlags | EncodeForceGoldenFrame | EncodeForceAltRefFrame
)

func vp9TemporalReferenceRefresh(refreshFlags uint8) temporalReferenceRefresh {
	return temporalReferenceRefresh{
		Last:   refreshFlags&(1<<uint(vp9LastRefSlot)) != 0,
		Golden: refreshFlags&(1<<uint(vp9GoldenRefSlot)) != 0,
		AltRef: refreshFlags&(1<<uint(vp9AltRefSlot)) != 0,
	}
}

func (e *VP9Encoder) resetVP9EncoderFrameContexts() {
	e.fc = encoder.ResetFrameContexts(&e.frameContexts)
}

func (e *VP9Encoder) prepareVP9EncoderFrameContext(hdr *vp9dec.UncompressedHeader) int {
	idx, fc := encoder.PrepareFrameContext(&e.frameContexts, hdr)
	e.fc = fc
	return idx
}

func (e *VP9Encoder) commitVP9EncoderFrameContext(hdr *vp9dec.UncompressedHeader, idx int) {
	encoder.CommitFrameContext(&e.frameContexts, e.fc, hdr, idx)
}

func (e *VP9Encoder) updateVP9NonrdModeCostFrameContext(frameIsIntra bool) {
	if !e.vp9NonrdModeCostFcValid || e.sf.UseNonrdPickMode == 0 ||
		frameIsIntra || e.frameIndex&0x07 == 1 {
		e.vp9NonrdModeCostFc = e.fc
		e.vp9NonrdModeCostFcValid = true
	}
	e.updateVP9NonrdMvCostFrameContext(frameIsIntra)
}

func (e *VP9Encoder) vp9NonrdModeCostFrameContext() vp9dec.FrameContext {
	if e.vp9NonrdModeCostFcValid {
		return e.vp9NonrdModeCostFc
	}
	return e.fc
}

// updateVP9NonrdMvCostFrameContext mirrors libvpx's vp9_build_nmv_cost_table
// refresh inside vp9_initialize_rd_consts (vp9_rd.c:435-443). The MV-entropy
// cost table is rebuilt only when the fill_mode_costs gate
// (!use_nonrd_pick_mode || current_video_frame&7 == 1 || KEY_FRAME) holds AND
// the frame is non-intra (the inner !frame_is_intra_only guard). KEY_FRAME is
// always intra, so the KEY_FRAME leg never rebuilds the MV cost; the table
// stays at its vpx_calloc'd zero state until the first non-intra build, which
// vp9NonrdMvCostFcValid tracks.
func (e *VP9Encoder) updateVP9NonrdMvCostFrameContext(frameIsIntra bool) {
	if frameIsIntra {
		return
	}
	if e.sf.UseNonrdPickMode == 0 || e.frameIndex&0x07 == 1 {
		e.vp9NonrdMvCostFc = e.fc
		e.vp9NonrdMvCostFcValid = true
	}
}

// vp9NonrdMvCostFrameContext returns the frozen MV-entropy cost FrameContext
// and whether the underlying x->nmvcost table has been built at least once.
// When false, the nonrd motion search must cost MVs with zero entropy
// (the calloc'd table), not with the live/mode-cost FrameContext probabilities.
func (e *VP9Encoder) vp9NonrdMvCostFrameContext() (vp9dec.FrameContext, bool) {
	if e.vp9NonrdMvCostFcValid {
		return e.vp9NonrdMvCostFc, true
	}
	return e.fc, false
}

func (e *VP9Encoder) adaptVP9EncoderFrameContext(hdr *vp9dec.UncompressedHeader,
	idx int, counts *encoder.FrameCounts, txMode common.TxMode,
) {
	if e == nil || hdr == nil || counts == nil ||
		idx < 0 || idx >= common.FrameContexts ||
		hdr.ErrorResilientMode || hdr.FrameParallelDecoding {
		return
	}
	encoder.AdaptFrameContext(&e.fc, &e.frameContexts, hdr, idx, counts, txMode,
		e.lastVP9HeaderValid && e.lastVP9HeaderFrameType == common.KeyFrame)
}

func (e *VP9Encoder) vp9FrameParallelDecodingMode() bool {
	if e == nil || e.opts.ErrorResilient || !e.opts.FrameParallelDecodingSet {
		return true
	}
	return e.opts.FrameParallelDecoding
}

func (e *VP9Encoder) vp9TimingState() timingState {
	return vp9TimingStateFromOptions(e.opts)
}

func vp9TimingStateFromOptions(opts VP9EncoderOptions) timingState {
	fps := opts.FPS
	if opts.TimebaseNum > 0 && opts.TimebaseDen > 0 {
		return timingState{
			timebaseNum:   opts.TimebaseNum,
			timebaseDen:   opts.TimebaseDen,
			frameDuration: 1,
		}
	}
	if fps == 0 {
		fps = 30
	}
	return timingState{timebaseNum: 1, timebaseDen: fps, frameDuration: 1}
}

func (e *VP9Encoder) vp9TemporalBufferConfig() temporalBufferConfig {
	return temporalBufferConfig{
		timing:              e.vp9TimingState(),
		bufferInitialSizeMs: libvpxDefaultBufferInitialMs,
		bufferSizeMs:        libvpxDefaultBufferSizeMs,
	}
}

func (e *VP9Encoder) vp9ResultTargetBitrateKbps() int {
	if e.rc.enabled {
		return e.rc.targetBitrateKbps
	}
	return e.opts.TargetBitrateKbps
}

func vp9InterReferenceMask(flags EncodeFlags) uint8 {
	var mask uint8
	if flags&EncodeNoReferenceLast == 0 {
		mask |= 1 << uint(vp9dec.LastFrame)
	}
	if flags&EncodeNoReferenceGolden == 0 {
		mask |= 1 << uint(vp9dec.GoldenFrame)
	}
	if flags&EncodeNoReferenceAltRef == 0 {
		mask |= 1 << uint(vp9dec.AltrefFrame)
	}
	return mask
}

func (e *VP9Encoder) vp9InterReferenceMaskForFrame(flags EncodeFlags) uint8 {
	mask := vp9InterReferenceMask(flags)
	const explicitNoRef = EncodeNoReferenceLast | EncodeNoReferenceGolden |
		EncodeNoReferenceAltRef
	if e == nil || flags&explicitNoRef != 0 {
		return mask
	}
	return e.vp9PruneAliasedInterReferenceMask(mask)
}

func (e *VP9Encoder) vp9PruneAliasedInterReferenceMask(mask uint8) uint8 {
	if e == nil {
		return mask
	}
	if e.vp9ReferenceSlotsAlias(vp9GoldenRefSlot, vp9LastRefSlot) {
		mask &^= 1 << uint(vp9dec.GoldenFrame)
	}
	if e.vp9ReferenceSlotsAlias(vp9AltRefSlot, vp9LastRefSlot) ||
		e.vp9ReferenceSlotsAlias(vp9AltRefSlot, vp9GoldenRefSlot) {
		mask &^= 1 << uint(vp9dec.AltrefFrame)
	}
	return mask
}

func (e *VP9Encoder) vp9ReferenceSlotsAlias(slot, other int) bool {
	if slot < 0 || slot >= len(e.refValid) || other < 0 ||
		other >= len(e.refValid) {
		return false
	}
	return e.refValid[slot] && e.refValid[other] &&
		e.refMap[slot] != 0 && e.refMap[slot] == e.refMap[other]
}

func vp9AllInterReferencesDisabled(flags EncodeFlags) bool {
	const allNoRef = EncodeNoReferenceLast | EncodeNoReferenceGolden | EncodeNoReferenceAltRef
	return flags&allNoRef == allNoRef
}

func vp9InterRefreshFrameFlags(flags EncodeFlags) uint8 {
	flags = normalizeVP9EncodeFlags(flags)
	if flags&vp9ExternalRefreshCtlFlags == 0 {
		return 1 << vp9LastRefSlot
	}
	refresh := uint8(0x07)
	if flags&EncodeNoUpdateLast != 0 {
		refresh &^= 1 << vp9LastRefSlot
	}
	if flags&EncodeNoUpdateGolden != 0 {
		refresh &^= 1 << vp9GoldenRefSlot
	}
	if flags&EncodeNoUpdateAltRef != 0 {
		refresh &^= 1 << vp9AltRefSlot
	}
	return refresh
}

func (e *VP9Encoder) vp9InterRefreshFrameFlags(flags EncodeFlags) uint8 {
	// libvpx vp9/encoder/vp9_bitstream.c reads cpi->refresh_{last,golden,
	// alt_ref}_frame to emit RefreshFrameFlags on inter frames. Those
	// fields are written by set_ext_overrides (vp9_encoder.c:4761-4775)
	// from cpi->ext_refresh_{last,golden,alt_ref}_frame when the
	// caller-supplied vpx_enc_frame_flags_t armed
	// ext_refresh_frame_flags_pending via vp9_apply_encoding_flags
	// (vp9_encoder.c:6826-6838 -> vp9_update_reference at 2954-2959).
	if mask, ok := e.vp9ExtOverrideRefreshMask(); ok {
		return mask
	}
	// The one-pass VBR golden refresh bit is now armed by
	// setGFUpdateOnePassVBR via rc.refreshGoldenFrame (caller ORs it into the
	// mask), mirroring libvpx's begin-of-frame vp9_set_gf_update_one_pass_vbr.
	return vp9InterRefreshFrameFlags(flags)
}

func vp9InterFrameContextIdx(refreshFlags uint8) uint8 {
	if refreshFlags&(1<<vp9AltRefSlot) != 0 {
		return 1
	}
	return 0
}

func (e *VP9Encoder) vp9OnePassVBRSourceAltRefOverlay(inter *vp9InterEncodeState) bool {
	return e != nil && inter != nil && inter.isSrcFrameAltRef &&
		e.opts.LookaheadFrames > 0 &&
		e.opts.RateControlModeSet &&
		e.opts.RateControlMode == RateControlVBR
}

// vp9InterRefSignBias mirrors libvpx set_ref_sign_bias
// (vp9/encoder/vp9_encoder.c:4806-4821), invoked from
// encode_frame_to_data_rate (vp9_encoder.c:5297) for every non
// show_existing_frame. libvpx computes the per-frame sign bias as
//
//	cm->ref_frame_sign_bias[ref_frame] =
//	    cur_frame_index < ref_cnt_buf->frame_index;
//
// where cur_frame_index is the current frame's buffer frame_index and
// ref_cnt_buf->frame_index is the frame_index stamped on the referenced
// buffer when it was created (set_frame_index, vp9_encoder.c:5029-5038:
// frame_index = current_video_frame + arf_src_offset). For the one-pass
// realtime / non-RD path arf_src_offset is 0, so frame_index ==
// current_video_frame and the bias is set iff the current frame indexes a
// reference buffer stamped with a later video frame number. govpx stamps
// each ref slot with the encoder's frameIndex (current_video_frame) at
// refresh time, so this reproduces the libvpx comparison exactly.
//
// Note: this is NOT the FORCE_ARF flag — libvpx derives the bias purely
// from frame ordering, so a buffer refreshed under EncodeForceAltRefFrame
// in display order still gets sign bias 0 when later referenced.
func (e *VP9Encoder) vp9InterRefSignBias(flags EncodeFlags) [3]uint8 {
	cur := e.frameIndex
	var bias [3]uint8
	for i, slot := range [3]int{vp9LastRefSlot, vp9GoldenRefSlot, vp9AltRefSlot} {
		// libvpx guards on ref_cnt_buf != NULL; an unrefreshed slot keeps
		// the previous value (which defaults to 0 for VP9_COMMON).
		if e.refValid[slot] && cur < e.refFrameIndex[slot] {
			bias[i] = 1
		}
	}
	return bias
}

func vp9EncoderTileInfo(miCols, threads int, log2TileRows int8) vp9dec.TileInfo {
	return vp9EncoderTileInfoForTargetLevel(miCols, 0, 0, threads,
		log2TileRows, VP9TargetLevelUnconstrained)
}

func vp9EncoderTileInfoForTargetLevel(miCols, width, height, threads int, log2TileRows int8, targetLevel int) vp9dec.TileInfo {
	minLog2, maxLog2 := vp9dec.TileNBits(miCols)
	log2Cols := minLog2
	if threads > 1 {
		log2Cols = max(log2Cols, vp9CeilLog2(threads))
	}
	log2Cols = min(log2Cols, maxLog2)
	log2Cols = vp9TargetLevelClampLog2TileCols(targetLevel, width, height,
		minLog2, log2Cols)
	return vp9dec.TileInfo{
		Log2TileCols: log2Cols,
		Log2TileRows: int(log2TileRows),
	}
}

func vp9CeilLog2(v int) int {
	if v <= 1 {
		return 0
	}
	n := 0
	p := 1
	for p < v {
		p <<= 1
		n++
	}
	return n
}

func vp9EncoderReferenceSlot(refFrame int8) (int, bool) {
	switch refFrame {
	case vp9dec.LastFrame:
		return vp9LastRefSlot, true
	case vp9dec.GoldenFrame:
		return vp9GoldenRefSlot, true
	case vp9dec.AltrefFrame:
		return vp9AltRefSlot, true
	default:
		return 0, false
	}
}

func validateVP9EncodeFlags(flags EncodeFlags) error {
	flags = normalizeVP9EncodeFlags(flags)
	if err := validateEncodeFlags(flags); err != nil {
		return err
	}
	return nil
}

func normalizeVP9EncodeFlags(flags EncodeFlags) EncodeFlags {
	if flags&EncodeForceGoldenFrame != 0 {
		flags &^= EncodeNoUpdateGolden
	}
	if flags&EncodeForceAltRefFrame != 0 {
		flags &^= EncodeNoUpdateAltRef
	}
	return flags
}

func (e *VP9Encoder) vp9ShouldEncodeKeyFrame(flags EncodeFlags) bool {
	if e == nil || e.closed {
		return false
	}
	if flags&EncodeForceKeyFrame != 0 {
		return true
	}
	if e.vp9DeadlineModeChanged() {
		return true
	}
	return e.IsKeyFrameNext()
}

func (e *VP9Encoder) hasVP9UsableInterReference(flags EncodeFlags) bool {
	mask := e.vp9InterReferenceMaskForFrame(flags)
	for _, refFrame := range [...]int8{vp9dec.LastFrame, vp9dec.GoldenFrame, vp9dec.AltrefFrame} {
		slot, ok := vp9EncoderReferenceSlot(refFrame)
		if ok && mask&(1<<uint(refFrame)) != 0 && e.refFrames[slot].valid {
			return true
		}
	}
	return false
}

func (e *VP9Encoder) validateVP9InterSegmentationReferences(flags EncodeFlags) error {
	seg := e.opts.Segmentation
	if !seg.Enabled {
		return nil
	}
	mask := e.vp9InterReferenceMaskForFrame(flags)
	for i := range VP9MaxSegments {
		if !seg.RefFrameEnabled[i] {
			continue
		}
		refFrame := seg.RefFrame[i]
		if refFrame == vp9dec.IntraFrame {
			continue
		}
		if mask&(1<<uint(refFrame)) == 0 {
			return ErrInvalidConfig
		}
		slot, ok := vp9EncoderReferenceSlot(refFrame)
		if !ok || !e.refFrames[slot].valid {
			return ErrInvalidConfig
		}
	}
	return nil
}

func (e *VP9Encoder) vp9RefDims(slot uint8) (uint32, uint32) {
	idx := int(slot)
	if idx < len(e.refValid) && e.refValid[idx] {
		return e.refWidth[idx], e.refHeight[idx]
	}
	return uint32(e.opts.Width), uint32(e.opts.Height)
}

func (e *VP9Encoder) refreshVP9EncoderRefs(header *vp9dec.UncompressedHeader, flags EncodeFlags) {
	refreshFlags := header.RefreshFrameFlags
	if refreshFlags != 0 {
		e.subpelRefBorderedValid = false
	}
	refMapID := 0
	if refreshFlags != 0 {
		e.nextRefMapID++
		refMapID = e.nextRefMapID
	}
	for slot := range e.refValid {
		if refreshFlags&(1<<uint(slot)) == 0 {
			continue
		}
		e.refWidth[slot] = header.Width
		e.refHeight[slot] = header.Height
		e.refValid[slot] = true
		e.refMap[slot] = refMapID
		// libvpx set_frame_index (vp9_encoder.c:5029-5038) stamps the freshly
		// reconstructed buffer with frame_index = current_video_frame +
		// arf_src_offset. arf_src_offset is 0 on the one-pass realtime /
		// non-RD path, so the stamp is just the current video frame number.
		// vp9InterRefSignBias reads this back to reproduce set_ref_sign_bias.
		e.refFrameIndex[slot] = e.frameIndex
		if e.reconFrame.Width != 0 && e.reconFrame.Height != 0 {
			e.refFrames[slot].store(e.reconFrame)
		}
	}
	// After the reconstruction has been stored into the ref slots, rebuild
	// the border-padded LAST_FRAME mirror that choose_partitioning's low_res
	// int_pro path reads against. Mirrors libvpx's post-reconstruction
	// vpx_extend_frame_borders call (vp9/encoder/vp9_encoder.c:3424 /
	// 3470 — extend_borders after the frame is reconstructed for the
	// realtime path).
	e.ensureLastBordered()
}

// ensureLastBordered (re)builds the encoder's border-padded LAST_FRAME luma
// mirror from the current contents of e.refFrames[vp9LastRefSlot]. Called
// at the end of refreshVP9EncoderRefs so the next frame's
// choose_partitioning sees a libvpx-shaped padded LAST plane that
// vp9_int_pro_motion_estimation can read up to (bw>>1) pixels before the
// SB origin (libvpx vp9/encoder/vp9_mcomp.c:2317-2320).
//
// libvpx counterpart: vpx_extend_frame_borders_c
// (vpx_scale/generic/yv12extend.c:130-171) invoked after each
// reconstructed frame is stored into the YV12_BUFFER_CONFIG.
func (e *VP9Encoder) ensureLastBordered() {
	if !e.refFrames[vp9LastRefSlot].valid {
		e.lastBorderedValid = false
		return
	}
	plane, stride, w, h := vp9ReferenceVisiblePlane(&e.refFrames[vp9LastRefSlot], 0)
	if len(plane) == 0 || stride <= 0 || w <= 0 || h <= 0 {
		e.lastBorderedValid = false
		return
	}
	common.YV12BuildBorderedPlane(&e.lastBordered, plane, stride, w, h,
		common.VP9EncBorderInPixels)
	e.lastBorderedValid = true
}

func (e *VP9Encoder) refreshVP9EncoderMvRefs(isKey bool, miRows, miCols int) {
	if isKey {
		e.prevFrameMvsValid = false
		e.prevFrameMvRows = 0
		e.prevFrameMvCols = 0
		return
	}
	need := miRows * miCols
	e.prevFrameMvs = buffers.EnsureLen(e.prevFrameMvs, need)
	for i := range need {
		mi := e.miGrid[i]
		e.prevFrameMvs[i] = vp9dec.MvRef{RefFrame: mi.RefFrame, Mv: mi.Mv}
	}
	e.prevFrameMvRows = miRows
	e.prevFrameMvCols = miCols
	e.prevFrameMvsValid = true
}

func (e *VP9Encoder) refreshVP9EncoderSegmentMap(miRows, miCols int) {
	need := miRows * miCols
	if need <= 0 || len(e.miGrid) < need {
		e.prevSegmentMapValid = false
		e.prevSegmentMapRows = 0
		e.prevSegmentMapCols = 0
		return
	}
	e.prevSegmentMap = buffers.EnsureLen(e.prevSegmentMap, need)
	for i := range need {
		e.prevSegmentMap[i] = e.miGrid[i].SegmentID
	}
	e.prevSegmentMapRows = miRows
	e.prevSegmentMapCols = miCols
	e.prevSegmentMapValid = true
}

func (e *VP9Encoder) useVP9EncoderPrevFrameMvs(miRows, miCols int) bool {
	return e.prevFrameMvsValid &&
		!e.opts.ErrorResilient &&
		e.prevFrameMvRows == miRows &&
		e.prevFrameMvCols == miCols &&
		len(e.prevFrameMvs) >= miRows*miCols
}

func (e *VP9Encoder) useVP9EncoderPrevSegmentMap(miRows, miCols int) bool {
	return e.prevSegmentMapValid &&
		e.prevSegmentMapRows == miRows &&
		e.prevSegmentMapCols == miCols &&
		len(e.prevSegmentMap) >= miRows*miCols
}

func (e *VP9Encoder) ensureVP9EncoderModeBuffers(miRows, miCols int) {
	miColsAligned := common.AlignToSB(miCols)
	e.aboveSegCtx = buffers.EnsureLenZeroed(e.aboveSegCtx, miColsAligned)
	e.leftSegCtx = buffers.EnsureLen(e.leftSegCtx, common.MiBlockSize)
	miGridLen := miRows * miCols
	e.miGrid = buffers.EnsureLenZeroed(e.miGrid, miGridLen)
	// varPartGrid / varPartSBComputed are allocated lazily inside
	// vp9EnsureSBPartitionChosen so the steady-state encode path
	// (which currently does not invoke the libvpx choose_partitioning
	// port) pays no allocation cost. Reset the frame-validity flag
	// and per-SB computed mask in place when state already exists; the
	// reset MUST happen here (once per frame) and never on each per-MI
	// vp9EnsureSBPartitionChosen call, because the picker stamps the
	// partition tree into varPartGrid for every SB in the frame and a
	// per-call wipe would lose decisions for SBs the walker re-visits.
	if cap(e.varPartGrid) >= miGridLen {
		e.varPartGrid = e.varPartGrid[:miGridLen]
		for i := range e.varPartGrid {
			e.varPartGrid[i] = vp9dec.NeighborMi{}
		}
	}
	sbCount := ((miRows + 7) >> 3) * ((miCols + 7) >> 3)
	if cap(e.varPartSBComputed) >= sbCount {
		e.varPartSBComputed = e.varPartSBComputed[:sbCount]
		for i := range e.varPartSBComputed {
			e.varPartSBComputed[i] = false
		}
	}
	if cap(e.varPartSBUseMvPart) >= sbCount {
		e.varPartSBUseMvPart = e.varPartSBUseMvPart[:sbCount]
		for i := range e.varPartSBUseMvPart {
			e.varPartSBUseMvPart[i] = false
		}
	}
	if cap(e.varPartSBMvPart) >= sbCount {
		e.varPartSBMvPart = e.varPartSBMvPart[:sbCount]
	}
	if cap(e.varPartSBPredValid) >= sbCount {
		e.varPartSBPredValid = e.varPartSBPredValid[:sbCount]
		for i := range e.varPartSBPredValid {
			e.varPartSBPredValid[i] = false
		}
	}
	if cap(e.varPartSBPredLast) >= sbCount {
		e.varPartSBPredLast = e.varPartSBPredLast[:sbCount]
	}
	if cap(e.varPartSBVarLow) >= sbCount {
		e.varPartSBVarLow = e.varPartSBVarLow[:sbCount]
		for i := range e.varPartSBVarLow {
			e.varPartSBVarLow[i] = [25]uint8{}
		}
	}
	if cap(e.varPartSBContentStateValid) >= sbCount {
		e.varPartSBContentStateValid = e.varPartSBContentStateValid[:sbCount]
		for i := range e.varPartSBContentStateValid {
			e.varPartSBContentStateValid[i] = false
		}
	}
	if cap(e.varPartSBContentState) >= sbCount {
		e.varPartSBContentState = e.varPartSBContentState[:sbCount]
	}
	if cap(e.varPartSBZeroTempSADSource) >= sbCount {
		e.varPartSBZeroTempSADSource = e.varPartSBZeroTempSADSource[:sbCount]
	}
	if cap(e.varPartSBColorSensitivity) >= sbCount {
		e.varPartSBColorSensitivity = e.varPartSBColorSensitivity[:sbCount]
		for i := range e.varPartSBColorSensitivity {
			e.varPartSBColorSensitivity[i] = [2]bool{}
		}
	}
	if cap(e.varPartSBLastHighContentValid) >= sbCount {
		e.varPartSBLastHighContentValid = e.varPartSBLastHighContentValid[:sbCount]
		for i := range e.varPartSBLastHighContentValid {
			e.varPartSBLastHighContentValid[i] = false
		}
	}
	if cap(e.varPartSBLastHighContent) >= sbCount {
		e.varPartSBLastHighContent = e.varPartSBLastHighContent[:sbCount]
	}
	e.varPartFrameValid = false
	// Invalidate the per-frame border-padded source mirror so the next
	// choose_partitioning inter call rebuilds it from the current frame's
	// source plane. The padded LAST mirror (e.lastBordered) is rebuilt at
	// end-of-frame inside refreshVP9EncoderRefs, not here.
	e.intProSrcBorderedValid = false
	// ML_BASED_PARTITION's per-SB context cache must be reset per frame
	// (libvpx vp9_encodeframe.c:5314 — get_estimated_pred fills x->est_pred
	// fresh for every SB on every frame). See vp9_nonrd_pick_partition.go.
	e.vp9ResetMLPartitionCache(miRows, miCols)
	e.ensureVP9LeafInterDecisionCache(miRows, miCols)
	e.ensureVP9LeafKeyframeDecisionCache(miRows, miCols)
	e.ensureVP9KeyframePartitionDecisionCache(miRows, miCols)
	e.partitionReconScratch = buffers.EnsureLen(e.partitionReconScratch, vp9MaxPartitionReconScratchStack)
	e.partitionReconScratchTop = 0
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		aboveLen := vp9dec.PlaneEntropyLen(miColsAligned, pd.SubsamplingX)
		leftLen := vp9dec.PlaneEntropyLen(common.MiBlockSize, pd.SubsamplingY)
		pd.AboveContext = buffers.EnsureLen(pd.AboveContext, aboveLen)
		pd.LeftContext = buffers.EnsureLen(pd.LeftContext, leftLen)
	}
}

func (e *VP9Encoder) resetVP9EncoderAboveEntropyContexts() {
	for plane := range vp9dec.MaxMbPlane {
		ctx := e.planes[plane].AboveContext
		for i := range ctx {
			ctx[i] = 0
		}
	}
}

func (e *VP9Encoder) resetVP9EncoderLeftEntropyContexts() {
	for plane := range vp9dec.MaxMbPlane {
		ctx := e.planes[plane].LeftContext
		for i := range ctx {
			ctx[i] = 0
		}
	}
}

func (e *VP9Encoder) vp9EncoderPlaneContextOffsets(miRow, miCol int) (
	above [vp9dec.MaxMbPlane]int, left [vp9dec.MaxMbPlane]int,
) {
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		above[plane] = (miCol * 2) >> pd.SubsamplingX
		left[plane] = ((miRow * 2) >> pd.SubsamplingY) % len(pd.LeftContext)
	}
	return above, left
}

func (e *VP9Encoder) prepareVP9EncoderOutputFrame(width, height int) {
	layout := common.NewFrameLayout(width, height)
	e.reconYFull = buffers.EnsureAlignedCapacity(e.reconYFull, layout.YFullLen, 32)
	e.reconUFull = buffers.EnsureAlignedCapacity(e.reconUFull, layout.UVFullLen, 32)
	e.reconVFull = buffers.EnsureAlignedCapacity(e.reconVFull, layout.UVFullLen, 32)
	buffers.Fill(e.reconYFull, 128)
	buffers.Fill(e.reconUFull, 128)
	buffers.Fill(e.reconVFull, 128)
	e.reconY = e.reconYFull[layout.YOrigin:]
	e.reconU = e.reconUFull[layout.UVOrigin:]
	e.reconV = e.reconVFull[layout.UVOrigin:]
	e.reconFrame = Image{
		Width:   width,
		Height:  height,
		Y:       e.reconY,
		U:       e.reconU,
		V:       e.reconV,
		YStride: layout.YStride,
		UStride: layout.UVStride,
		VStride: layout.UVStride,
	}
}

func (e *VP9Encoder) resetVP9EncoderCodingState(width, height int) {
	e.prepareVP9EncoderOutputFrame(width, height)
	for i := range e.aboveSegCtx {
		e.aboveSegCtx[i] = 0
	}
	for i := range e.leftSegCtx {
		e.leftSegCtx[i] = 0
	}
	for i := range e.miGrid {
		e.miGrid[i] = vp9dec.NeighborMi{}
	}
	e.resetVP9EncoderAboveEntropyContexts()
	e.resetVP9EncoderLeftEntropyContexts()
}
