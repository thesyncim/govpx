package benchcmd

import (
	govpx "github.com/thesyncim/govpx"
)

const quantizerHistogramBins = 128

type benchConfig struct {
	Width               int
	Height              int
	Frames              int
	FPS                 int
	BitrateKbps         int
	Mode                string
	Codec               string
	Decode              bool
	SkipQuality         bool
	Threads             int
	NoiseSensitivity    int
	NoiseSensitivitySet bool
	CpuUsed             int
	PhaseTiming         bool
	CPUProfile          string
	LibvpxVpxenc        string
	LibvpxVpxencVP9     string
	LibvpxOracle        string
	LibvpxArgs          []string

	// QualityGate is consulted by runBenchmark/Main after the encode pass
	// completes. When Enabled is true the bench exits non-zero on regression.
	QualityGate QualityGate
}

// codec values for benchConfig.Codec.
const (
	codecVP8 = "vp8"
	codecVP9 = "vp9"
)

func benchCodec(cfg benchConfig) string {
	if cfg.Codec == codecVP9 {
		return codecVP9
	}
	return codecVP8
}

type benchReport struct {
	Codec             string                   `json:"codec,omitempty"`
	Encoder           string                   `json:"encoder"`
	Mode              string                   `json:"mode"`
	Width             int                      `json:"width"`
	Height            int                      `json:"height"`
	Frames            int                      `json:"frames"`
	FPS               int                      `json:"fps"`
	TargetBitrateKbps int                      `json:"target_bitrate_kbps"`
	OutputBitrateKbps float64                  `json:"output_bitrate_kbps"`
	BitrateErrorPct   float64                  `json:"bitrate_error_pct"`
	NSPerFrame        int64                    `json:"ns_per_frame"`
	EncodeFPS         float64                  `json:"encode_fps"`
	MacroblocksPerSec float64                  `json:"macroblocks_per_second"`
	AllocsPerFrame    float64                  `json:"allocs_per_frame"`
	PSNR              float64                  `json:"psnr"`
	SSIM              float64                  `json:"ssim"`
	QualityFrames     int                      `json:"quality_frames"`
	QualitySkipped    bool                     `json:"quality_skipped,omitempty"`
	KeyframeBytes     int                      `json:"keyframe_bytes"`
	AvgInterBytes     float64                  `json:"avg_interframe_bytes"`
	Quantizers        quantizerReport          `json:"quantizers"`
	LatencyNS         latencyReport            `json:"latency_ns"`
	PhaseNS           *govpx.EncoderPhaseStats `json:"phase_ns,omitempty"`
	OutputBytes       int                      `json:"output_bytes"`
	EncodedFrames     int                      `json:"encoded_frames"`
	DroppedFrames     int                      `json:"dropped_frames"`
	QuantizerHist     map[string]int           `json:"quantizer_histogram"`
	Reference         *referenceReport         `json:"reference,omitempty"`
	Comparison        *comparisonReport        `json:"comparison_vs_reference,omitempty"`
	Options           benchConfigSummary       `json:"options"`
}

type measuredEncodePacket struct {
	data        []byte
	sourceIndex int
}

// comparisonReport summarizes how govpx compared against the libvpx
// reference encoder on the same input. It is populated only when a
// libvpx vpxenc binary is configured or auto-located so callers can read a
// single "did we beat libvpx?" snapshot without diffing the full reference
// block manually.
type comparisonReport struct {
	BitrateRatioVsReference float64 `json:"bitrate_ratio_vs_reference"`
	BitrateDeltaKbps        float64 `json:"bitrate_delta_kbps"`
	BitrateErrorPctDelta    float64 `json:"bitrate_error_pct_delta"`
	PSNRDeltaDB             float64 `json:"psnr_delta_db"`
	SSIMDelta               float64 `json:"ssim_delta"`
	EncodeFPSRatio          float64 `json:"encode_fps_ratio_vs_reference"`
	NSPerFrameRatio         float64 `json:"ns_per_frame_ratio_vs_reference"`
	OutputBytesRatio        float64 `json:"output_bytes_ratio_vs_reference"`
	AvgInterBytesRatio      float64 `json:"avg_interframe_bytes_ratio_vs_reference"`
	KeyframeBytesRatio      float64 `json:"keyframe_bytes_ratio_vs_reference"`
	EncodedFramesDelta      int     `json:"encoded_frames_delta"`
	DroppedFramesDelta      int     `json:"dropped_frames_delta"`
}

type referenceReport struct {
	Encoder              string        `json:"encoder"`
	Mode                 string        `json:"mode"`
	OutputBitrateKbps    float64       `json:"output_bitrate_kbps"`
	BitrateErrorPct      float64       `json:"bitrate_error_pct"`
	NSPerFrame           int64         `json:"ns_per_frame"`
	EncodeFPS            float64       `json:"encode_fps"`
	MacroblocksPerSec    float64       `json:"macroblocks_per_second"`
	PSNR                 float64       `json:"psnr"`
	SSIM                 float64       `json:"ssim"`
	QualityFrames        int           `json:"quality_frames"`
	QualitySkipped       bool          `json:"quality_skipped,omitempty"`
	QualityError         string        `json:"quality_error,omitempty"`
	KeyframeBytes        int           `json:"keyframe_bytes"`
	AvgInterBytes        float64       `json:"avg_interframe_bytes"`
	LatencyNS            latencyReport `json:"latency_ns"`
	OutputBytes          int           `json:"output_bytes"`
	EncodedFrames        int           `json:"encoded_frames"`
	DroppedFrames        int           `json:"dropped_frames"`
	TimingSource         string        `json:"timing_source"`
	WallNSPerFrame       int64         `json:"wall_ns_per_frame"`
	WallEncodeFPS        float64       `json:"wall_encode_fps"`
	SubprocessOverheadNS int64         `json:"subprocess_overhead_ns"`
	ParityFlags          []string      `json:"parity_flags,omitempty"`
	VP9CallStats         *vp9CallStats `json:"vp9_call_stats,omitempty"`
}

type vp9CallStats struct {
	InterModePicks                        uint64 `json:"inter_mode_picks,omitempty"`
	InterModeSub8x8Picks                  uint64 `json:"inter_mode_sub8x8_picks,omitempty"`
	BuildSBY                              uint64 `json:"build_sby,omitempty"`
	BuildSBP                              uint64 `json:"build_sbp,omitempty"`
	BuildSBUV                             uint64 `json:"build_sbuv,omitempty"`
	BuildSB                               uint64 `json:"build_sb,omitempty"`
	BuildPlanes                           uint64 `json:"build_planes,omitempty"`
	SinglePredictorBuilds                 uint64 `json:"single_predictor_builds,omitempty"`
	FullpelSearches                       uint64 `json:"fullpel_searches,omitempty"`
	SADCalls                              uint64 `json:"sad_calls,omitempty"`
	SADCandidates                         uint64 `json:"sad_candidates,omitempty"`
	SADBatchCalls                         uint64 `json:"sad_batch_calls,omitempty"`
	PredictorCopy                         uint64 `json:"predictor_copy,omitempty"`
	PredictorAvg                          uint64 `json:"predictor_avg,omitempty"`
	PredictorVert                         uint64 `json:"predictor_vert,omitempty"`
	PredictorAvgVert                      uint64 `json:"predictor_avg_vert,omitempty"`
	PredictorHoriz                        uint64 `json:"predictor_horiz,omitempty"`
	PredictorAvgHoriz                     uint64 `json:"predictor_avg_horiz,omitempty"`
	Predictor2D                           uint64 `json:"predictor_2d,omitempty"`
	PredictorAvg2D                        uint64 `json:"predictor_avg_2d,omitempty"`
	ModeBlock64x64                        uint64 `json:"mode_block_64x64,omitempty"`
	ModeBlock32x32                        uint64 `json:"mode_block_32x32,omitempty"`
	ModeBlock32x16                        uint64 `json:"mode_block_32x16,omitempty"`
	ModeBlock16x32                        uint64 `json:"mode_block_16x32,omitempty"`
	ModeBlock16x16                        uint64 `json:"mode_block_16x16,omitempty"`
	ModeBlock16x8                         uint64 `json:"mode_block_16x8,omitempty"`
	ModeBlock8x16                         uint64 `json:"mode_block_8x16,omitempty"`
	ModeBlock8x8                          uint64 `json:"mode_block_8x8,omitempty"`
	ModeBlockSub8                         uint64 `json:"mode_block_sub8,omitempty"`
	VarpartChooseCalls                    uint64 `json:"varpart_choose_calls,omitempty"`
	VarpartCopyHits                       uint64 `json:"varpart_copy_hits,omitempty"`
	VarpartContentStateInvalid            uint64 `json:"varpart_content_state_invalid,omitempty"`
	VarpartContentStateLowSadLowSumdiff   uint64 `json:"varpart_content_state_low_sad_low_sumdiff,omitempty"`
	VarpartContentStateLowSadHighSumdiff  uint64 `json:"varpart_content_state_low_sad_high_sumdiff,omitempty"`
	VarpartContentStateHighSadLowSumdiff  uint64 `json:"varpart_content_state_high_sad_low_sumdiff,omitempty"`
	VarpartContentStateHighSadHighSumdiff uint64 `json:"varpart_content_state_high_sad_high_sumdiff,omitempty"`
	VarpartContentStateLowVarHighSumdiff  uint64 `json:"varpart_content_state_low_var_high_sumdiff,omitempty"`
	VarpartContentStateVeryHighSad        uint64 `json:"varpart_content_state_very_high_sad,omitempty"`
	VarpartYSADValid                      uint64 `json:"varpart_ysad_valid,omitempty"`
	VarpartYSADSelect64x64                uint64 `json:"varpart_ysad_select_64x64,omitempty"`
	VarpartCopyPartitionSelect            uint64 `json:"varpart_copy_partition_select,omitempty"`
	VarpartForceSplit64                   uint64 `json:"varpart_force_split_64,omitempty"`
	VarpartForceSplit32                   uint64 `json:"varpart_force_split_32,omitempty"`
	VarpartForceSplit16                   uint64 `json:"varpart_force_split_16,omitempty"`
	VarpartForceSplit16Variance           uint64 `json:"varpart_force_split_16_variance,omitempty"`
	VarpartForceSplit16Minmax             uint64 `json:"varpart_force_split_16_minmax,omitempty"`
	VarpartThreshold2Count                uint64 `json:"varpart_threshold2_count,omitempty"`
	VarpartThreshold2Sum                  uint64 `json:"varpart_threshold2_sum,omitempty"`
	VarpartVar16Samples                   uint64 `json:"varpart_var16_samples,omitempty"`
	VarpartVar16Sum                       uint64 `json:"varpart_var16_sum,omitempty"`
	VarpartForce16VarianceSum             uint64 `json:"varpart_force16_variance_sum,omitempty"`
	VarpartForce16ThresholdSum            uint64 `json:"varpart_force16_threshold_sum,omitempty"`
	VarpartSetVTCalls                     uint64 `json:"varpart_setvt_calls,omitempty"`
	VarpartSetVT64x64                     uint64 `json:"varpart_setvt_64x64,omitempty"`
	VarpartSetVT32x32                     uint64 `json:"varpart_setvt_32x32,omitempty"`
	VarpartSetVT16x16                     uint64 `json:"varpart_setvt_16x16,omitempty"`
	VarpartSetVT8x8                       uint64 `json:"varpart_setvt_8x8,omitempty"`
	VarpartSetVTForceSplit                uint64 `json:"varpart_setvt_force_split,omitempty"`
	VarpartSetVTForceSplit64x64           uint64 `json:"varpart_setvt_force_split_64x64,omitempty"`
	VarpartSetVTForceSplit32x32           uint64 `json:"varpart_setvt_force_split_32x32,omitempty"`
	VarpartSetVTForceSplit16x16           uint64 `json:"varpart_setvt_force_split_16x16,omitempty"`
	VarpartSetVTSelect                    uint64 `json:"varpart_setvt_select,omitempty"`
	VarpartSetVTSplit                     uint64 `json:"varpart_setvt_split,omitempty"`
}

func (s vp9CallStats) ModeBlocks() uint64 {
	return s.ModeBlock64x64 +
		s.ModeBlock32x32 +
		s.ModeBlock32x16 +
		s.ModeBlock16x32 +
		s.ModeBlock16x16 +
		s.ModeBlock16x8 +
		s.ModeBlock8x16 +
		s.ModeBlock8x8 +
		s.ModeBlockSub8
}

func (s vp9CallStats) VarpartContentStates() uint64 {
	return s.VarpartContentStateInvalid +
		s.VarpartContentStateLowSadLowSumdiff +
		s.VarpartContentStateLowSadHighSumdiff +
		s.VarpartContentStateHighSadLowSumdiff +
		s.VarpartContentStateHighSadHighSumdiff +
		s.VarpartContentStateLowVarHighSumdiff +
		s.VarpartContentStateVeryHighSad
}

func (s vp9CallStats) VarpartSetVTBlockCalls() uint64 {
	return s.VarpartSetVT64x64 +
		s.VarpartSetVT32x32 +
		s.VarpartSetVT16x16 +
		s.VarpartSetVT8x8
}

type decodeBenchReport struct {
	Codec                    string                  `json:"codec,omitempty"`
	Decoder                  string                  `json:"decoder"`
	Operation                string                  `json:"operation"`
	Mode                     string                  `json:"mode"`
	Width                    int                     `json:"width"`
	Height                   int                     `json:"height"`
	Frames                   int                     `json:"frames"`
	FPS                      int                     `json:"fps"`
	InputBytes               int                     `json:"input_bytes"`
	DecodedFrames            int                     `json:"decoded_frames"`
	NSPerFrame               int64                   `json:"ns_per_frame"`
	DecodeFPS                float64                 `json:"decode_fps"`
	MacroblocksPerSec        float64                 `json:"macroblocks_per_second"`
	CodedMegabytesPerSec     float64                 `json:"coded_megabytes_per_second"`
	AllocsPerFrame           float64                 `json:"allocs_per_frame"`
	LatencyNS                latencyReport           `json:"latency_ns"`
	Reference                *decodeReferenceReport  `json:"reference,omitempty"`
	Comparison               *decodeComparisonReport `json:"comparison_vs_reference,omitempty"`
	RelativeSpeedVsReference float64                 `json:"relative_speed_vs_reference,omitempty"`
	Options                  benchConfigSummary      `json:"options"`
}

type decodeComparisonReport struct {
	NSPerFrameRatio           float64 `json:"ns_per_frame_ratio_vs_reference"`
	DecodeFPSRatio            float64 `json:"decode_fps_ratio_vs_reference"`
	CodedMegabytesPerSecRatio float64 `json:"coded_megabytes_per_second_ratio_vs_reference"`
	DecodedFramesDelta        int     `json:"decoded_frames_delta"`
}

type decodeReferenceReport struct {
	Decoder              string        `json:"decoder"`
	Mode                 string        `json:"mode"`
	DecodedFrames        int           `json:"decoded_frames"`
	NSPerFrame           int64         `json:"ns_per_frame"`
	DecodeFPS            float64       `json:"decode_fps"`
	MacroblocksPerSec    float64       `json:"macroblocks_per_second"`
	CodedMegabytesPerSec float64       `json:"coded_megabytes_per_second"`
	LatencyNS            latencyReport `json:"latency_ns"`
	TimingSource         string        `json:"timing_source"`
	WallNSPerFrame       int64         `json:"wall_ns_per_frame"`
	WallDecodeFPS        float64       `json:"wall_decode_fps"`
	SubprocessOverheadNS int64         `json:"subprocess_overhead_ns"`
}

type quantizerReport struct {
	Min  int     `json:"min"`
	Max  int     `json:"max"`
	Mean float64 `json:"mean"`
}

type latencyReport struct {
	P50 int64 `json:"p50"`
	P95 int64 `json:"p95"`
	P99 int64 `json:"p99"`
}

type benchConfigSummary struct {
	Deadline string `json:"deadline"`
}

type suiteReport struct {
	Name          string            `json:"name"`
	Runs          int               `json:"runs"`
	Selector      string            `json:"selector"`
	LibvpxVpxenc  string            `json:"libvpx_vpxenc,omitempty"`
	PhaseTiming   bool              `json:"phase_timing,omitempty"`
	QualitySkip   bool              `json:"quality_skipped,omitempty"`
	Cases         []suiteCaseReport `json:"cases"`
	GeomeanNSGap  float64           `json:"geomean_ns_frame_gap"`
	GeomeanFPSGap float64           `json:"geomean_encode_fps_gap"`
}

type suiteCaseReport struct {
	Name   string      `json:"name"`
	Report benchReport `json:"report"`
}

// encoderParity captures the rate-control knobs that have to match between
// govpx and libvpx for the comparison to be apples-to-apples. Both
// newBenchmarkEncoder and runLibvpxBenchmark consume this so the two encoders
// see the same problem (CBR, same buffer sizes, same q-range, same kf
// cadence, realtime drop/intra/noise knobs when enabled, single-pass, matched
// thread count, zero lag).
type encoderParity struct {
	MinQuantizer        int
	MaxQuantizer        int
	KeyFrameInterval    int
	BufferSizeMs        int
	BufferInitialSizeMs int
	BufferOptimalSizeMs int
	UndershootPct       int
	OvershootPct        int
	MaxIntraBitratePct  int
	DropFrameAllowed    bool
	DropFrameWaterMark  int
	NoiseSensitivity    int
	StaticThreshold     int
	Threads             int
	TokenPartitions     int
	CpuUsed             int
}
