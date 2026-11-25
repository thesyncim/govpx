package govpx

import "github.com/thesyncim/govpx/internal/vpx/codecapi"

const (
	// UpstreamLibvpxVersion is the libvpx release tag used for parity
	// porting and the oracle test corpus. Matches the tag pinned by
	// internal/coracle.
	UpstreamLibvpxVersion = codecapi.UpstreamLibvpxVersion
)

// Codec identifies a codec family supported by govpx.
type Codec = codecapi.Codec

const (
	// CodecVP8 selects the VP8 bitstream format.
	CodecVP8 = codecapi.CodecVP8
	// CodecVP9 selects the VP9 profile 0 bitstream format. Other VP9
	// profiles are outside the package scope.
	CodecVP9 = codecapi.CodecVP9
)

const (
	maxVP8Dimension              = codecapi.MaxVP8Dimension
	maxVP9Dimension              = codecapi.MaxVP9Dimension
	maxQuantizer                 = codecapi.MaxQuantizer
	maxARNRFrames                = codecapi.MaxARNRFrames
	defaultDropFramesWaterMark   = codecapi.DefaultDropFramesWaterMark
	libvpxDefaultBufferSizeMs    = codecapi.LibvpxDefaultBufferSizeMs
	libvpxDefaultBufferInitialMs = codecapi.LibvpxDefaultBufferInitialMs
	libvpxDefaultBufferOptimalMs = codecapi.LibvpxDefaultBufferOptimalMs
	maxRateControlOvershootPct   = codecapi.MaxRateControlOvershootPct
	maxRateControlUndershootPct  = codecapi.MaxRateControlUndershootPct
)

func maxInt() int                                 { return codecapi.MaxInt() }
func validRateControlMode(m RateControlMode) bool { return codecapi.ValidRateControlMode(m) }
func validateEncodeFlags(f EncodeFlags) error     { return codecapi.ValidateEncodeFlags(f) }
func vbrMinFrameBandwidthBits(b, pct int) int     { return codecapi.VBRMinFrameBandwidthBits(b, pct) }
func encoderMacroblockCols(w int) int             { return codecapi.EncoderMacroblockCols(w) }
func encoderMacroblockRows(h int) int             { return codecapi.EncoderMacroblockRows(h) }
func nanotime() int64                             { return codecapi.Nanotime() }
func runtimeProcYield(cycles uint32)              { codecapi.RuntimeProcYield(cycles) }
func saturatingAddUint64(a, b uint64) uint64      { return codecapi.SaturatingAddUint64(a, b) }
func newEncoderSourceTimestampState(t timingState) encoderSourceTimestampState {
	return codecapi.NewEncoderSourceTimestampState(codecapi.TimingState{
		TimebaseNum:   t.timebaseNum,
		TimebaseDen:   t.timebaseDen,
		FrameDuration: t.frameDuration,
		FrameRate:     t.frameRate,
	})
}

type encoderSourceTimestampState = codecapi.EncoderSourceTimestampState
type workerWakeSlot = codecapi.WorkerWakeSlot
type encoderPhaseStatsOptions = codecapi.EncoderPhaseStatsOptions

const (
	libvpxTimestampTicksPerSecond = codecapi.LibvpxTimestampTicksPerSecond
	maxInt64Value                 = codecapi.MaxInt64Value
	rowWorkerIdleSpinBudget       = codecapi.RowWorkerIdleSpinBudget
	vp9WorkerIdleSpinBudget       = codecapi.VP9WorkerIdleSpinBudget
	rowWorkerIdleSchedulerBackoff = codecapi.RowWorkerIdleSchedulerBackoff
)
