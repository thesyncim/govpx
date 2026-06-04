package govpx

import (
	"image"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	vpxrc "github.com/thesyncim/govpx/internal/vpx/ratecontrol"
)

func (e *VP9Encoder) encodeVP9FrameIntoWithFlagsResultInternal(img *image.YCbCr, dst []byte, flags EncodeFlags, forceIntraOnly bool, temporalFrame temporalFrame, forceFirstInterLayer bool, isSrcFrameAltRef bool) (result VP9EncodeResult, err error) {
	if e == nil || e.closed {
		return VP9EncodeResult{}, ErrClosed
	}
	flags = normalizeVP9EncodeFlags(flags)
	if err := validateVP9EncodeFlags(flags); err != nil {
		return VP9EncodeResult{}, err
	}
	// libvpx vp9/vp9_cx_iface.c:1408 vp9_apply_encoding_flags(cpi, flags)
	// -> vp9/encoder/vp9_encoder.c:6812-6843 maps the EncodeFlags bitset
	// onto cpi->ext_refresh_{last,golden,alt_ref}_frame via
	// vp9_update_reference and cpi->ext_refresh_frame_context via
	// vp9_update_entropy.
	e.vp9ApplyEncodingFlags(flags)
	// libvpx vp9/encoder/vp9_encoder.c:5284 set_ext_overrides runs at the
	// start of encode_frame_to_data_rate. It copies cpi->ext_refresh_*
	// onto cpi->refresh_{last,golden,alt_ref}_frame and
	// cm->refresh_frame_context. govpx mirrors the latch here so the
	// downstream RefreshFrameFlags computation and frame-context
	// commitment read the post-override state.
	e.setExtOverrides()
	if forceIntraOnly {
		if flags&EncodeForceKeyFrame != 0 {
			return VP9EncodeResult{}, ErrInvalidConfig
		}
		if e.frameIndex == 0 {
			return VP9EncodeResult{}, ErrInvalidConfig
		}
		flags |= EncodeInvisibleFrame
	}
	if err := e.validateVP9EncoderSource(img); err != nil {
		return VP9EncodeResult{}, err
	}
	if len(dst) < vp9MinEncodeIntoBuffer {
		return VP9EncodeResult{}, ErrBufferTooSmall
	}
	img = e.prepareVP9DenoiserSource(img)

	width := uint32(e.opts.Width)
	height := uint32(e.opts.Height)
	miCols := int((width + 7) >> 3)
	miRows := int((height + 7) >> 3)
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.ensureVP9EncoderModeBuffers(miRows, miCols)

	isKey := e.vp9ShouldEncodeKeyFrame(flags)
	intraOnly := forceIntraOnly
	if intraOnly {
		isKey = false
	}
	if !isKey && !intraOnly &&
		e.shouldEncodeVP9SceneCutKeyFrame(img, flags, temporalFrame.Enabled,
			encoderMacroblockRows(e.opts.Height), encoderMacroblockCols(e.opts.Width)) {
		isKey = true
	}
	if forceFirstInterLayer && isKey && e.frameIndex == 0 &&
		!e.forceKeyFrame && flags&EncodeForceKeyFrame == 0 &&
		e.hasVP9UsableInterReference(flags) {
		e.resetVP9EncoderFrameContexts()
		isKey = false
	}
	if !isKey && !intraOnly && !e.hasVP9UsableInterReference(flags) &&
		!vp9AllInterReferencesDisabled(flags) {
		isKey = true
	}
	if !isKey && !intraOnly {
		if err := e.validateVP9InterSegmentationReferences(flags); err != nil {
			return VP9EncodeResult{}, err
		}
	}
	// libvpx vp9/encoder/vp9_encoder.c:5444 forces cpi->refresh_last_frame=1
	// on every KEY_FRAME after set_ext_overrides has copied the user-supplied
	// ext_refresh_*_frame fields, and vp9_encoder.c:856-858 forces
	// refresh_golden_frame=1 / refresh_alt_ref_frame=1 inside check_show_existing.
	// The net effect is that any EncodeNoUpdate{Last,Golden,AltRef} hint passed
	// with EncodeForceKeyFrame is SILENTLY IGNORED — it is not a "Conflicting
	// flags." error. govpx writes header.RefreshFrameFlags = 0xff on KEY_FRAMEs
	// at vp9_encoder.go:2593 unconditionally, mirroring this, so accepting
	// NoUpdate bits on key frames yields the same bitstream as libvpx.
	if intraOnly && vp9InterRefreshFrameFlags(flags) == 0 {
		return VP9EncodeResult{}, ErrInvalidConfig
	}
	// libvpx: vp9/encoder/vp9_encoder.c:6347-6364 — when
	// VP9E_SET_KEY_FRAME_FILTERING is enabled and the other libvpx
	// preconditions hold (non-realtime mode, non-lossless, single-pass,
	// non-SVC, ARNRMaxFrames>0, ARNRStrength>0, speed<2), run
	// vp9_temporal_filter(cpi, -1) on the keyframe source against the
	// forward lookahead window and substitute the filtered buffer for
	// the per-frame encode.  govpx's gate helper checks the same set;
	// when any gate trips we fall through to the raw source.
	if isKey && e.vp9KeyFrameFilteringActive() {
		img = e.applyVP9KeyFrameFilter(img)
	}
	showFrame := flags&EncodeInvisibleFrame == 0
	srcFrameAltRef := isSrcFrameAltRef && showFrame && !isKey && !intraOnly
	e.rc.isSrcFrameAltRef = srcFrameAltRef
	e.rc.seedFramesToKey(e.opts.MaxKeyframeInterval, isKey)
	e.rc.prepareOnePassCBRCyclicGoldenFrame(isKey, intraOnly,
		e.opts.AQMode, &e.cyclicAQ, e.opts.GFCBRBoostPct,
		e.extRefresh.flagsPending)
	// libvpx vp9_rc_get_one_pass_vbr_params (vp9_ratectrl.c:2143) runs
	// vp9_set_gf_update_one_pass_vbr for every frame (key or inter): when the
	// golden countdown reaches zero it recomputes the GF interval / af_ratio /
	// gfu_boost for the new group, re-seeds the countdown, and arms
	// refresh_golden_frame. The external-refresh override path keeps the
	// caller-supplied mask authoritative.
	if e.rc.enabled && e.rc.mode != RateControlCBR && !intraOnly &&
		!e.twoPass.enabled() && flags&vp9ExternalRefreshCtlFlags == 0 {
		e.rc.refreshGoldenFrame = false
		e.rc.setGFUpdateOnePassVBR(e.frameIndex)
	}
	refreshFlags := uint8(0xff)
	if !isKey {
		refreshFlags = e.vp9InterRefreshFrameFlags(flags)
		if e.rc.refreshGoldenFrame && flags&vp9ExternalRefreshCtlFlags == 0 {
			refreshFlags |= 1 << vp9GoldenRefSlot
		}
		if srcFrameAltRef && flags&vp9ExternalRefreshCtlFlags == 0 {
			// libvpx check_src_altref(): overlay/source-altref frames
			// preserve LAST and become GOLDEN for subsequent frames.
			refreshFlags &^= 1 << vp9LastRefSlot
			refreshFlags |= 1 << vp9GoldenRefSlot
		}
	}
	e.rc.beginFrameWithRefresh(isKey || intraOnly, e.frameIndex,
		refreshFlags)
	e.rc.preEncodeFrame(showFrame)
	e.vp9TwoPassFrameTarget = 0
	e.vp9SceneDetectionOnePass(img, showFrame, miRows, miCols)
	e.vp9UpdateNoiseEstimate(img, miRows, miCols, isKey || intraOnly)
	if !isKey && !intraOnly && showFrame && !e.rc.highSourceSAD {
		dropReason, dropFrame := e.rc.testDropInterFrame()
		if dropFrame {
			e.rc.postDropFrame()
			e.lastFrameDropped = true
			e.temporal.finishDroppedFrame(temporalFrame, e.vp9TemporalBufferConfig())
			firstPassStats := e.twoPass.statsForFrame()
			e.twoPass.finishFrame()
			if vp9OracleTraceBuild {
				e.emitVP9OracleDroppedFrameTrace(flags, width, height, temporalFrame, dropReason)
			}
			e.vp9FinishKeyFrameDistance(false)
			e.frameIndex++
			e.vp9LatchDeadlineModePreviousFrame()
			spatialLayerID, spatialLayerCount, interLayerDependency,
				notRefForUpperSpatialLayer, scalabilityStructurePresent,
				spatialScalabilityStructure := e.vp9SpatialResultFields()
			return VP9EncodeResult{
				Dropped:                     true,
				ShowFrame:                   true,
				TargetBitrateKbps:           e.rc.targetBitrateKbps,
				FrameTargetBits:             e.rc.frameTargetBits,
				BufferLevelBits:             e.rc.bufferLevelBits,
				FirstPassStats:              firstPassStats,
				TemporalLayerID:             temporalFrame.LayerID,
				TemporalLayerCount:          temporalFrame.LayerCount,
				TemporalLayerSync:           temporalFrame.LayerSync,
				TL0PICIDX:                   temporalFrame.TL0PICIDX,
				SpatialLayerID:              spatialLayerID,
				SpatialLayerCount:           spatialLayerCount,
				InterLayerDependency:        interLayerDependency,
				NotRefForUpperSpatialLayer:  notRefForUpperSpatialLayer,
				ScalabilityStructurePresent: scalabilityStructurePresent,
				SpatialScalabilityStructure: spatialScalabilityStructure,
			}, nil
		}
	}
	e.prepareVP9EncoderOutputFrame(int(width), int(height))

	header := &e.vp9HeaderScratch
	*header = vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		ShowFrame:             showFrame,
		ErrorResilientMode:    e.opts.ErrorResilient,
		IntraOnly:             intraOnly,
		Width:                 width,
		Height:                height,
		RefreshFrameContext:   flags&EncodeNoUpdateEntropy == 0,
		FrameParallelDecoding: e.vp9FrameParallelDecodingMode(),
		FrameContextIdx:       0,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:   vp9dec.Bits8,
			ColorSpace: vp9CommonColorSpace(e.opts.ColorSpace),
			ColorRange: vp9CommonColorRange(e.opts.ColorRange),
		},
	}
	if rw, rh := e.opts.RenderWidth, e.opts.RenderHeight; rw > 0 && rh > 0 {
		header.Render = vp9dec.RenderSize{
			Width:  uint32(rw),
			Height: uint32(rh),
		}
	} else {
		header.Render = vp9dec.RenderSize{Width: width, Height: height}
	}
	header.Tile = vp9EncoderTileInfoForTargetLevel(miCols, int(width), int(height), e.opts.Threads,
		e.opts.Log2TileRows, e.opts.TargetLevel)
	macroblocks := encoder.MacroblockCount(miRows, miCols)
	// TPL runs before the qindex is finalised so its per-SB rdmult delta
	// can scale the keyframe mode picker's Lagrangian search.  Unlike
	// the deleted scalar bias path, libvpx's TPL does NOT touch the
	// regulated qindex — it routes through cb_rdmult inside the per-SB
	// partition search (vp9_encodeframe.c:4245-4248).  The pass fires on
	// visible frames whenever a populated source-order lookahead window
	// is available; alt-ref / hidden frames are excluded for parity
	// with libvpx's restriction.
	e.populateVP9TPLForFrame(!showFrame || flags&EncodeForceAltRefFrame != 0, img)
	e.vp9LatchCyclicResizeForFrame(isKey, intraOnly)
	e.vp9UpdateCyclicRefreshParameters(isKey, intraOnly, showFrame, miRows, miCols,
		macroblocks, refreshFlags, header.Quant.Lossless)
	qindex := e.vp9EncoderFrameQIndex(isKey, header.IntraOnly, flags,
		refreshFlags, macroblocks)
	if e.rc.enabled {
		e.vp9ModeDecisionQIndex = uint8(qindex)
		e.vp9ModeDecisionQIndexSet = true
		defer func() {
			e.vp9ModeDecisionQIndexSet = false
		}()
	}
	// libvpx: vp9/encoder/vp9_rd.c:396-407 vp9_initialize_rd_consts.
	// rd->RDDIV = RDDIV_BITS; rd->RDMULT = vp9_compute_rd_mult(...).
	// govpx's frame-type bucket replays the libvpx branching: KF wins,
	// then a non-srcframe-altref ARF/GF refresh, else inter.  The
	// per-SB cb_rdmult cache cleared inside vp9EncoderInitializeRDConsts
	// matches libvpx's reset before each rd_pick_sb_modes invocation.
	{
		refreshGolden := refreshFlags&(1<<vp9GoldenRefSlot) != 0
		refreshAlt := refreshFlags&(1<<vp9AltRefSlot) != 0
		rdFrameType := encoder.RDFrameTypeFor(isKey, srcFrameAltRef, refreshGolden,
			refreshAlt)
		e.vp9EncoderInitializeRDConsts(qindex, rdFrameType)
		// libvpx vp9/encoder/vp9_encoder.c:3754 / 3765 call
		// set_speed_features_framesize_independent +
		// set_speed_features_framesize_dependent (via
		// set_size_independent_vars / set_size_dependent_vars) on every
		// frame from encode_frame_to_data_rate / encode_with_recode_loop
		// at vp9_encoder.c:4169-4170 and 4377-4392. The SF refresh
		// sees the per-frame (frame_type, intra_only, refresh_*_frame,
		// is_src_frame_alt_ref, base_qindex) tuple — the same triple
		// frame_is_kf_gf_arf / frame_is_boosted consume — so
		// sf.tx_size_search_method and sf.use_nonrd_pick_mode track the
		// live frame state. govpx previously pinned e.sf at compressor
		// create time which left non-key non-intra-only frames reading
		// the keyframe-context value; the per-frame call here closes
		// that gap.
		e.vp9ApplySpeedFeatures(e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
			IsKey:              isKey,
			IntraOnly:          intraOnly,
			ShowFrame:          showFrame,
			RefreshGoldenFrame: refreshGolden,
			RefreshAltRefFrame: refreshAlt,
			IsSrcFrameAltRef:   srcFrameAltRef,
			BaseQIndex:         qindex,
		}))
		e.vp9CarryPostEncodeDroppedSceneChange()
	}
	header.Quant.BaseQindex = int16(qindex)
	header.Quant.UvDcDeltaQ = int8(e.opts.DeltaQUV)
	header.Quant.UvAcDeltaQ = int8(e.opts.DeltaQUV)
	header.Quant.Lossless = qindex == 0 &&
		header.Quant.YDcDeltaQ == 0 &&
		header.Quant.UvDcDeltaQ == 0 &&
		header.Quant.UvAcDeltaQ == 0
	resetLoopfilterDeltas := isKey || intraOnly || e.opts.ErrorResilient
	// libvpx vp9_picklpf.c:159 — the picker reads sf.lpf_pick to
	// choose between LPF_PICK_FROM_FULL_IMAGE (default at speeds
	// 0-2), LPF_PICK_FROM_Q (speed 3+), and LPF_PICK_MINIMAL_LPF.
	//
	// govpx writes a placeholder FilterLevel (the closed-form FROM_Q
	// value) into the uncompressed header before tile encoding; once
	// the tiles populate the reconstruction buffer the full-image /
	// sub-image search runs (vp9EncoderRunFullImagePicker below) and
	// the uncompressed header is re-written in place with the picked
	// level. The filter_level field is a 6-bit literal at a stable
	// bit position (internal/vp9/encoder/header_writer.go:384
	// EncodeLoopfilterWithPrev), so the byte length of the
	// uncompressed header is invariant under filter_level and the
	// re-write keeps compressed_header / tile offsets stable. This
	// matches libvpx's order at vp9_encoder.c:5391-5467
	// (encode_with_recode_loop → loopfilter_frame → vp9_pack_bitstream).
	header.Loopfilter = e.vp9EncoderLoopFilterParams(qindex, isKey, intraOnly,
		resetLoopfilterDeltas, header.Quant.Lossless,
		e.vp9SegEnabledForLoopfilter(isKey, intraOnly), e.opts.Sharpness,
		e.opts.Width, e.opts.Height, common.TxModeSelect)
	if vp9DisableLoopfilterForFrame(e.opts.DisableLoopfilter, isKey) {
		header.Loopfilter.FilterLevel = 0
	}
	if isKey {
		header.FrameType = common.KeyFrame
		header.RefreshFrameFlags = 0xff
	} else if intraOnly {
		header.FrameType = common.InterFrame
		if flags&EncodeNoUpdateEntropy == 0 {
			header.ResetFrameContext = 2
		}
		header.RefreshFrameFlags = refreshFlags
	} else {
		header.FrameType = common.InterFrame
		header.RefreshFrameFlags = refreshFlags
		header.FrameContextIdx = vp9InterFrameContextIdx(header.RefreshFrameFlags)
		header.InterRef.RefIndex = [3]uint8{
			vp9LastRefSlot,
			vp9GoldenRefSlot,
			vp9AltRefSlot,
		}
		header.InterRef.SignBias = e.vp9InterRefSignBias(flags)
	}
	restoreFrameContext := e.opts.ErrorResilient || flags&EncodeNoUpdateEntropy != 0
	shouldRestoreFrameContexts := isKey || intraOnly || e.opts.ErrorResilient || restoreFrameContext
	var frameContextsSeed [common.FrameContexts]vp9dec.FrameContext
	var frameContextSeed vp9dec.FrameContext
	frameContextIdx := e.prepareVP9EncoderFrameContext(header)
	if shouldRestoreFrameContexts {
		frameContextsSeed = e.frameContexts
		frameContextSeed = e.fc
	}
	defer func() {
		if err == nil && !restoreFrameContext {
			return
		}
		if shouldRestoreFrameContexts {
			e.frameContexts = frameContextsSeed
			e.fc = frameContextSeed
			return
		}
		if frameContextIdx >= 0 && frameContextIdx < len(e.frameContexts) {
			e.fc = e.frameContexts[frameContextIdx]
		}
	}()
	// libvpx vp9/encoder/vp9_encoder.c:5355 calls save_encode_params once
	// per frame before the recode loop; vp9/encoder/vp9_encodeframe.c:5825
	// then calls restore_encode_params at the top of every vp9_encode_frame
	// so each recode iteration starts from the same prev snapshot. govpx
	// encodes each frame once, so save+restore collapses to a single
	// in-place pass, but the calls are ported verbatim to keep wire
	// behaviour identical when the recode loop is introduced.
	e.vp9SaveEncodeParamsFilterThreshes()
	e.vp9RestoreEncodeParamsFilterThreshes()
	header.InterpFilter = e.vp9EncoderFrameInterpFilter(isKey, header.IntraOnly,
		header.Quant.Lossless)
	interRefMask := e.vp9InterReferenceMaskForFrame(flags)
	if !isKey && !header.IntraOnly && interRefMask == 0 {
		header.InterpFilter = vp9dec.InterpSwitchable
	}
	// libvpx vp9/encoder/vp9_encodeframe.c:5876-5877 — when the frame
	// enters encode with cm->interp_filter == SWITCHABLE and the
	// frame_parameter_update speed feature is enabled, demote the frame
	// to the concrete EIGHTTAP / EIGHTTAP_SMOOTH / EIGHTTAP_SHARP that
	// won the previous frames' per-block 3-filter RD race
	// (filter_threshes accumulator). Skipped for intra-only frames
	// because the uncompressed-header writer omits the filter field for
	// those (internal/vp9/encoder/header_writer.go:196).
	if !isKey && !header.IntraOnly {
		refreshGolden := refreshFlags&(1<<vp9GoldenRefSlot) != 0
		refreshAlt := refreshFlags&(1<<vp9AltRefSlot) != 0
		header.InterpFilter = e.vp9DemoteSwitchableInterpFilter(
			header.InterpFilter, isKey, header.IntraOnly,
			srcFrameAltRef, refreshGolden, refreshAlt)
	}
	header.AllowHighPrecisionMv = vp9EncoderFrameAllowHighPrecisionMv(isKey, header.IntraOnly)
	e.updateVP9NonrdModeCostFrameContext(isKey || header.IntraOnly)
	nonrdModeCostFc := e.vp9NonrdModeCostFrameContext()

	txMode := e.vp9EncoderFrameTxMode(isKey, header.IntraOnly, header.Quant.Lossless)
	baseMi := vp9dec.NeighborMi{
		SbType: common.Block64x64,
		Mode:   common.DcPred,
		TxSize: common.TxModeToBiggestTxSize[txMode],
		Skip:   1,
		RefFrame: [2]int8{
			vp9dec.IntraFrame,
			vp9dec.NoRefFrame,
		},
	}
	// libvpx vp9/encoder/vp9_encodeframe.c:4336-4337 encodes the KEY_FRAME
	// && use_nonrd_pick_mode ALLOW_16X16 clamp inside select_tx_mode,
	// where it becomes baseMi.TxSize == Tx16x16 directly via
	// common.TxModeToBiggestTxSize[Allow16x16]. govpx previously layered a
	// redundant clamp on top; lifted now that vp9EncoderFrameTxMode ports
	// select_tx_mode verbatim.
	if !isKey && !intraOnly {
		baseMi.Mode = common.ZeroMv
		baseMi.InterpFilter = uint8(vp9dec.InterpEighttap)
		baseMi.RefFrame = [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame}
	}
	e.vp9PrepareCyclicRefreshFrame(isKey, intraOnly, showFrame, miRows, miCols,
		macroblocks, header, srcFrameAltRef, refreshFlags)
	if e.opts.AQMode == VP9AQPerceptual {
		e.perceptualAQ.PrepareFrame(img, int(header.Quant.BaseQindex), showFrame)
	}
	seg := e.vp9EncoderSegmentationParams(isKey || intraOnly,
		int(header.Quant.BaseQindex))
	e.vp9CarryActiveMapDisableSegmentation(&seg, isKey || intraOnly)

	dq := &e.dqScratch
	var keyState *vp9KeyframeEncodeState
	var interState *vp9InterEncodeState
	compoundAllowed := false
	referenceMode := vp9dec.SingleReference
	refSignBias := vp9dec.FrameRefSignBias(header)
	compoundRefs := vp9dec.SetupCompoundReferenceMode(refSignBias)
	vp9dec.SetupSegmentationDequant(&seg, vp9dec.SetupSegmentationDequantArgs{
		BaseQindex: int(header.Quant.BaseQindex),
		BitDepth:   vp9dec.Bits8,
	}, dq)
	if isKey {
		keyState = &vp9KeyframeEncodeState{
			img:      img,
			hdr:      header,
			dq:       dq,
			lossless: header.Quant.Lossless,
		}
	} else if intraOnly {
		keyState = &vp9KeyframeEncodeState{
			img:      img,
			hdr:      header,
			dq:       dq,
			lossless: header.Quant.Lossless,
		}
	} else {
		compoundAllowed = vp9dec.CompoundReferenceAllowed(refSignBias)
		if compoundAllowed {
			referenceMode = vp9dec.ReferenceModeSelect
		}
		interState = &vp9InterEncodeState{
			img:              img,
			dq:               dq,
			ref:              &e.refFrames[0],
			refMask:          interRefMask,
			allowHP:          header.AllowHighPrecisionMv,
			selectFc:         e.fc,
			modeCostFc:       nonrdModeCostFc,
			modeCostFcValid:  true,
			referenceMode:    referenceMode,
			compoundAllowed:  compoundAllowed,
			refSignBias:      refSignBias,
			compoundRefs:     compoundRefs,
			interpFilter:     header.InterpFilter,
			lossless:         header.Quant.Lossless,
			txMode:           txMode,
			baseQindex:       int(header.Quant.BaseQindex),
			isSrcFrameAltRef: srcFrameAltRef,
			showFrame:        showFrame,
		}
	}
	e.vp9ReuseStableSegmentationState(&seg, isKey || intraOnly, miRows, miCols,
		interState)
	header.Seg = seg
	e.resetVP9EncoderAboveEntropyContexts()

	// libvpx swaps in vp9_kf_partition_probs (not fc->partition_prob)
	// for keyframe / intra-only frames — see set_partition_probs in
	// vp9/common/vp9_onyxc_int.h. The two tables have the same shape
	// but different probabilities, so the bool stream desyncs if the
	// encoder uses the wrong one.
	partitionProbs := tables.KfPartitionProbs
	if !isKey && !intraOnly {
		partitionProbs = e.fc.PartitionProb
	}

	denoiserCountState := e.saveVP9DenoiserForCounts(interState)
	counts := e.collectVP9EncodeFrameCounts(int(width), int(height), miRows, miCols,
		header.Tile, &partitionProbs, &seg, baseMi, txMode, isKey, header.IntraOnly,
		keyState, interState)
	e.restoreVP9DenoiserAfterCounts(denoiserCountState)
	// libvpx vp9/encoder/vp9_encodeframe.c:5911 gates the post-encode
	// tx_mode demotion on cm->tx_mode == TX_MODE_SELECT. Only the
	// TX_MODE_SELECT partition-context ladder fires here; every other
	// tx_mode (including ALLOW_32X32 emitted by select_tx_mode for
	// USE_LARGESTALL, e.g. RT speed 1-4 keyframes) is written verbatim
	// with no demotion. vp9EncoderFrameTxModeFromCounts mirrors that
	// gate exactly — it returns the original txMode unchanged for
	// every non-TxModeSelect input, so a reducedTxMode != txMode here
	// can only be the libvpx-faithful TX_MODE_SELECT ladder firing.
	if reducedTxMode := vp9EncoderFrameTxModeFromCounts(txMode,
		header.Quant.Lossless, e.sf.FrameParameterUpdate != 0, counts); reducedTxMode != txMode {
		txMode = reducedTxMode
		baseMi.TxSize = common.TxModeToBiggestTxSize[txMode]
		e.clampVP9LeafDecisionTxSizes(baseMi.TxSize)
		if interState != nil {
			interState.txMode = txMode
		}
		denoiserCountState = e.saveVP9DenoiserForCounts(interState)
		counts = e.collectVP9EncodeFrameCounts(int(width), int(height), miRows, miCols,
			header.Tile, &partitionProbs, &seg, baseMi, txMode, isKey,
			header.IntraOnly, keyState, interState)
		e.restoreVP9DenoiserAfterCounts(denoiserCountState)
	}
	header.Seg = seg

	// libvpx vp9/encoder/vp9_bitstream.c:1312 — fix_interp_filter runs at
	// uncompressed-header write time, just before write_interp_filter and
	// before the compressed header is appended (libvpx vp9_bitstream.c:
	// 1425 then :1453). If exactly one filter has nonzero switchable
	// counts after the per-block RD pass, the frame header is demoted to
	// that filter so the bitstream omits the per-block filter bits.
	// govpx writes compressed first to size FirstPartitionSize, so we
	// apply the demotion here — between collectVP9EncodeFrameCounts and
	// WriteCompressedHeaderFromCounts — so the compressed-header
	// switchable_interp_probs update branch
	// (libvpx vp9_bitstream.c:1356 ; govpx WriteCompressedHeaderFromCounts)
	// reads the post-demotion InterpFilter, matching libvpx wire bits.
	header.InterpFilter = vp9FixInterpFilter(header.InterpFilter, counts)
	if !isKey && !intraOnly {
		referenceMode = encoder.CollapseReferenceModeFromCounts(referenceMode,
			&counts.ReferenceMode)
		if interState != nil {
			interState.referenceMode = referenceMode
		}
	}
	// libvpx's tile-write pass reads cm->interp_filter via
	// vp9_bitstream.c:306-314 to decide whether each block emits a
	// per-block switchable_interp literal. govpx mirrors that through
	// vp9ModeTreeInterpFilter -> inter.interpFilter, so the demoted
	// value must be propagated to the InterEncodeState the tile writer
	// reads (vp9_encoder.go:5740,5785). When c==1, fix_interp_filter
	// only demotes to the filter every block already picked
	// (libvpx vp9_bitstream.c:877-881), so the per-block assert
	// `mi->interp_filter == cm->interp_filter`
	// (libvpx vp9_bitstream.c:313) stays satisfied.
	if interState != nil {
		interState.interpFilter = header.InterpFilter
	}

	restorePostDropContext := e.rc.enabled && e.rc.mode == RateControlCBR &&
		e.rc.postEncodeDrop
	var postDropFC vp9dec.FrameContext
	var postDropFrameContexts [common.FrameContexts]vp9dec.FrameContext
	if restorePostDropContext {
		postDropFC = e.fc
		postDropFrameContexts = e.frameContexts
	}

	compSize, err := encoder.WriteCompressedHeaderFromCounts(e.scratch[:], encoder.WriteCompressedHeaderFromCountsArgs{
		Lossless:                header.Quant.Lossless,
		TxMode:                  txMode,
		IntraOnly:               isKey || header.IntraOnly,
		InterpFilter:            header.InterpFilter,
		ReferenceMode:           referenceMode,
		CompoundRefAllowed:      compoundAllowed,
		AllowHighPrecisionMv:    header.AllowHighPrecisionMv,
		CoefStepsize:            e.vp9CoeffProbAppxStep(),
		CoefUpdateMode:          e.vp9CoefUpdateModeForFrame(),
		SkipTx16PlusCoefUpdates: e.vp9SkipTx16PlusCoefUpdates(),
		Probs:                   &e.fc,
		Counts:                  counts,
	})
	if err != nil {
		return VP9EncodeResult{}, err
	}
	if compSize > 0xffff {
		return VP9EncodeResult{}, encoder.ErrCompressedHeaderTooLarge
	}
	header.FirstPartitionSize = uint16(compSize)
	if !isKey && !intraOnly {
		partitionProbs = e.fc.PartitionProb
	}

	var headerBW encoder.BitWriter
	headerBW.Init(dst)
	var uncSize int
	prevLfRef, prevLfMode := e.vp9EncoderLoopFilterPrevDeltas(resetLoopfilterDeltas)
	if header.FrameType == common.KeyFrame {
		uncSize = encoder.WriteKeyframeUncompressedHeaderWithLoopfilterPrev(
			&headerBW, header, &prevLfRef, &prevLfMode)
	} else if header.IntraOnly {
		uncSize = encoder.WriteIntraOnlyUncompressedHeaderWithLoopfilterPrev(
			&headerBW, header, &prevLfRef, &prevLfMode)
	} else {
		uncSize = encoder.WriteInterUncompressedHeaderWithLoopfilterPrev(
			&headerBW, header, e.vp9RefDims, &prevLfRef, &prevLfMode)
	}
	if uncSize+compSize >= len(dst) {
		return VP9EncodeResult{}, encoder.ErrPackBufferFull
	}
	copy(dst[uncSize:uncSize+compSize], e.scratch[:compSize])

	tileStart := uncSize + compSize
	tileKind := vp9ModeTreeInterSource
	if isKey || intraOnly {
		tileKind = vp9ModeTreeKeyframeSource
	} else if header.IntraOnly {
		tileKind = vp9ModeTreeKeyframe
	}
	tileSize, err := e.writeVP9FrameTiles(dst[tileStart:], miRows, miCols,
		header.Tile, &partitionProbs, &seg, baseMi, txMode, tileKind, keyState,
		interState)
	if err != nil {
		return VP9EncodeResult{}, err
	}
	e.sf.SkipEncodeFrame = 0
	if e.sf.SkipEncodeSb != 0 {
		e.sf.SkipEncodeFrame = vp9SkipEncodeFrameFromCounts(header, counts)
	}
	n := tileStart + tileSize
	// Post-tile loop-filter strength picker. The reconstruction
	// buffer (e.reconYFull) is now populated with the unfiltered
	// luma; the dispatcher can route LPF_PICK_FROM_FULL_IMAGE /
	// LPF_PICK_FROM_SUBIMAGE through the quadratic search against
	// real recon (libvpx vp9_picklpf.c:78-157 search_filter_level via
	// try_filter_frame at lines 46-76). LPF_PICK_FROM_Q and
	// LPF_PICK_MINIMAL_LPF do not consult the recon buffer, so the
	// pre-tile placeholder already carries the libvpx-correct level
	// and we skip the post-tile re-run entirely — this also keeps
	// the steady-state encode path allocation-free for the
	// (default-realtime) speed >= 3 case where sf.LpfPick =
	// LpfPickFromQ. libvpx vp9_speed_features.c:555 anchors the
	// realtime-default switchover.
	//
	// The picker is suppressed when the disable / lossless gates
	// already forced filter_level to 0; rerunning would only flip
	// the level back away from the intended override. The applyLPF
	// gate further enforces zero-level skip below.
	runFullImageSearch := (e.sf.LpfPick == LpfPickFromFullImage ||
		e.sf.LpfPick == LpfPickFromSubImage) &&
		header.Loopfilter.FilterLevel != 0 &&
		!vp9DisableLoopfilterForFrame(e.opts.DisableLoopfilter, isKey) &&
		!header.Quant.Lossless
	if runFullImageSearch {
		// header.Seg already mirrors seg at this point (line 2426
		// above); we pass header.Seg so the compiler doesn't have to
		// heap-promote the local seg in the steady-state FROM_Q /
		// MINIMAL path that never enters this block.
		pickedLevel := e.vp9EncoderRunFullImagePicker(header, &header.Seg, img,
			txMode, isKey)
		if pickedLevel != header.Loopfilter.FilterLevel {
			header.Loopfilter.FilterLevel = pickedLevel
			// Re-write the uncompressed header in place at dst[0:].
			// The byte length is invariant under filter_level (6-bit
			// literal at a fixed position), so the compressed-header
			// + tiles tail stays valid.
			var rewriteBW encoder.BitWriter
			rewriteBW.Init(dst)
			var rewSize int
			rewPrevLfRef, rewPrevLfMode := e.vp9EncoderLoopFilterPrevDeltas(resetLoopfilterDeltas)
			if header.FrameType == common.KeyFrame {
				rewSize = encoder.WriteKeyframeUncompressedHeaderWithLoopfilterPrev(
					&rewriteBW, header, &rewPrevLfRef, &rewPrevLfMode)
			} else if header.IntraOnly {
				rewSize = encoder.WriteIntraOnlyUncompressedHeaderWithLoopfilterPrev(
					&rewriteBW, header, &rewPrevLfRef, &rewPrevLfMode)
			} else {
				rewSize = encoder.WriteInterUncompressedHeaderWithLoopfilterPrev(
					&rewriteBW, header, e.vp9RefDims, &rewPrevLfRef, &rewPrevLfMode)
			}
			if rewSize != uncSize {
				// The uncompressed-header byte length must be
				// invariant — filter_level is a fixed-width 6-bit
				// literal and all sibling fields are independent of
				// it (libvpx encode_loopfilter, header_writer.go:384).
				// A drift here indicates a bitstream-writer bug; bail
				// rather than corrupting the stream.
				return VP9EncodeResult{}, ErrInvalidVP9Data
			}
		}
		// libvpx: vp9_encoder.c:3448 — `lf->last_filt_level =
		// lf->filter_level` after the picker returns. We refresh the
		// encoder-side cache here so the next frame's picker reads
		// the final post-search level instead of the pre-tile
		// placeholder.
		e.vp9LastFiltLevel = header.Loopfilter.FilterLevel
	}
	// libvpx vp9/encoder/vp9_encodeframe.c:5890-5891 — after the encode
	// pass produces rdc->filter_diff (per-block best_filter_diff[i] sums
	// at vp9_encodeframe.c:1881), merge it into the persistent
	// filter_threshes accumulator that drives the next frame's
	// SWITCHABLE -> concrete demotion. Skipped outside
	// frame_parameter_update inside the helper. We compute the same
	// refresh/alt-ref flags used at the demotion site so the frame_type
	// bucket is consistent across save / demote / update.
	{
		refreshGolden := refreshFlags&(1<<vp9GoldenRefSlot) != 0
		refreshAlt := refreshFlags&(1<<vp9AltRefSlot) != 0
		e.vp9UpdateFilterThreshesPostEncode(isKey, header.IntraOnly,
			srcFrameAltRef, refreshGolden, refreshAlt, macroblocks)
	}
	var firstPassStats VP9FirstPassFrameStats
	twoPassTargetBits := 0
	if header.ShowFrame {
		firstPassStats = e.twoPass.statsForFrame()
		twoPassTargetBits = e.vp9TwoPassFrameTarget
	}
	postDrop := e.rc.shouldPostEncodeDrop(isKey || intraOnly,
		header.ShowFrame, qindex, vpxrc.EncodedSizeBits(n))
	if postDrop {
		if restorePostDropContext {
			e.fc = postDropFC
			e.frameContexts = postDropFrameContexts
		}
		e.rc.postEncodeDropFrame(qindex)
	} else {
		e.adaptVP9EncoderFrameContext(header, frameContextIdx, counts, txMode)
		if header.RefreshFrameFlags != 0 {
			if !e.applyVP9EncoderLoopFilter(header, &seg) {
				return VP9EncodeResult{}, ErrInvalidVP9Data
			}
		}
		cyclicForRC, cyclicPost := e.vp9CyclicRefreshPostencodeFromMiGrid(
			miRows, miCols, header, isKey, intraOnly)
		e.applyCyclicRefreshPostencodeResult(header, cyclicPost)
		e.refreshVP9EncoderSegmentMap(miRows, miCols)
		e.prevSegmentation = header.Seg
		e.prevSegmentationValid = true
		e.prevFrameActiveMapEnabled = e.activeMapEnabled
		e.refreshVP9EncoderMvRefs(isKey || intraOnly, miRows, miCols)
		e.refreshVP9EncoderRefs(header, flags)
		e.finishVP9DenoiserFrame(header, img)
		e.commitVP9EncoderLoopFilterDeltas(&header.Loopfilter, resetLoopfilterDeltas)
		e.commitVP9EncoderFrameContext(header, frameContextIdx)
		e.lastVP9HeaderFrameType = header.FrameType
		e.lastVP9HeaderValid = true
		// libvpx vp9/encoder/vp9_encodeframe.c:5650 writes cm->tx_mode at the
		// top of vp9_encode_frame_internal; the value persists across frames so
		// the final else branch of select_tx_mode (vp9_encodeframe.c:4344) can
		// read back the previous frame's tx_mode. Mirror that commit here once
		// the per-frame encode (including the post-encode demotion at
		// vp9_encodeframe.c:5911-5944) has settled the final tx_mode.
		e.prevFrameTxMode = txMode
		e.rc.postEncodeFrame(n, header.ShowFrame, qindex, isKey || intraOnly,
			header.RefreshFrameFlags, macroblocks,
			e.vp9AltRefEnabledForRateControlStats(), cyclicForRC,
			e.vp9DampedAdjustmentRFLevel())
		if !isKey && !intraOnly {
			e.rc.computeFrameLowMotion(miRows, miCols,
				func(miRow, miCol int) *vp9dec.NeighborMi {
					return e.vp9MiAt(miRows, miCols, miRow, miCol)
				})
		}
	}
	e.lastFrameDropped = postDrop
	if header.ShowFrame {
		// libvpx vp9_twopass_postencode_update consumes the encoded bit
		// count to drive vbr_bits_off_target. Feed it 0 on drops, the
		// encoded size in bits otherwise, mirroring rc->projected_frame_size.
		// libvpx: vp9/encoder/vp9_firstpass.c:3733
		projected := 0
		if !postDrop {
			projected = vpxrc.EncodedSizeBits(n)
		}
		e.twoPass.finishFrameWithActual(projected)
	}
	e.vp9CommitLastSource(img, header.ShowFrame, postDrop)
	if postDrop {
		e.temporal.finishDroppedFrame(temporalFrame, e.vp9TemporalBufferConfig())
	} else {
		e.temporal.finishFrame(temporalFrame, isKey, header.ShowFrame,
			vp9TemporalReferenceRefresh(header.RefreshFrameFlags),
			vpxrc.EncodedSizeBits(n), e.vp9TemporalBufferConfig())
	}
	e.vp9FinishKeyFrameDistance(isKey)
	encodedFrameIndex := e.frameIndex
	if header.ShowFrame {
		e.frameIndex++
	}
	if isKey {
		e.forceKeyFrame = false
	}
	e.vp9LatchDeadlineModePreviousFrame()
	e.cyclicResizeFramePending = false
	// Consume the head TPL slab now that this frame has committed.  The
	// pass refills the new tail on the next populate call.
	if e.vp9TPLEnabled() {
		e.tpl.ShiftAndInvalidate()
	}
	spatialLayerID, spatialLayerCount, interLayerDependency,
		notRefForUpperSpatialLayer, scalabilityStructurePresent,
		spatialScalabilityStructure := e.vp9SpatialResultFields()
	resultData := dst[:n]
	resultSize := n
	resultRefreshFlags := header.RefreshFrameFlags
	if postDrop {
		// Discard the encoded payload and clear refresh-frame metadata so
		// downstream consumers treat the frame as dropped. The post-drop
		// decision is made before reference slots, segment maps, and frame
		// contexts commit, matching libvpx's restore-and-return path.
		resultData = nil
		resultSize = 0
		resultRefreshFlags = 0
	}
	publicQuantizer := encoder.QIndexToPublicQuantizer(qindex)
	if !postDrop {
		e.lastQuantizerInternal = qindex
		e.lastQuantizerPublic = publicQuantizer
		e.lastQuantizerValid = true
		e.lastLoopFilterLevel = header.Loopfilter.FilterLevel
		e.lastLoopFilterValid = true
	}
	result = VP9EncodeResult{
		Data:                        resultData,
		KeyFrame:                    isKey,
		IntraOnly:                   intraOnly,
		ShowFrame:                   header.ShowFrame,
		Dropped:                     postDrop,
		Droppable:                   !isKey && header.RefreshFrameFlags == 0 && !header.RefreshFrameContext,
		Quantizer:                   publicQuantizer,
		InternalQuantizer:           qindex,
		SizeBytes:                   resultSize,
		TargetBitrateKbps:           e.vp9ResultTargetBitrateKbps(),
		FrameTargetBits:             e.rc.frameTargetBits,
		BufferLevelBits:             e.rc.bufferLevelBits,
		RefreshFrameFlags:           resultRefreshFlags,
		FirstPassStats:              firstPassStats,
		TwoPassFrameTargetBits:      twoPassTargetBits,
		TemporalLayerID:             temporalFrame.LayerID,
		TemporalLayerCount:          temporalFrame.LayerCount,
		TemporalLayerSync:           temporalFrame.LayerSync,
		TL0PICIDX:                   temporalFrame.TL0PICIDX,
		SpatialLayerID:              spatialLayerID,
		SpatialLayerCount:           spatialLayerCount,
		InterLayerDependency:        interLayerDependency,
		NotRefForUpperSpatialLayer:  notRefForUpperSpatialLayer,
		ScalabilityStructurePresent: scalabilityStructurePresent,
		SpatialScalabilityStructure: spatialScalabilityStructure,
	}
	if result.TemporalLayerCount == 0 {
		result.TemporalLayerCount = 1
	}
	if vp9OracleTraceBuild {
		e.emitVP9OracleEncodedFrameTrace(encodedFrameIndex, flags, header,
			int(txMode), int(referenceMode), compoundAllowed, result, n)
	}
	// libvpx vp9/encoder/vp9_encoder.c:5567 clears
	// cpi->ext_refresh_frame_flags_pending at the tail of
	// encode_frame_to_data_rate, after the per-frame encode has committed.
	// govpx mirrors this so the next frame defaults to the
	// encoder-internal refresh decision unless the caller rearms it via
	// vp9_apply_encoding_flags (i.e., passes a fresh EncodeFlags set).
	e.vp9CommitExtOverridesAfterEncode()
	return result, nil
}
