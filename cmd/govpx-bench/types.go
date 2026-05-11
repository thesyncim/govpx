package main

import (
	govpx "github.com/thesyncim/govpx"
)

const quantizerHistogramBins = 128

type benchConfig struct {
	Width        int
	Height       int
	Frames       int
	FPS          int
	BitrateKbps  int
	Mode         string
	Decode       bool
	SkipQuality  bool
	Threads      int
	CpuUsed      int
	PhaseTiming  bool
	LibvpxVpxenc string
	LibvpxOracle string
	LibvpxArgs   []string
}

type benchReport struct {
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
// libvpx vpxenc binary is explicitly configured so callers can read a
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
	TimingSource         string        `json:"timing_source"`
	WallNSPerFrame       int64         `json:"wall_ns_per_frame"`
	WallEncodeFPS        float64       `json:"wall_encode_fps"`
	SubprocessOverheadNS int64         `json:"subprocess_overhead_ns"`
	ParityFlags          []string      `json:"parity_flags,omitempty"`
}

type decodeBenchReport struct {
	Decoder                  string                 `json:"decoder"`
	Operation                string                 `json:"operation"`
	Mode                     string                 `json:"mode"`
	Width                    int                    `json:"width"`
	Height                   int                    `json:"height"`
	Frames                   int                    `json:"frames"`
	FPS                      int                    `json:"fps"`
	InputBytes               int                    `json:"input_bytes"`
	DecodedFrames            int                    `json:"decoded_frames"`
	NSPerFrame               int64                  `json:"ns_per_frame"`
	DecodeFPS                float64                `json:"decode_fps"`
	MacroblocksPerSec        float64                `json:"macroblocks_per_second"`
	CodedMegabytesPerSec     float64                `json:"coded_megabytes_per_second"`
	AllocsPerFrame           float64                `json:"allocs_per_frame"`
	LatencyNS                latencyReport          `json:"latency_ns"`
	Reference                *decodeReferenceReport `json:"reference,omitempty"`
	RelativeSpeedVsReference float64                `json:"relative_speed_vs_reference,omitempty"`
	Options                  benchConfigSummary     `json:"options"`
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

// encoderParity captures the rate-control knobs that have to match between
// govpx and libvpx for the comparison to be apples-to-apples. Both
// newBenchmarkEncoder and runLibvpxBenchmark consume this so the two encoders
// see the same problem (CBR, same buffer sizes, same q-range, same kf
// cadence, single-pass, matched thread count, zero lag).
type encoderParity struct {
	MinQuantizer        int
	MaxQuantizer        int
	KeyFrameInterval    int
	BufferSizeMs        int
	BufferInitialSizeMs int
	BufferOptimalSizeMs int
	UndershootPct       int
	OvershootPct        int
	Threads             int
	TokenPartitions     int
	CpuUsed             int
}
