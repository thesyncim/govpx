package codecapi

// EncoderPhaseStats accumulates opt-in coarse encoder phase timing in
// nanoseconds and counters for the SAD/subpel search hot path when govpx is
// built with the govpx_phase_stats tag. Normal builds compile the option path
// out of VP8 encode hot paths: no clock reads, counter writes, or nil-check
// branches. The caller owns the value, may [EncoderPhaseStats.Reset] it between
// warmup and measurement, and may sample it concurrently with EncodeInto only
// under its own synchronization.
type EncoderPhaseStats struct {
	// InterReconstructNS is time spent rebuilding inter-frame prediction and
	// residual planes.
	InterReconstructNS int64 `json:"inter_reconstruct_ns"`
	// KeyReconstructNS is time spent rebuilding key-frame prediction and
	// residual planes.
	KeyReconstructNS int64 `json:"key_reconstruct_ns"`
	// LoopFilterPickNS is time spent selecting loop-filter parameters.
	LoopFilterPickNS int64 `json:"loop_filter_pick_ns"`
	// LoopFilterApplyNS is time spent applying the loop filter to committed
	// frames.
	LoopFilterApplyNS int64 `json:"loop_filter_apply_ns"`
	// PacketWriteNS is time spent packing VP8 frame data.
	PacketWriteNS int64 `json:"packet_write_ns"`
	// InterAttempts counts inter-frame encode attempts, including recodes.
	InterAttempts int64 `json:"inter_attempts"`
	// KeyAttempts counts key-frame encode attempts, including recodes.
	KeyAttempts int64 `json:"key_attempts"`
	// VP9PreEncodeDrops counts VP9 frames skipped before mode search.
	VP9PreEncodeDrops int64 `json:"vp9_pre_encode_drops"`
	// VP9PreEncodeDropNegativeBuffer counts pre-encode drops from negative CBR buffer.
	VP9PreEncodeDropNegativeBuffer int64 `json:"vp9_pre_encode_drop_negative_buffer"`
	// VP9PreEncodeDropWatermarkDecimation counts pre-encode drops from CBR decimation.
	VP9PreEncodeDropWatermarkDecimation int64 `json:"vp9_pre_encode_drop_watermark_decimation"`
	// VP9PostEncodeDrops counts VP9 frames encoded and then discarded by CBR.
	VP9PostEncodeDrops int64 `json:"vp9_post_encode_drops"`
	// VP9PostEncodeDropBits counts encoded bits spent on VP9 post-encode drops.
	VP9PostEncodeDropBits int64 `json:"vp9_post_encode_drop_bits"`
	// VP9CountNS is time spent in VP9's count/tokencollect mode-tree pre-pass.
	VP9CountNS int64 `json:"vp9_count_ns"`
	// VP9HeaderWriteNS is time spent packing VP9 compressed and uncompressed headers.
	VP9HeaderWriteNS int64 `json:"vp9_header_write_ns"`
	// VP9TileWriteNS is time spent writing VP9 tile data and replaying staged tokens.
	VP9TileWriteNS int64 `json:"vp9_tile_write_ns"`
	// VP9LoopFilterPickNS is time spent in VP9's post-tile loop-filter picker.
	VP9LoopFilterPickNS int64 `json:"vp9_loop_filter_pick_ns"`
	// VP9LoopFilterApplyNS is time spent applying VP9's committed loop filter.
	VP9LoopFilterApplyNS int64 `json:"vp9_loop_filter_apply_ns"`

	// LoopFilterTrials counts candidate filter-strength evaluations.
	LoopFilterTrials int64 `json:"loop_filter_trials"`
	// LoopFilterTrialCopyNS is time spent copying trial frame buffers.
	LoopFilterTrialCopyNS int64 `json:"loop_filter_trial_copy_ns"`
	// LoopFilterTrialFilterNS is time spent filtering trial frame buffers.
	LoopFilterTrialFilterNS int64 `json:"loop_filter_trial_filter_ns"`
	// LoopFilterTrialSSENS is time spent scoring loop-filter trials.
	LoopFilterTrialSSENS int64 `json:"loop_filter_trial_sse_ns"`

	// InterRDCoeffCacheRequests counts inter RD coefficient-cache lookups.
	InterRDCoeffCacheRequests int64 `json:"inter_rd_coeff_cache_requests"`
	// InterRDCoeffCacheDCTHits counts cached DCT/residual reuse hits.
	InterRDCoeffCacheDCTHits int64 `json:"inter_rd_coeff_cache_dct_hits"`
	// InterCoefTokenRecords counts token-rate records produced during inter RD.
	InterCoefTokenRecords int64 `json:"inter_coef_token_records"`

	// FullPelSADCalls counts full-pixel SAD search invocations.
	FullPelSADCalls int64 `json:"fullpel_sad_calls"`
	// FullPelSADCandidates counts full-pixel candidate positions scored.
	FullPelSADCandidates int64 `json:"fullpel_sad_candidates"`
	// FullPelBatchCalls counts vectorized multi-candidate SAD batches.
	FullPelBatchCalls int64 `json:"fullpel_batch_calls"`
	// VP9FullPelSAD* count VP9 full-pixel SAD calls/candidates by call site.
	VP9FullPelSADZeroCalls         int64 `json:"vp9_fullpel_sad_zero_calls"`
	VP9FullPelSADZeroCandidates    int64 `json:"vp9_fullpel_sad_zero_candidates"`
	VP9FullPelSADSeedCalls         int64 `json:"vp9_fullpel_sad_seed_calls"`
	VP9FullPelSADSeedCandidates    int64 `json:"vp9_fullpel_sad_seed_candidates"`
	VP9FullPelSADHintCalls         int64 `json:"vp9_fullpel_sad_hint_calls"`
	VP9FullPelSADHintCandidates    int64 `json:"vp9_fullpel_sad_hint_candidates"`
	VP9FullPelSADPatternCalls      int64 `json:"vp9_fullpel_sad_pattern_calls"`
	VP9FullPelSADPatternCandidates int64 `json:"vp9_fullpel_sad_pattern_candidates"`
	VP9FullPelSADFullRDCalls       int64 `json:"vp9_fullpel_sad_fullrd_calls"`
	VP9FullPelSADFullRDCandidates  int64 `json:"vp9_fullpel_sad_fullrd_candidates"`
	VP9FullPelSADOtherCalls        int64 `json:"vp9_fullpel_sad_other_calls"`
	VP9FullPelSADOtherCandidates   int64 `json:"vp9_fullpel_sad_other_candidates"`
	// VP9FullPelSearches counts VP9 full-pixel motion-search invocations.
	VP9FullPelSearches int64 `json:"vp9_fullpel_searches"`
	// VP9FullPelSearchSkipMVPart counts searches skipped by variance-partition MVs.
	VP9FullPelSearchSkipMVPart int64 `json:"vp9_fullpel_search_skip_mvpart"`
	// VP9FullPelSearchSkipIntPro counts searches skipped by int-pro estimation.
	VP9FullPelSearchSkipIntPro int64 `json:"vp9_fullpel_search_skip_intpro"`
	// VP9FullPelSearch* count VP9 full-pixel searches by block size.
	VP9FullPelSearch64x64 int64 `json:"vp9_fullpel_search_64x64"`
	VP9FullPelSearch32x32 int64 `json:"vp9_fullpel_search_32x32"`
	VP9FullPelSearch32x16 int64 `json:"vp9_fullpel_search_32x16"`
	VP9FullPelSearch16x32 int64 `json:"vp9_fullpel_search_16x32"`
	VP9FullPelSearch16x16 int64 `json:"vp9_fullpel_search_16x16"`
	VP9FullPelSearch16x8  int64 `json:"vp9_fullpel_search_16x8"`
	VP9FullPelSearch8x16  int64 `json:"vp9_fullpel_search_8x16"`
	VP9FullPelSearch8x8   int64 `json:"vp9_fullpel_search_8x8"`
	// FullPelBoundsRejects counts full-pixel candidates outside legal bounds.
	FullPelBoundsRejects int64 `json:"fullpel_bounds_rejects"`
	// FullPelEarlyBreaks counts SAD evaluations stopped by an existing best.
	FullPelEarlyBreaks int64 `json:"fullpel_early_breaks"`
	// SubpelCandidates counts sub-pixel candidate positions scored.
	SubpelCandidates int64 `json:"subpel_candidates"`
	// SubpelVarianceCalls counts sub-pixel variance evaluations.
	SubpelVarianceCalls int64 `json:"subpel_variance_calls"`
	// SubpelCacheHits counts sub-pixel predictor-cache hits.
	SubpelCacheHits int64 `json:"subpel_cache_hits"`
	// SubpelBoundsRejects counts sub-pixel candidates outside legal bounds.
	SubpelBoundsRejects int64 `json:"subpel_bounds_rejects"`
	// SubpelEarlyBreaks counts sub-pixel evaluations stopped by an existing best.
	SubpelEarlyBreaks int64 `json:"subpel_early_breaks"`

	// VP9ModeBlocks counts write_modes_b-equivalent VP9 mode-block visits.
	VP9ModeBlocks int64 `json:"vp9_mode_blocks"`
	// VP9FrameTiles counts VP9 tile writer entries.
	VP9FrameTiles int64 `json:"vp9_frame_tiles"`
	// VP9FrameTilesCountPass counts VP9 tile writer entries in the count pre-pass.
	VP9FrameTilesCountPass int64 `json:"vp9_frame_tiles_count_pass"`
	// VP9FrameTilesWritePass counts VP9 tile writer entries in the bitstream pass.
	VP9FrameTilesWritePass int64 `json:"vp9_frame_tiles_write_pass"`
	// VP9ModeSBs counts recursive write_modes_sb-equivalent VP9 superblock visits.
	VP9ModeSBs int64 `json:"vp9_mode_sbs"`
	// VP9ModeSBsCountPass counts VP9 superblock visits in the count pre-pass.
	VP9ModeSBsCountPass int64 `json:"vp9_mode_sbs_count_pass"`
	// VP9ModeSBsWritePass counts VP9 superblock visits in the bitstream pass.
	VP9ModeSBsWritePass int64 `json:"vp9_mode_sbs_write_pass"`
	// VP9ModeBlocksCountPass counts VP9 mode-block visits in the count pre-pass.
	VP9ModeBlocksCountPass int64 `json:"vp9_mode_blocks_count_pass"`
	// VP9ModeBlocksWritePass counts VP9 mode-block visits in the bitstream pass.
	VP9ModeBlocksWritePass int64 `json:"vp9_mode_blocks_write_pass"`
	// VP9ModeBlock* count VP9 leaf block-size visits.
	VP9ModeBlock64x64 int64 `json:"vp9_mode_block_64x64"`
	VP9ModeBlock32x32 int64 `json:"vp9_mode_block_32x32"`
	VP9ModeBlock32x16 int64 `json:"vp9_mode_block_32x16"`
	VP9ModeBlock16x32 int64 `json:"vp9_mode_block_16x32"`
	VP9ModeBlock16x16 int64 `json:"vp9_mode_block_16x16"`
	VP9ModeBlock16x8  int64 `json:"vp9_mode_block_16x8"`
	VP9ModeBlock8x16  int64 `json:"vp9_mode_block_8x16"`
	VP9ModeBlock8x8   int64 `json:"vp9_mode_block_8x8"`
	VP9ModeBlockSub8  int64 `json:"vp9_mode_block_sub8"`
	// VP9InterModePicks counts VP9 realtime inter-mode picker calls.
	VP9InterModePicks int64 `json:"vp9_inter_mode_picks"`
	// VP9InterModePicksCountPass counts inter-mode picks in the count pre-pass.
	VP9InterModePicksCountPass int64 `json:"vp9_inter_mode_picks_count_pass"`
	// VP9InterModePicksWritePass counts inter-mode picks in the bitstream pass.
	VP9InterModePicksWritePass int64 `json:"vp9_inter_mode_picks_write_pass"`
	// VP9InterLeafCacheStores counts count-pass inter leaf decisions cached.
	VP9InterLeafCacheStores int64 `json:"vp9_inter_leaf_cache_stores"`
	// VP9InterLeafCacheReplayHits counts write-pass leaf-decision replay hits.
	VP9InterLeafCacheReplayHits int64 `json:"vp9_inter_leaf_cache_replay_hits"`
	// VP9InterLeafCacheReplayMisses counts write-pass leaf-decision replay misses.
	VP9InterLeafCacheReplayMisses int64 `json:"vp9_inter_leaf_cache_replay_misses"`
	// VP9VarPartChooseCalls counts 64x64 variance-partition prepass invocations.
	VP9VarPartChooseCalls int64 `json:"vp9_varpart_choose_calls"`
	// VP9VarPartCacheHits counts choose_partitioning SB-cache hits.
	VP9VarPartCacheHits int64 `json:"vp9_varpart_cache_hits"`
	// VP9VarPartCacheMisses counts choose_partitioning SB-cache misses.
	VP9VarPartCacheMisses int64 `json:"vp9_varpart_cache_misses"`
	// VP9VarPartMergedSBs counts computed SB flags merged from threaded count workers.
	VP9VarPartMergedSBs int64 `json:"vp9_varpart_merged_sbs"`
	// VP9VarPartChooseCountPass counts variance-partition invocations in the count pre-pass.
	VP9VarPartChooseCountPass int64 `json:"vp9_varpart_choose_count_pass"`
	// VP9VarPartChooseWritePass counts variance-partition invocations in the bitstream pass.
	VP9VarPartChooseWritePass int64 `json:"vp9_varpart_choose_write_pass"`
	// VP9VarPartCopyHits counts choose_partitioning copy_partitioning successes.
	VP9VarPartCopyHits int64 `json:"vp9_varpart_copy_hits"`
	// VP9VarPartContentState* count choose_partitioning content-state inputs.
	VP9VarPartContentStateInvalid            int64 `json:"vp9_varpart_content_state_invalid"`
	VP9VarPartContentStateLowSadLowSumdiff   int64 `json:"vp9_varpart_content_state_low_sad_low_sumdiff"`
	VP9VarPartContentStateLowSadHighSumdiff  int64 `json:"vp9_varpart_content_state_low_sad_high_sumdiff"`
	VP9VarPartContentStateHighSadLowSumdiff  int64 `json:"vp9_varpart_content_state_high_sad_low_sumdiff"`
	VP9VarPartContentStateHighSadHighSumdiff int64 `json:"vp9_varpart_content_state_high_sad_high_sumdiff"`
	VP9VarPartContentStateLowVarHighSumdiff  int64 `json:"vp9_varpart_content_state_low_var_high_sumdiff"`
	VP9VarPartContentStateVeryHighSad        int64 `json:"vp9_varpart_content_state_very_high_sad"`
	// VP9VarPart* split counters instrument choose_partitioning's variance tree.
	VP9VarPartYSADValid            int64 `json:"vp9_varpart_ysad_valid"`
	VP9VarPartYSADSelect64x64      int64 `json:"vp9_varpart_ysad_select_64x64"`
	VP9VarPartCopyPartitionSelect  int64 `json:"vp9_varpart_copy_partition_select"`
	VP9VarPartForceSplit64         int64 `json:"vp9_varpart_force_split_64"`
	VP9VarPartForceSplit32         int64 `json:"vp9_varpart_force_split_32"`
	VP9VarPartForceSplit16         int64 `json:"vp9_varpart_force_split_16"`
	VP9VarPartForceSplit16Variance int64 `json:"vp9_varpart_force_split_16_variance"`
	VP9VarPartForceSplit16Minmax   int64 `json:"vp9_varpart_force_split_16_minmax"`
	VP9VarPartThreshold2Count      int64 `json:"vp9_varpart_threshold2_count"`
	VP9VarPartThreshold2Sum        int64 `json:"vp9_varpart_threshold2_sum"`
	VP9VarPartVar16Samples         int64 `json:"vp9_varpart_var16_samples"`
	VP9VarPartVar16Sum             int64 `json:"vp9_varpart_var16_sum"`
	VP9VarPartForce16VarianceSum   int64 `json:"vp9_varpart_force16_variance_sum"`
	VP9VarPartForce16ThresholdSum  int64 `json:"vp9_varpart_force16_threshold_sum"`
	VP9VarPartSetVTCalls           int64 `json:"vp9_varpart_setvt_calls"`
	VP9VarPartSetVT64x64           int64 `json:"vp9_varpart_setvt_64x64"`
	VP9VarPartSetVT32x32           int64 `json:"vp9_varpart_setvt_32x32"`
	VP9VarPartSetVT16x16           int64 `json:"vp9_varpart_setvt_16x16"`
	VP9VarPartSetVT8x8             int64 `json:"vp9_varpart_setvt_8x8"`
	VP9VarPartSetVTForceSplit      int64 `json:"vp9_varpart_setvt_force_split"`
	VP9VarPartSetVTForceSplit64x64 int64 `json:"vp9_varpart_setvt_force_split_64x64"`
	VP9VarPartSetVTForceSplit32x32 int64 `json:"vp9_varpart_setvt_force_split_32x32"`
	VP9VarPartSetVTForceSplit16x16 int64 `json:"vp9_varpart_setvt_force_split_16x16"`
	VP9VarPartSetVTSelect          int64 `json:"vp9_varpart_setvt_select"`
	VP9VarPartSetVTSplit           int64 `json:"vp9_varpart_setvt_split"`
	// VP9InterPredictionBlocks counts VP9 inter-predictor block builds.
	VP9InterPredictionBlocks int64 `json:"vp9_inter_prediction_blocks"`
	// VP9InterPredictPlaneCalls counts VP9 per-plane inter-predictor calls.
	VP9InterPredictPlaneCalls int64 `json:"vp9_inter_predict_plane_calls"`
	// VP9InterPredictionVarianceCalls counts VP9 predictor+variance scoring calls.
	VP9InterPredictionVarianceCalls int64 `json:"vp9_inter_prediction_variance_calls"`
	// VP9InterPredictor* count libvpx inter_predictor dispatch keys.
	VP9InterPredictorCopy     int64 `json:"vp9_inter_predictor_copy"`
	VP9InterPredictorAvg      int64 `json:"vp9_inter_predictor_avg"`
	VP9InterPredictorVert     int64 `json:"vp9_inter_predictor_vert"`
	VP9InterPredictorAvgVert  int64 `json:"vp9_inter_predictor_avg_vert"`
	VP9InterPredictorHoriz    int64 `json:"vp9_inter_predictor_horiz"`
	VP9InterPredictorAvgHoriz int64 `json:"vp9_inter_predictor_avg_horiz"`
	VP9InterPredictor2D       int64 `json:"vp9_inter_predictor_2d"`
	VP9InterPredictorAvg2D    int64 `json:"vp9_inter_predictor_avg_2d"`

	// VP9TileWorker* count persistent tile-worker scheduling activity.
	VP9TileWorkerCountEpochs   int64 `json:"vp9_tile_worker_count_epochs"`
	VP9TileWorkerEncodeEpochs  int64 `json:"vp9_tile_worker_encode_epochs"`
	VP9TileWorkerWakeSignals   int64 `json:"vp9_tile_worker_wake_signals"`
	VP9TileWorkerParks         int64 `json:"vp9_tile_worker_parks"`
	VP9TileWorkerWaitSpins     int64 `json:"vp9_tile_worker_wait_spins"`
	VP9TileWorkerWaitGoscheds  int64 `json:"vp9_tile_worker_wait_goscheds"`
	VP9TileWorkerCountJobRuns  int64 `json:"vp9_tile_worker_count_job_runs"`
	VP9TileWorkerEncodeJobRuns int64 `json:"vp9_tile_worker_encode_job_runs"`
	VP9RowWorkerCountEpochs    int64 `json:"vp9_row_worker_count_epochs"`
	VP9RowWorkerCountJobs      int64 `json:"vp9_row_worker_count_jobs"`
}

// Reset clears all accumulated phase counters.
func (s *EncoderPhaseStats) Reset() {
	if s == nil {
		return
	}
	*s = EncoderPhaseStats{}
}
