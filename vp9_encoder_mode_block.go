package govpx

import (
	"image"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

func (e *VP9Encoder) writeVP9ModeBlock(bw *bitstream.Writer, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	kind vp9ModeTreeKind, key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) {
	cur := baseMi
	cur.SbType = bsize
	cur.TxSize = clampVP9TxSizeForBlock(cur.TxSize, bsize)
	useDynamicMap := vp9ModeTreeUsesInterSegmentMap(kind)
	var segmentImg *image.YCbCr
	if kind == vp9ModeTreeKeyframeSource && e.opts.AQMode == VP9AQVariance && key != nil {
		useDynamicMap = true
		segmentImg = key.img
	}
	cur.SegmentID, cur.SegIDPredicted = e.vp9EncoderBlockSegmentID(
		seg, miRows, miCols, miRow, miCol, bsize,
		useDynamicMap, segmentImg, inter)
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	counts := vp9EncodeCountsForState(key, inter)
	if kind == vp9ModeTreeInterSkip || kind == vp9ModeTreeInterSource {
		reconBsize := vp9dec.ModeInfoDecodeBSize(bsize)
		hasResidue := false
		uvMode := common.DcPred
		segID := vp9EncoderMiSegmentID(&cur)
		segmentSkip := vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlSkip)
		forcedRefFrame, forcedRef := vp9EncoderForcedSegmentRefFrame(seg, segID)
		forcedIntra := forcedRef && forcedRefFrame == vp9dec.IntraFrame
		if forcedIntra {
			cur.RefFrame = [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame}
			// libvpx vp9_pickmode.c:2644-2645 — intra blocks park mv[0]/mv[1]
			// at INVALID_MV for the NEWMV-diff-bias neighbour check.
			cur.Mv = [2]vp9dec.MV{vp9dec.InvalidMV, vp9dec.InvalidMV}
			cur.InterpFilter = uint8(vp9dec.SwitchableFilters)
			intra, ok := e.pickVP9ForcedInterIntraMode(inter, tile,
				miRows, miCols, miRow, miCol, reconBsize, cur.TxSize)
			if ok {
				cur.Mode = intra.mode
				uvMode = intra.uvMode
				if intra.txSize < common.TxSizes {
					cur.TxSize = intra.txSize
				}
			}
			if kind == vp9ModeTreeInterSource && inter != nil {
				intraResidue := e.prepareVP9InterIntraBlockResidue(inter, tile,
					miRows, miCols, miRow, miCol, reconBsize, &cur, uvMode)
				if !segmentSkip && intraResidue {
					hasResidue = true
					cur.Skip = 0
				}
			}
			if segmentSkip {
				cur.Skip = 1
			}
		} else if segmentSkip {
			if kind == vp9ModeTreeInterSource && inter != nil {
				e.prepareVP9InterSkipPrediction(inter, miRows, miCols,
					miRow, miCol, reconBsize, &cur, forcedRefFrame, forcedRef)
			}
			cur.Skip = 1
		} else if kind == vp9ModeTreeInterSource && inter != nil {
			// libvpx x->skip_encode search-context freeze: run the leaf's RD
			// search + zcoeff_blk decision against the SB-entry entropy context
			// (frozen because the search-phase intermediate encode never advances
			// it, vp9_encodeframe.c:6112-6115), then re-thread the running context
			// so WriteCoefSb commits the real coefficient context. No-op when
			// skip_encode is not armed (snapshot invalid), so frame-1 / production
			// keep the running-threaded search context.
			var interDecision vp9InterModeDecision
			var chosenUvMode common.PredictionMode
			var residue bool
			e.vp9WithSBSearchEntropy(miRows, miCols, miRow, miCol, reconBsize, func() {
				interDecision, chosenUvMode, residue = e.prepareVP9InterBlockResidue(inter, miRows, miCols,
					miRow, miCol, reconBsize, tile, &cur, seg, forcedRefFrame, forcedRef)
			})
			uvMode, hasResidue = chosenUvMode, residue
			segID = vp9EncoderMiSegmentID(&cur)
			segmentSkip = vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlSkip)
			if hasResidue {
				cur.Skip = 0
			}
			// libvpx vp9_encodeframe.c:1809-1810 passes x->skip from
			// encode_breakout_test only (vp9_pickmode.c:1026), not the
			// block_yrd skip bit or post-tokenize mi->skip.
			e.vp9UpdateCyclicRefreshInterSegment(inter, seg, miRows, miCols,
				miRow, miCol, reconBsize, &cur, interDecision)
		}
		if !segmentSkip {
			if hasResidue {
				cur.Skip = 0
			} else {
				cur.Skip = 1
			}
		}
		isInter := cur.RefFrame[0] > vp9dec.IntraFrame
		if isInter && bsize < common.Block8x8 {
			if !e.ensureVP9Sub8InterBmiForWrite(&cur, tile, miRows, miCols,
				miRow, miCol, bsize, inter) {
				return
			}
		}
		interModeCtx := vp9dec.InterModeContext(e.miGrid, miCols,
			tile, miRows, miRow, miCol, bsize)
		maxTxSize := common.MaxTxsizeLookup[bsize]
		txCtx := vp9dec.GetTxSizeContext(above, left, maxTxSize)
		// libvpx vp9/encoder/vp9_encodeframe.c:6109-6125 — post-encode
		// tx_size commit. When (cm->tx_mode == TX_MODE_SELECT &&
		// mi->sb_type >= BLOCK_8X8 && !(is_inter_block && mi->skip))
		// the per-context tx_counts get incremented. Otherwise libvpx
		// re-clamps mi->tx_size: for inter blocks to
		// min(tx_mode_to_biggest_tx_size[tx_mode], max_txsize_lookup[bsize])
		// (line 6117-6118), and for intra blocks to TX_4X4 when bsize <
		// BLOCK_8X8 (line 6120). The re-clamped tx_size then feeds the
		// unconditional tx_totals++ at line 6124.
		if txMode == common.TxModeSelect && bsize >= common.Block8x8 &&
			!(isInter && cur.Skip != 0) {
			countVP9TxSize(counts, txCtx, maxTxSize, cur.TxSize)
		} else {
			// libvpx vp9_encodeframe.c:6114-6121 else-branch.
			if isInter {
				biggest := common.TxModeToBiggestTxSize[txMode]
				cur.TxSize = min(biggest, maxTxSize)
			} else if bsize < common.Block8x8 {
				cur.TxSize = common.Tx4x4
			}
		}
		countVP9TxTotals(counts, bsize, cur.TxSize, &e.planes)
		frameInterpFilter := vp9ModeTreeInterpFilter(kind, inter)
		countVP9Skip(counts, seg, segID, above, left, cur.Skip)
		bestRefMv := e.vp9EncoderBestInterRefMvs(tile, miRows, miCols,
			miRow, miCol, bsize, &cur, inter != nil && inter.allowHP,
			vp9InterSignBias(inter))
		countVP9IntraInter(counts, seg, segID, above, left, vp9dec.BoolInt(isInter))
		if isInter {
			frameMode := vp9InterReferenceMode(inter)
			compoundRefs := vp9InterCompoundRefs(inter)
			signBias := vp9InterSignBias(inter)
			isCompound := cur.RefFrame[1] > vp9dec.IntraFrame
			countVP9ReferenceMode(counts, seg, segID, frameMode, compoundRefs,
				above, left, isCompound)
			if isCompound {
				countVP9CompoundRef(counts, seg, segID, above, left,
					compoundRefs, signBias, cur.RefFrame)
			} else {
				countVP9SingleRef(counts, seg, segID, above, left, cur.RefFrame[0])
			}
			if bsize < common.Block8x8 {
				countVP9InterSub8Modes(counts, seg, segID, bsize,
					interModeCtx, &cur.Bmi)
				e.countVP9InterSub8NewMvs(counts, tile, miRows, miCols,
					miRow, miCol, bsize, &cur, inter != nil && inter.allowHP,
					signBias)
			} else {
				countVP9InterMode(counts, seg, segID, bsize, interModeCtx, cur.Mode)
				if cur.Mode == common.NewMv {
					halves := 1
					if isCompound {
						halves = 2
					}
					for ref := 0; ref < halves; ref++ {
						countVP9NewMv(counts, cur.Mv[ref], bestRefMv[ref])
					}
				}
			}
			if frameInterpFilter == vp9dec.InterpSwitchable {
				countVP9SwitchableInterp(counts, above, left, cur.InterpFilter)
			}
		} else {
			countVP9InterIntraMode(counts, bsize, cur.Mode)
		}
		// Compile-elided per-block ground-truth probe (govpx_oracle_trace builds
		// only; silent unless GOVPX_GT_TRACE is set). Fire once per leaf on the
		// real bitstream pass (count pre-pass keeps inter.counts != nil).
		if vp9OracleTraceBuild && kind == vp9ModeTreeInterSource && inter != nil {
			if inter.counts == nil {
				e.vp9TraceCommitBlock(e.frameIndex, miRow, miCol, &cur, uvMode)
			} else {
				e.vp9TraceCommitBlockPre(e.frameIndex, miRow, miCol, &cur, uvMode)
			}
		}
		encoder.WriteInterBlock(bw, encoder.WriteInterBlockArgs{
			Seg:              seg,
			Mi:               &cur,
			AboveMi:          above,
			LeftMi:           left,
			Fc:               &e.fc,
			TxMode:           txMode,
			MaxTxSize:        maxTxSize,
			TxProbs:          vp9TxProbsRow(&e.fc.TxProbs, maxTxSize, txCtx),
			FrameRefMode:     vp9InterReferenceMode(inter),
			InterpFilter:     frameInterpFilter,
			CompFixedRef:     vp9InterCompoundRefs(inter).CompFixedRef,
			CompVarRef:       vp9InterCompoundRefs(inter).CompVarRef,
			RefFrameSignBias: vp9InterSignBias(inter),
			SwitchableInterpCtx: vp9dec.GetPredContextSwitchableInterp(
				above, left),
			InterModeCtx: interModeCtx,
			IsCompound:   cur.RefFrame[1] > vp9dec.IntraFrame,
			Mv:           cur.Mv,
			BestRefMv:    bestRefMv,
			AllowHP:      inter != nil && inter.allowHP,
			UvMode:       uvMode,
		})
		if kind == vp9ModeTreeInterSource && inter != nil {
			aboveOffsets, leftOffsets := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
			if cur.Skip != 0 {
				vp9dec.ResetSkipContext(e.planes[:], reconBsize, aboveOffsets[:], leftOffsets[:])
				e.fillVP9MiGrid(miRows, miCols, miRow, miCol, bsize, cur)
				return
			}
			_ = encoder.WriteCoefSb(bw, encoder.WriteCoefSbArgs{
				BSize:        reconBsize,
				MiTxSize:     cur.TxSize,
				IsInter:      vp9dec.BoolInt(isInter),
				Lossless:     inter.lossless,
				Mi:           &cur,
				MiRows:       miRows,
				MiCols:       miCols,
				MiRow:        miRow,
				MiCol:        miCol,
				Planes:       &e.planes,
				AboveOffsets: aboveOffsets,
				LeftOffsets:  leftOffsets,
				PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
					inter.dq.Y[segID],
					inter.dq.Uv[segID],
					inter.dq.Uv[segID],
				},
				Fc:              &e.fc.CoefProbs,
				CoefBranchStats: vp9CoefBranchStats(counts),
				GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
					return e.vp9BlockCoeffs(plane, reconBsize, r, c, tx)
				},
				GetQCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
					return e.vp9BlockQCoeffs(plane, reconBsize, r, c, tx)
				},
			})
		}
		e.fillVP9MiGrid(miRows, miCols, miRow, miCol, bsize, cur)
		return
	}
	if kind == vp9ModeTreeKeyframeSource && key != nil {
		reconBsize := vp9dec.ModeInfoDecodeBSize(bsize)
		// libvpx vp9_rdopt.c:3239-3252 — vp9_rd_pick_intra_mode_sb
		// dispatches the Y-mode picker on bsize: BLOCK_8X8+ routes
		// through rd_pick_intra_sby_mode (the per-MI mode picker), while
		// BLOCK_4X4 / BLOCK_4X8 / BLOCK_8X4 route through
		// rd_pick_intra_sub_8x8_y_mode which runs an independent
		// DC..TM_PRED RD scan per 4x4 raster sub-block and stows the
		// per-subblock pick in mic->bmi[i].as_mode.
		useNonRDKeyframeMode := e.useVP9KeyframeNonRDIntraMode(reconBsize)
		uvMode := common.DcPred
		keyDecisionReplayed := false
		var cachedKeyDecision vp9KeyframeModeDecision
		if cached, ok := e.lookupVP9LeafKeyframeDecision(miRow, miCol, bsize); ok {
			cur.Mode = cached.mode
			cur.Bmi = cached.bmi
			cur.TxSize = cached.txSize
			uvMode = cached.uvMode
			cachedKeyDecision = cached
			keyDecisionReplayed = true
		}
		if !keyDecisionReplayed {
			if bsize < common.Block8x8 {
				_, _ = e.pickVP9KeyframeSub8x8YMode(key, tile, miRows, miCols,
					miRow, miCol, bsize, &cur, ^uint64(0))
			} else {
				cur.Mode = e.pickVP9KeyframeMode(key, tile, miRows, miCols,
					miRow, miCol, reconBsize, &cur, txMode)
			}
			if !useNonRDKeyframeMode {
				uvMode = e.pickVP9KeyframeUvMode(key, tile, miRows, miCols,
					miRow, miCol, reconBsize, &cur)
			}
		}
		segID := vp9EncoderMiSegmentID(&cur)
		segmentSkip := vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlSkip)
		hasResidue := false
		if segmentSkip {
			cur.Skip = 1
			if key.counts != nil {
				storeDecision := vp9KeyframeModeDecision{
					mode:   cur.Mode,
					bmi:    cur.Bmi,
					txSize: cur.TxSize,
					uvMode: uvMode,
					skip:   true,
				}
				if keyDecisionReplayed {
					storeDecision.reconReplayValid = cachedKeyDecision.reconReplayValid
					storeDecision.reconReplayMode = cachedKeyDecision.reconReplayMode
					storeDecision.reconReplayTx = cachedKeyDecision.reconReplayTx
					storeDecision.reconReplayRD = cachedKeyDecision.reconReplayRD
				}
				e.storeVP9LeafKeyframeDecision(miRow, miCol, bsize, storeDecision)
			}
		} else {
			// libvpx vp9_rdopt.c:3221-3270 — vp9_rd_pick_intra_mode_sb
			// chains rd_pick_intra_sby_mode (which runs the per-block
			// tx_size RD via super_block_yrd -> choose_tx_size_from_rd
			// when cm->tx_mode == TX_MODE_SELECT) before
			// rd_pick_intra_sbuv_mode. When pickVP9KeyframeMode already
			// ran that full-RD path, keep its chosen tx_size; otherwise
			// layer the standalone tx picker on top of the simpler mode
			// score so TxSize still follows choose_tx_size_from_rd.
			modePickerChoseTx := e.sf.TxSizeSearchMethod == UseFullRD &&
				e.vp9KeyframeRDRefinementEnabled()
			if !useNonRDKeyframeMode && !keyDecisionReplayed &&
				!modePickerChoseTx && bsize >= common.Block8x8 {
				e.pickVP9KeyframeBlockTxSize(key, tile, miRows, miCols,
					miRow, miCol, reconBsize, &cur, txMode)
			}
			if key.counts != nil {
				storeDecision := vp9KeyframeModeDecision{
					mode:   cur.Mode,
					bmi:    cur.Bmi,
					txSize: cur.TxSize,
					uvMode: uvMode,
					skip:   cachedKeyDecision.skip,
				}
				if keyDecisionReplayed {
					storeDecision.reconReplayValid = cachedKeyDecision.reconReplayValid
					storeDecision.reconReplayMode = cachedKeyDecision.reconReplayMode
					storeDecision.reconReplayTx = cachedKeyDecision.reconReplayTx
					storeDecision.reconReplayRD = cachedKeyDecision.reconReplayRD
				}
				e.storeVP9LeafKeyframeDecision(miRow, miCol, bsize, storeDecision)
			}
			// libvpx vp9_encodeframe.c:6057-6060 initializes every intra
			// block as skipped before vp9_encode_intra_block_plane tokenizes
			// it; the transform path clears mi->skip when any plane emits
			// non-zero coefficients. Mirror that state transition here so
			// keyframe blocks with no residual write the skip bit instead of
			// a zero-coefficient block body.
			if keyDecisionReplayed {
				e.replayVP9KeyframeYReconSideEffect(key, tile, miRows, miCols,
					miRow, miCol, reconBsize, &cur, cachedKeyDecision)
			}
			cur.Skip = 1
			hasResidue = e.prepareVP9KeyframeBlockResidue(key, tile, miRows, miCols,
				miRow, miCol, reconBsize, &cur, uvMode)
			if hasResidue {
				cur.Skip = 0
			}
		}
		countVP9Skip(counts, seg, segID, above, left, cur.Skip)
		maxTxSize := common.MaxTxsizeLookup[bsize]
		txCtx := vp9dec.GetTxSizeContext(above, left, maxTxSize)
		if txMode == common.TxModeSelect && bsize >= common.Block8x8 {
			countVP9TxSize(counts, txCtx, maxTxSize, cur.TxSize)
		}
		countVP9TxTotals(counts, bsize, cur.TxSize, &e.planes)
		encoder.WriteKeyframeBlock(bw, encoder.WriteKeyframeBlockArgs{
			Seg:       seg,
			Mi:        &cur,
			AboveMi:   above,
			LeftMi:    left,
			TxMode:    txMode,
			MaxTxSize: maxTxSize,
			TxProbs:   vp9TxProbsRow(&e.fc.TxProbs, maxTxSize, txCtx),
			SkipProbs: e.fc.SkipProbs,
		})
		encoder.WriteKeyframeUvMode(bw, uvMode, cur.Mode)
		aboveOffsets, leftOffsets := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
		if !hasResidue {
			vp9dec.ResetSkipContext(e.planes[:], reconBsize, aboveOffsets[:], leftOffsets[:])
			e.fillVP9MiGrid(miRows, miCols, miRow, miCol, bsize, cur)
			return
		}
		_ = encoder.WriteCoefSb(bw, encoder.WriteCoefSbArgs{
			BSize:        reconBsize,
			MiTxSize:     cur.TxSize,
			IsInter:      0,
			Lossless:     key.lossless,
			Mi:           &cur,
			MiRows:       miRows,
			MiCols:       miCols,
			MiRow:        miRow,
			MiCol:        miCol,
			Planes:       &e.planes,
			AboveOffsets: aboveOffsets,
			LeftOffsets:  leftOffsets,
			PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
				key.dq.Y[segID],
				key.dq.Uv[segID],
				key.dq.Uv[segID],
			},
			Fc:              &e.fc.CoefProbs,
			CoefBranchStats: vp9CoefBranchStats(counts),
			GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
				return e.vp9BlockCoeffs(plane, reconBsize, r, c, tx)
			},
			GetQCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
				return e.vp9BlockQCoeffs(plane, reconBsize, r, c, tx)
			},
		})
		e.fillVP9MiGrid(miRows, miCols, miRow, miCol, bsize, cur)
		return
	}
	// Fallback path: vp9ModeTreeKeyframe (counts-pass dispatch for
	// intra-only frames at collectVP9EncodeFrameCounts:3480) and any
	// other kind that arrives without key/inter state. libvpx's
	// equivalent is write_modes_b at vp9/encoder/vp9_bitstream.c:378-403
	// inside frame_is_intra_only(cm) -> write_mb_modes_kf — the same
	// function the keyframe-source branch above dispatches to. The
	// TX_MODE_SELECT cascade needs the fc.TxProbs row keyed by
	// (max_tx_size, ctx); without it WriteSelectedTxSize would index
	// into an empty slice (the bug a843f45d cited as a deferred panic).
	fallbackMaxTxSize := common.MaxTxsizeLookup[bsize]
	fallbackTxCtx := vp9dec.GetTxSizeContext(above, left, fallbackMaxTxSize)
	if txMode == common.TxModeSelect && bsize >= common.Block8x8 {
		countVP9TxSize(counts, fallbackTxCtx, fallbackMaxTxSize, cur.TxSize)
	}
	countVP9TxTotals(counts, bsize, cur.TxSize, &e.planes)
	encoder.WriteKeyframeBlock(bw, encoder.WriteKeyframeBlockArgs{
		Seg:       seg,
		Mi:        &cur,
		AboveMi:   above,
		LeftMi:    left,
		TxMode:    txMode,
		MaxTxSize: fallbackMaxTxSize,
		TxProbs:   vp9TxProbsRow(&e.fc.TxProbs, fallbackMaxTxSize, fallbackTxCtx),
		SkipProbs: e.fc.SkipProbs,
	})
	encoder.WriteKeyframeUvMode(bw, common.DcPred, cur.Mode)
	e.fillVP9MiGrid(miRows, miCols, miRow, miCol, bsize, cur)
}

func (e *VP9Encoder) vp9EncoderBlockSegmentID(seg *vp9dec.SegmentationParams,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize, useDynamicMap bool,
	img *image.YCbCr, inter *vp9InterEncodeState,
) (uint8, uint8) {
	if seg == nil || !seg.Enabled {
		return 0, 0
	}
	if !seg.UpdateMap {
		return e.vp9EncoderPreviousSegmentID(miRows, miCols, miRow, miCol,
			bsize), 0
	}
	segID := e.vp9StaticSegmentIDForMap()
	if useDynamicMap {
		if dynamicID, ok := e.vp9DynamicSegmentID(miRow, miCol, img, inter); ok {
			segID = dynamicID
		}
	}
	predicted := segID
	if inter != nil {
		predicted = 0
	}
	if seg.TemporalUpdate {
		predicted = e.vp9EncoderSegmentMapPredicted(miRows, miCols,
			miRow, miCol, bsize, segID)
	}
	return segID, predicted
}

func (e *VP9Encoder) vp9EncoderPreviousSegmentID(miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
) uint8 {
	if e == nil || !e.useVP9EncoderPrevSegmentMap(miRows, miCols) ||
		miRow < 0 || miCol < 0 || miRow >= miRows || miCol >= miCols {
		return 0
	}
	xMis := int(common.Num8x8BlocksWideLookup[bsize])
	yMis := int(common.Num8x8BlocksHighLookup[bsize])
	if xMis > miCols-miCol {
		xMis = miCols - miCol
	}
	if yMis > miRows-miRow {
		yMis = miRows - miRow
	}
	if xMis <= 0 || yMis <= 0 {
		return 0
	}
	miOffset := miRow*miCols + miCol
	segID := vp9dec.DecGetSegmentId(e.prevSegmentMap, miCols, miOffset,
		xMis, yMis)
	if segID < 0 || segID >= vp9dec.MaxSegments {
		return 0
	}
	return uint8(segID)
}

func (e *VP9Encoder) vp9EncoderSegmentMapPredicted(miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, segID uint8,
) uint8 {
	if e.vp9EncoderPreviousSegmentID(miRows, miCols, miRow, miCol,
		bsize) == segID {
		return 1
	}
	return 0
}

func vp9ModeTreeUsesInterSegmentMap(kind vp9ModeTreeKind) bool {
	return kind == vp9ModeTreeInterSkip || kind == vp9ModeTreeInterSource
}
