package encoder

// NoiseEstimateMaxVarHistBins mirrors libvpx's MAX_VAR_HIST_BINS
// (vp9/encoder/vp9_noise_estimate.h:26).
const NoiseEstimateMaxVarHistBins = 20

// NoiseEstimateState mirrors libvpx's NOISE_ESTIMATE struct
// (vp9/encoder/vp9_noise_estimate.h:30-40).
type NoiseEstimateState struct {
	Enabled           bool
	Level             NoiseLevel
	Value             int
	Thresh            int
	AdaptThresh       int
	Count             int
	LastW             int
	LastH             int
	NumFramesEstimate int
}

// Init ports libvpx's vp9_noise_estimate_init
// (vp9/encoder/vp9_noise_estimate.c:33-50).
func (ne *NoiseEstimateState) Init(width, height int) {
	if ne == nil {
		return
	}
	ne.Enabled = false
	if width*height < 1280*720 {
		ne.Level = NoiseLevelLowLow
	} else {
		ne.Level = NoiseLevelLow
	}
	ne.Value = 0
	ne.Count = 0
	ne.Thresh = 90
	ne.LastW = 0
	ne.LastH = 0
	if width*height >= 1920*1080 {
		ne.Thresh = 200
	} else if width*height >= 1280*720 {
		ne.Thresh = 140
	} else if width*height >= 640*360 {
		ne.Thresh = 115
	}
	ne.NumFramesEstimate = 15
	ne.AdaptThresh = (3 * ne.Thresh) >> 1
}

// ExtractLevel ports libvpx's vp9_noise_estimate_extract_level
// (vp9/encoder/vp9_noise_estimate.c:94-107).
func (ne *NoiseEstimateState) ExtractLevel() NoiseLevel {
	if ne == nil {
		return NoiseLevelLowLow
	}
	if ne.Value > (ne.Thresh << 1) {
		return NoiseLevelHigh
	}
	if ne.Value > ne.Thresh {
		return NoiseLevelMedium
	}
	if ne.Value > (ne.Thresh >> 1) {
		return NoiseLevelLow
	}
	return NoiseLevelLowLow
}

// EnableNoiseEstimationArgs carries the predicate inputs libvpx's
// enable_noise_estimation reads off cpi fields.
type EnableNoiseEstimationArgs struct {
	UseHighBitdepth     bool
	NoiseSensitivity    int8
	UseSVC              bool
	Pass                int
	RcModeCBR           bool
	AqModeCyclicRefresh bool
	Speed               int
	ResizeStateOrig     bool
	ResizePending       bool
	ScreenContent       bool
	Width               int
	Height              int
}

// EnableNoiseEstimation ports libvpx's enable_noise_estimation
// (vp9/encoder/vp9_noise_estimate.c:52-74).
func EnableNoiseEstimation(args EnableNoiseEstimationArgs) bool {
	if args.UseHighBitdepth {
		return false
	}
	// CONFIG_VP9_TEMPORAL_DENOISING branch. govpx wires this only for
	// single-layer encoders; SVC enables it at the top spatial layer later.
	if args.NoiseSensitivity > 0 && !args.UseSVC &&
		args.Width >= 320 && args.Height >= 180 {
		return true
	}
	if args.Pass == 0 && args.RcModeCBR && args.AqModeCyclicRefresh &&
		args.Speed >= 5 && args.ResizeStateOrig && !args.ResizePending &&
		!args.UseSVC && !args.ScreenContent &&
		args.Width*args.Height >= 640*360 {
		return true
	}
	return false
}
