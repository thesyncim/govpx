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

// NoiseEstimateUpdateArgs carries the frame-local inputs used by libvpx's
// vp9_update_noise_estimate realtime CBR/cyclic-AQ branch. The caller keeps
// ownership of frame buffers and cyclic-refresh state; this method only reads
// pixels and updates the supplied noise-estimate state.
type NoiseEstimateUpdateArgs struct {
	Width             int
	Height            int
	FrameCounter      int
	NoiseSensitivity  int8
	MIRows            int
	MICols            int
	IntraOnly         bool
	SourceY           []byte
	SourceYStride     int
	SourceWidth       int
	SourceHeight      int
	LastSourceY       []byte
	LastSourceYStride int
	LastSourceWidth   int
	LastSourceHeight  int
	LastSourceValid   bool
	ConsecZeroMV      []uint8
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

// Update ports libvpx's vp9_update_noise_estimate (vp9/encoder/
// vp9_noise_estimate.c:109-302) for the single-layer non-denoiser realtime
// path. It samples steady 16x16 blocks every eighth frame, buckets
// source-vs-last-source variance into MAX_VAR_HIST_BINS, and updates the
// smoothed noise value, counter, and level.
func (ne *NoiseEstimateState) Update(args NoiseEstimateUpdateArgs) {
	if ne == nil {
		return
	}

	if args.IntraOnly {
		for i := range args.ConsecZeroMV {
			args.ConsecZeroMV[i] = 0
		}
	}

	threshConsecZeroMV := 6
	if args.NoiseSensitivity > 0 && args.Width > 640 && args.Width <= 1920 {
		threshConsecZeroMV = 2
	}

	lastSourceValid := args.LastSourceValid &&
		args.LastSourceWidth == args.Width && args.LastSourceHeight == args.Height
	sourceValid := args.SourceWidth == args.Width && args.SourceHeight == args.Height
	if args.NoiseSensitivity > 0 ||
		!ne.Enabled ||
		args.FrameCounter%8 != 0 ||
		!lastSourceValid ||
		!sourceValid ||
		args.MIRows <= 0 ||
		args.MICols <= 0 ||
		len(args.ConsecZeroMV) < args.MIRows*args.MICols ||
		ne.LastW != args.Width ||
		ne.LastH != args.Height {
		if lastSourceValid {
			ne.LastW = args.Width
			ne.LastH = args.Height
		}
		return
	}

	numLowMotion := 0
	for miRow := range args.MIRows {
		for miCol := range args.MICols {
			blIndex := miRow*args.MICols + miCol
			if args.ConsecZeroMV[blIndex] > uint8(threshConsecZeroMV) {
				numLowMotion++
			}
		}
	}
	frameLowMotion := true
	if numLowMotion < ((3 * args.MIRows * args.MICols) >> 3) {
		frameLowMotion = false
	}

	var hist [NoiseEstimateMaxVarHistBins]uint32
	for miRow := range args.MIRows {
		for miCol := range args.MICols {
			if miRow%4 != 0 || miCol%4 != 0 ||
				miRow >= args.MIRows-1 || miCol >= args.MICols-1 {
				continue
			}
			blIndex := miRow*args.MICols + miCol
			blIndex1 := blIndex + 1
			blIndex2 := blIndex + args.MICols
			blIndex3 := blIndex2 + 1
			if blIndex3 >= len(args.ConsecZeroMV) {
				continue
			}
			consec := min(int(args.ConsecZeroMV[blIndex]),
				min(int(args.ConsecZeroMV[blIndex1]),
					min(int(args.ConsecZeroMV[blIndex2]), int(args.ConsecZeroMV[blIndex3]))))
			if !frameLowMotion || consec <= threshConsecZeroMV {
				continue
			}
			srcX := miCol << 3
			srcY := miRow << 3
			if srcX+16 > args.Width || srcY+16 > args.Height {
				continue
			}
			variance, _ := BlockDiffVarianceSSE(args.SourceY, args.SourceYStride,
				args.LastSourceY, args.LastSourceYStride, srcX, srcY, srcX, srcY, 16, 16)
			histIndex := variance / 100
			if histIndex < NoiseEstimateMaxVarHistBins {
				hist[histIndex]++
			} else if histIndex < 3*(NoiseEstimateMaxVarHistBins>>1) {
				hist[NoiseEstimateMaxVarHistBins-1]++
			}
		}
	}
	ne.LastW = args.Width
	ne.LastH = args.Height

	if hist[0] > 10 && hist[NoiseEstimateMaxVarHistBins-1] > hist[0]>>2 {
		hist[0] = 0
		hist[1] >>= 2
		hist[2] >>= 2
		hist[3] >>= 2
		hist[4] >>= 1
		hist[5] >>= 1
		hist[6] = 3 * hist[6] >> 1
		hist[NoiseEstimateMaxVarHistBins-1] >>= 1
	}

	var histAvg [NoiseEstimateMaxVarHistBins]uint32
	var maxBin uint32
	var maxBinCount uint32
	for binCnt := range NoiseEstimateMaxVarHistBins {
		switch {
		case binCnt == 0:
			histAvg[binCnt] = (hist[0] + hist[1] + hist[2]) / 3
		case binCnt == NoiseEstimateMaxVarHistBins-1:
			histAvg[binCnt] = hist[NoiseEstimateMaxVarHistBins-1] >> 2
		case binCnt == NoiseEstimateMaxVarHistBins-2:
			histAvg[binCnt] = (hist[binCnt-1] + 2*hist[binCnt] +
				(hist[binCnt+1] >> 1) + 2) >> 2
		default:
			histAvg[binCnt] = (hist[binCnt-1] + 2*hist[binCnt] +
				hist[binCnt+1] + 2) >> 2
		}
		if histAvg[binCnt] > maxBinCount {
			maxBinCount = histAvg[binCnt]
			maxBin = uint32(binCnt)
		}
	}

	ne.Value = (3*ne.Value + int(maxBin)*40) >> 2
	if ne.Level < NoiseLevelMedium && ne.Value > ne.AdaptThresh {
		ne.Count = ne.NumFramesEstimate
	} else {
		ne.Count++
	}
	if ne.Count == ne.NumFramesEstimate {
		ne.NumFramesEstimate = 30
		ne.Count = 0
		ne.Level = ne.ExtractLevel()
	}
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
