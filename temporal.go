package govpx

const (
	// MaxTemporalLayers is the largest VP8 temporal-layer count exposed by
	// govpx. It bounds [TemporalScalabilityConfig.LayerTargetBitrateKbps]
	// and the per-layer counters on [EncodeResult].
	MaxTemporalLayers     = 5
	maxTemporalPeriodSize = 16
)

// TemporalLayeringMode selects a built-in temporal scalability pattern.
// Values mirror libvpx's VP8E_TEMPORAL_LAYERING_MODE_* enum; the
// underlying frame-by-frame layer IDs and reference flags follow the
// corresponding libvpx VP8 patterns.
type TemporalLayeringMode int

const (
	// TemporalLayeringOneLayer disables temporal layering. The encoder
	// produces a single base-layer stream.
	TemporalLayeringOneLayer TemporalLayeringMode = iota
	// TemporalLayeringTwoLayers selects libvpx's two-layer 2-frame pattern.
	TemporalLayeringTwoLayers
	// TemporalLayeringTwoLayersThreeFrame selects libvpx's two-layer 3-frame
	// pattern.
	TemporalLayeringTwoLayersThreeFrame
	// TemporalLayeringThreeLayersSixFrame selects libvpx's three-layer
	// 6-frame pattern.
	TemporalLayeringThreeLayersSixFrame
	// TemporalLayeringThreeLayersNoInterLayerPrediction selects a three-layer
	// pattern with no inter-layer prediction.
	TemporalLayeringThreeLayersNoInterLayerPrediction
	// TemporalLayeringThreeLayersLayerOnePrediction selects a three-layer
	// pattern that allows prediction from layer one.
	TemporalLayeringThreeLayersLayerOnePrediction
	// TemporalLayeringThreeLayers selects libvpx's default three-layer
	// 4-frame pattern.
	TemporalLayeringThreeLayers
	// TemporalLayeringFiveLayers selects libvpx's five-layer 16-frame
	// pattern.
	TemporalLayeringFiveLayers
	// TemporalLayeringTwoLayersWithSync selects a two-layer pattern that
	// emits libvpx-style temporal sync frames.
	TemporalLayeringTwoLayersWithSync
	// TemporalLayeringThreeLayersWithSync selects a three-layer pattern that
	// emits libvpx-style temporal sync frames.
	TemporalLayeringThreeLayersWithSync
	// TemporalLayeringThreeLayersAltRefWithSync selects a three-layer
	// pattern with alt-ref-backed sync frames.
	TemporalLayeringThreeLayersAltRefWithSync
	// TemporalLayeringThreeLayersOneReference selects a three-layer pattern
	// that uses a single reference across layers.
	TemporalLayeringThreeLayersOneReference
	// TemporalLayeringThreeLayersNoSync selects a three-layer pattern that
	// omits sync signaling.
	TemporalLayeringThreeLayersNoSync
)

// TemporalScalabilityConfig configures automatic temporal-layer
// scheduling. The zero value disables temporal layering.
type TemporalScalabilityConfig struct {
	// Enabled turns on temporal layering when true.
	Enabled bool
	// Mode selects the built-in layer pattern.
	Mode TemporalLayeringMode

	// LayerTargetBitrateKbps holds per-layer target bitrates in cumulative
	// form, matching libvpx's ts_target_bitrate[]: entry i is the sum of
	// the bitrate budgeted to layers 0..i. Unused trailing entries must
	// be zero. The last non-zero entry should equal
	// [EncoderOptions.TargetBitrateKbps]. An all-zero array auto-derives
	// a default 60/40 split (two layers) or 40/20/40 split (three
	// layers) from TargetBitrateKbps.
	LayerTargetBitrateKbps [MaxTemporalLayers]int
}

type temporalPattern struct {
	Layers          int
	Periodicity     int
	FlagPeriodicity int
	RateDecimator   [MaxTemporalLayers]int
	LayerID         [maxTemporalPeriodSize]int
	Flags           [maxTemporalPeriodSize]EncodeFlags
}

type temporalState struct {
	enabled         bool
	autoBitrate     bool
	config          TemporalScalabilityConfig
	pattern         temporalPattern
	layerIDOverride int
	frameIndex      uint64
	tl0PicIdx       uint8
	tl0Valid        bool
	refLayer        [temporalReferenceCount]int
	accounting      [MaxTemporalLayers]temporalLayerAccounting
	buffersSet      bool
	// codingState mirrors the subset of libvpx LAYER_CONTEXT fields that
	// influence subsequent-frame byte output. libvpx
	// vp8_save_layer_context / vp8_restore_layer_context (vp8/encoder/onyx_if.c)
	// stash these around every encode so each temporal layer sees its own
	// previous-frame state instead of the trailing layer's. govpx tracks
	// only the strict subset needed to keep the encoded bitstream
	// byte-identical (currently filter_level, which seeds the LF picker
	// bracket midpoint). Additional fields (rate-control state, mode
	// counts) live in their own per-layer structures and don't need a
	// separate save/restore hop here.
	codingState [MaxTemporalLayers]temporalLayerCodingState
	codingValid [MaxTemporalLayers]bool
}

// temporalLayerCodingState captures the per-layer pieces of libvpx
// LAYER_CONTEXT that survive a frame boundary and feed the next encode of
// the same layer. Only fields that demonstrably move the encoded bitstream
// are tracked here; the rest stay shared with the encoder's global state
// (libvpx itself shares them across the recode loop and only splits them
// per-layer for save/restore).
type temporalLayerCodingState struct {
	// FilterLevel mirrors LAYER_CONTEXT.filter_level. The LF picker
	// (encoder_loopfilter.go pickFull / pickFast) uses the previous
	// frame's filter_level as the bracket midpoint, and that bracket
	// midpoint chains across frames at the same temporal layer in
	// libvpx. Without per-layer tracking the L1/L2 LF picker seeds with
	// whatever the most-recently-encoded L0 frame chose, which steers
	// the bracketed search down a different branch and drifts the
	// uncompressed LF header byte for the next L1/L2 frame.
	FilterLevel uint8
}

type temporalFrame struct {
	Enabled                    bool
	LayerID                    int
	LayerCount                 int
	LayerSync                  bool
	TL0PICIDX                  uint8
	Flags                      EncodeFlags
	LayerTargetBitrateKbps     int
	LayerFrameTargetBits       int
	LayerCumulativeBitrateKbps int
}

type temporalLayerAccounting struct {
	InputFrames        int
	EncodedFrames      int
	TotalEncodedFrames int
	EncodedBits        int
	FrameBandwidthBits int
	MaximumBufferBits  int
	BufferLevelBits    int
}

type temporalBufferConfig struct {
	timing              timingState
	bufferInitialSizeMs int
	bufferSizeMs        int
}

const (
	temporalReferenceLast = iota
	temporalReferenceGolden
	temporalReferenceAltRef
	temporalReferenceCount
)

func (t *temporalState) configure(cfg TemporalScalabilityConfig, totalBitrateKbps int) error {
	if !cfg.Enabled {
		*t = temporalState{}
		return nil
	}
	pattern, ok := temporalLayeringPattern(cfg.Mode)
	if !ok {
		return ErrInvalidConfig
	}
	normalized, autoBitrate, err := normalizeTemporalBitrates(cfg, pattern.Layers, totalBitrateKbps)
	if err != nil {
		return err
	}
	t.enabled = true
	t.autoBitrate = autoBitrate
	t.config = normalized
	t.pattern = pattern
	t.layerIDOverride = -1
	t.frameIndex = 0
	t.tl0PicIdx = 0
	t.tl0Valid = false
	t.refLayer = [temporalReferenceCount]int{}
	t.accounting = [MaxTemporalLayers]temporalLayerAccounting{}
	t.buffersSet = false
	t.codingState = [MaxTemporalLayers]temporalLayerCodingState{}
	t.codingValid = [MaxTemporalLayers]bool{}
	return nil
}

func (t *temporalState) refreshBitrate(totalBitrateKbps int) error {
	if !t.enabled {
		return nil
	}
	if !t.autoBitrate {
		if t.config.LayerTargetBitrateKbps[t.pattern.Layers-1] != totalBitrateKbps {
			return ErrInvalidBitrate
		}
		return nil
	}
	cfg, _, err := normalizeTemporalBitrates(t.config, t.pattern.Layers, totalBitrateKbps)
	if err != nil {
		return err
	}
	t.config.LayerTargetBitrateKbps = cfg.LayerTargetBitrateKbps
	return nil
}

func (t *temporalState) nextFrame(timing timingState) temporalFrame {
	if !t.enabled {
		return temporalFrame{LayerID: 0, LayerCount: 1}
	}
	patternIndex := int(t.frameIndex % uint64(t.pattern.Periodicity))
	flagIndex := int(t.frameIndex % uint64(t.pattern.FlagPeriodicity))
	layerID := t.pattern.LayerID[patternIndex]
	if t.layerIDOverride >= 0 {
		layerID = t.layerIDOverride
	}
	flags := t.pattern.Flags[flagIndex]
	if t.config.Mode != TemporalLayeringFiveLayers && t.frameIndex > 0 && flagIndex == 0 {
		flags &^= EncodeForceKeyFrame
	}
	tl0PicIdx := t.tl0PicIdx
	if layerID == 0 {
		if t.tl0Valid {
			tl0PicIdx++
		} else {
			tl0PicIdx = 0
		}
	}
	meta := temporalFrame{
		Enabled:                    true,
		LayerID:                    layerID,
		LayerCount:                 t.pattern.Layers,
		TL0PICIDX:                  tl0PicIdx,
		Flags:                      flags,
		LayerTargetBitrateKbps:     t.temporalLayerBitrateKbps(layerID),
		LayerFrameTargetBits:       t.temporalLayerFrameTargetBits(layerID, timing),
		LayerCumulativeBitrateKbps: t.config.LayerTargetBitrateKbps[layerID],
	}
	meta.LayerSync = t.layerSync(meta)
	return meta
}

func (t *temporalState) finishFrame(meta temporalFrame, keyFrame bool, showFrame bool, refresh temporalReferenceRefresh, encodedBits int, buffers temporalBufferConfig) {
	if !t.enabled {
		return
	}
	t.accountEncodedFrame(meta, keyFrame, showFrame, encodedBits, buffers)
	if keyFrame {
		t.refLayer = [temporalReferenceCount]int{}
	} else {
		if refresh.Last {
			t.refLayer[temporalReferenceLast] = meta.LayerID
		}
		if refresh.Golden {
			t.refLayer[temporalReferenceGolden] = meta.LayerID
		}
		if refresh.AltRef {
			t.refLayer[temporalReferenceAltRef] = meta.LayerID
		}
	}
	if meta.LayerID == 0 {
		t.tl0PicIdx = meta.TL0PICIDX
		t.tl0Valid = true
	}
	t.frameIndex++
}

func (t *temporalState) finishDroppedFrame(meta temporalFrame, buffers temporalBufferConfig) {
	if t.enabled {
		t.updateLayerBufferConfig(buffers)
		t.accountInputFrame(meta)
		t.accountDroppedFrameBuffer(meta)
		t.frameIndex++
	}
}

func (t *temporalState) accountEncodedFrame(meta temporalFrame, keyFrame bool, showFrame bool, encodedBits int, buffers temporalBufferConfig) {
	t.updateLayerBufferConfig(buffers)
	t.accountInputFrame(meta)
	if uint(meta.LayerID) >= uint(t.pattern.Layers) {
		return
	}
	for layer := meta.LayerID; layer < t.pattern.Layers; layer++ {
		accounting := &t.accounting[layer]
		accounting.TotalEncodedFrames++
		accounting.EncodedBits = saturatingAdd(accounting.EncodedBits, encodedBits)
		t.updateLayerBuffer(accounting, encodedBits, showFrame || layer > meta.LayerID)
	}
	if !keyFrame {
		t.accounting[meta.LayerID].EncodedFrames++
	}
}

func (t *temporalState) accountInputFrame(meta temporalFrame) {
	if uint(meta.LayerID) >= uint(t.pattern.Layers) {
		return
	}
	t.accounting[meta.LayerID].InputFrames++
}

func (t *temporalState) accountDroppedFrameBuffer(meta temporalFrame) {
	if uint(meta.LayerID) >= uint(t.pattern.Layers) {
		return
	}
	for layer := meta.LayerID; layer < t.pattern.Layers; layer++ {
		t.updateLayerBuffer(&t.accounting[layer], 0, true)
	}
}

func (t *temporalState) updateLayerBufferConfig(cfg temporalBufferConfig) {
	for layer := 0; layer < t.pattern.Layers; layer++ {
		targetKbps := t.config.LayerTargetBitrateKbps[layer]
		targetBits, ok := checkedMul(targetKbps, 1000)
		if !ok {
			targetBits = maxInt()
		}
		accounting := &t.accounting[layer]
		accounting.FrameBandwidthBits = computeLayerBitsPerFrame(targetBits, cfg.timing, t.pattern.RateDecimator[layer], 1)
		accounting.MaximumBufferBits = temporalLayerBufferBits(targetKbps, cfg.bufferSizeMs)
		if !t.buffersSet {
			accounting.BufferLevelBits = temporalLayerBufferBits(targetKbps, cfg.bufferInitialSizeMs)
		}
	}
	t.buffersSet = true
}

func (t *temporalState) updateLayerBuffer(accounting *temporalLayerAccounting, encodedBits int, showFrame bool) {
	if showFrame {
		accounting.BufferLevelBits = saturatingAdd(accounting.BufferLevelBits, accounting.FrameBandwidthBits)
	}
	accounting.BufferLevelBits = min(saturatingSub(accounting.BufferLevelBits, encodedBits), accounting.MaximumBufferBits)
}

func temporalLayerBufferBits(targetKbps int, bufferMs int) int {
	value, ok := checkedMul(targetKbps, bufferMs)
	if !ok {
		return maxInt()
	}
	return value
}

func (t *temporalState) layerSync(meta temporalFrame) bool {
	if meta.LayerID <= 0 {
		return false
	}
	if meta.Flags&EncodeNoReferenceLast == 0 && t.refLayer[temporalReferenceLast] >= meta.LayerID {
		return false
	}
	if meta.Flags&EncodeNoReferenceGolden == 0 && t.refLayer[temporalReferenceGolden] >= meta.LayerID {
		return false
	}
	if meta.Flags&EncodeNoReferenceAltRef == 0 && t.refLayer[temporalReferenceAltRef] >= meta.LayerID {
		return false
	}
	return true
}

func (t *temporalState) temporalLayerBitrateKbps(layerID int) int {
	if uint(layerID) >= uint(t.pattern.Layers) {
		return 0
	}
	current := t.config.LayerTargetBitrateKbps[layerID]
	if layerID == 0 {
		return current
	}
	return current - t.config.LayerTargetBitrateKbps[layerID-1]
}

func (t *temporalState) temporalLayerFrameTargetBits(layerID int, timing timingState) int {
	if uint(layerID) >= uint(t.pattern.Layers) {
		return 0
	}
	layerBitrateBits := t.temporalLayerBitrateKbps(layerID) * 1000
	if layerBitrateBits <= 0 || min(min(timing.timebaseNum, timing.timebaseDen), timing.frameDuration) <= 0 {
		return 0
	}
	if layerID == 0 {
		return computeLayerBitsPerFrame(layerBitrateBits, timing, t.pattern.RateDecimator[0], 1)
	}
	prev := t.pattern.RateDecimator[layerID-1]
	current := t.pattern.RateDecimator[layerID]
	if prev <= current {
		return 0
	}
	num := int64(layerBitrateBits) * int64(timing.timebaseNum) * int64(timing.frameDuration) * int64(current) * int64(prev)
	den := int64(timing.timebaseDen) * int64(prev-current)
	return roundedInt(num, den)
}

type temporalReferenceRefresh struct {
	Last   bool
	Golden bool
	AltRef bool
}

func normalizeTemporalBitrates(cfg TemporalScalabilityConfig, layers int, totalBitrateKbps int) (TemporalScalabilityConfig, bool, error) {
	if layers <= 0 || layers > MaxTemporalLayers {
		return TemporalScalabilityConfig{}, false, ErrInvalidConfig
	}
	if totalBitrateKbps <= 0 {
		return TemporalScalabilityConfig{}, false, ErrInvalidBitrate
	}
	hasExplicitBitrate := false
	for i := range MaxTemporalLayers {
		if cfg.LayerTargetBitrateKbps[i] < 0 {
			return TemporalScalabilityConfig{}, false, ErrInvalidBitrate
		}
		if cfg.LayerTargetBitrateKbps[i] != 0 {
			hasExplicitBitrate = true
		}
	}
	if !hasExplicitBitrate {
		if !deriveTemporalBitrates(&cfg, layers, totalBitrateKbps) {
			return TemporalScalabilityConfig{}, false, ErrInvalidBitrate
		}
		return cfg, true, nil
	}
	for i := range layers {
		if cfg.LayerTargetBitrateKbps[i] <= 0 {
			return TemporalScalabilityConfig{}, false, ErrInvalidBitrate
		}
		if i > 0 && cfg.LayerTargetBitrateKbps[i] <= cfg.LayerTargetBitrateKbps[i-1] {
			return TemporalScalabilityConfig{}, false, ErrInvalidBitrate
		}
	}
	if cfg.LayerTargetBitrateKbps[layers-1] != totalBitrateKbps {
		return TemporalScalabilityConfig{}, false, ErrInvalidBitrate
	}
	for i := layers; i < MaxTemporalLayers; i++ {
		if cfg.LayerTargetBitrateKbps[i] != 0 {
			return TemporalScalabilityConfig{}, false, ErrInvalidBitrate
		}
	}
	return cfg, false, nil
}

func (t *temporalState) setLayerID(layerID int) error {
	layers := 1
	if t.enabled {
		layers = t.pattern.Layers
	}
	if uint(layerID) >= uint(layers) {
		return ErrInvalidConfig
	}
	if t.enabled {
		t.layerIDOverride = layerID
	}
	return nil
}

func deriveTemporalBitrates(cfg *TemporalScalabilityConfig, layers int, totalBitrateKbps int) bool {
	switch layers {
	case 1:
		cfg.LayerTargetBitrateKbps[0] = totalBitrateKbps
	case 2:
		cfg.LayerTargetBitrateKbps[0] = (60 * totalBitrateKbps) / 100
		cfg.LayerTargetBitrateKbps[1] = totalBitrateKbps
	case 3:
		cfg.LayerTargetBitrateKbps[0] = (40 * totalBitrateKbps) / 100
		cfg.LayerTargetBitrateKbps[1] = (60 * totalBitrateKbps) / 100
		cfg.LayerTargetBitrateKbps[2] = totalBitrateKbps
	default:
		return false
	}
	return true
}

func computeLayerBitsPerFrame(targetBandwidthBits int, timing timingState, frameInterval int, denominatorScale int) int {
	num := int64(targetBandwidthBits) * int64(timing.timebaseNum) * int64(timing.frameDuration) * int64(frameInterval)
	den := int64(timing.timebaseDen) * int64(denominatorScale)
	return roundedInt(num, den)
}

func roundedInt(num int64, den int64) int {
	if min(num, den) <= 0 {
		return 0
	}
	v := (num + den/2) / den
	if v > int64(maxInt()) {
		return 0
	}
	return int(v)
}

// Ported from libvpx v1.16.0 examples/vpx_temporal_svc_encoder.c
// set_temporal_layer_pattern().
func temporalLayeringPattern(mode TemporalLayeringMode) (temporalPattern, bool) {
	p := temporalPattern{}
	switch mode {
	case TemporalLayeringOneLayer:
		p.Layers = 1
		p.Periodicity = 1
		p.FlagPeriodicity = 1
		p.RateDecimator[0] = 1
		p.LayerID[0] = 0
		p.Flags[0] = EncodeForceKeyFrame | EncodeNoUpdateGolden | EncodeNoUpdateAltRef
	case TemporalLayeringTwoLayers:
		p.Layers = 2
		p.Periodicity = 2
		p.FlagPeriodicity = 2
		p.RateDecimator[0] = 2
		p.RateDecimator[1] = 1
		p.LayerID[0] = 0
		p.LayerID[1] = 1
		p.Flags[0] = EncodeForceKeyFrame | EncodeNoUpdateGolden | EncodeNoUpdateAltRef | EncodeNoReferenceGolden | EncodeNoReferenceAltRef
		p.Flags[1] = EncodeNoUpdateAltRef | EncodeNoUpdateLast | EncodeNoReferenceAltRef
	case TemporalLayeringTwoLayersThreeFrame:
		p.Layers = 2
		p.Periodicity = 3
		p.FlagPeriodicity = 3
		p.RateDecimator[0] = 3
		p.RateDecimator[1] = 1
		p.LayerID[0] = 0
		p.LayerID[1] = 1
		p.LayerID[2] = 1
		p.Flags[0] = EncodeForceKeyFrame | EncodeNoReferenceGolden | EncodeNoReferenceAltRef | EncodeNoUpdateGolden | EncodeNoUpdateAltRef
		p.Flags[1] = EncodeNoReferenceGolden | EncodeNoReferenceAltRef | EncodeNoUpdateAltRef | EncodeNoUpdateLast
		p.Flags[2] = p.Flags[1]
	case TemporalLayeringThreeLayersSixFrame:
		p.Layers = 3
		p.Periodicity = 6
		p.FlagPeriodicity = 6
		p.RateDecimator[0] = 6
		p.RateDecimator[1] = 3
		p.RateDecimator[2] = 1
		setTemporalLayerIDs(&p, 0, 2, 2, 1, 2, 2)
		p.Flags[0] = EncodeForceKeyFrame | EncodeNoReferenceGolden | EncodeNoReferenceAltRef | EncodeNoUpdateGolden | EncodeNoUpdateAltRef
		p.Flags[3] = EncodeNoReferenceAltRef | EncodeNoUpdateAltRef | EncodeNoUpdateLast
		for _, i := range [...]int{1, 2, 4, 5} {
			p.Flags[i] = EncodeNoUpdateGolden | EncodeNoUpdateLast
		}
	case TemporalLayeringThreeLayersNoInterLayerPrediction:
		p.Layers = 3
		p.Periodicity = 4
		p.FlagPeriodicity = 4
		p.RateDecimator[0] = 4
		p.RateDecimator[1] = 2
		p.RateDecimator[2] = 1
		setTemporalLayerIDs(&p, 0, 2, 1, 2)
		p.Flags[0] = EncodeForceKeyFrame | EncodeNoReferenceGolden | EncodeNoReferenceAltRef | EncodeNoUpdateGolden | EncodeNoUpdateAltRef
		p.Flags[2] = EncodeNoReferenceGolden | EncodeNoReferenceAltRef | EncodeNoUpdateAltRef | EncodeNoUpdateLast
		p.Flags[1] = EncodeNoReferenceAltRef | EncodeNoUpdateLast | EncodeNoUpdateGolden | EncodeNoUpdateAltRef
		p.Flags[3] = p.Flags[1]
	case TemporalLayeringThreeLayersLayerOnePrediction:
		p.Layers = 3
		p.Periodicity = 4
		p.FlagPeriodicity = 4
		p.RateDecimator[0] = 4
		p.RateDecimator[1] = 2
		p.RateDecimator[2] = 1
		setTemporalLayerIDs(&p, 0, 2, 1, 2)
		p.Flags[0] = EncodeForceKeyFrame | EncodeNoReferenceGolden | EncodeNoReferenceAltRef | EncodeNoUpdateGolden | EncodeNoUpdateAltRef
		p.Flags[2] = EncodeNoReferenceAltRef | EncodeNoUpdateLast | EncodeNoUpdateAltRef
		p.Flags[1] = EncodeNoReferenceAltRef | EncodeNoUpdateLast | EncodeNoUpdateGolden | EncodeNoUpdateAltRef
		p.Flags[3] = p.Flags[1]
	case TemporalLayeringThreeLayers:
		p.Layers = 3
		p.Periodicity = 4
		p.FlagPeriodicity = 4
		p.RateDecimator[0] = 4
		p.RateDecimator[1] = 2
		p.RateDecimator[2] = 1
		setTemporalLayerIDs(&p, 0, 2, 1, 2)
		p.Flags[0] = EncodeForceKeyFrame | EncodeNoReferenceGolden | EncodeNoReferenceAltRef | EncodeNoUpdateGolden | EncodeNoUpdateAltRef
		p.Flags[2] = EncodeNoReferenceAltRef | EncodeNoUpdateLast | EncodeNoUpdateAltRef
		p.Flags[1] = EncodeNoUpdateLast | EncodeNoUpdateGolden
		p.Flags[3] = p.Flags[1]
	case TemporalLayeringFiveLayers:
		p.Layers = 5
		p.Periodicity = 16
		p.FlagPeriodicity = 16
		p.RateDecimator[0] = 16
		p.RateDecimator[1] = 8
		p.RateDecimator[2] = 4
		p.RateDecimator[3] = 2
		p.RateDecimator[4] = 1
		setTemporalLayerIDs(&p, 0, 4, 3, 4, 2, 4, 3, 4, 1, 4, 3, 4, 2, 4, 3, 4)
		p.Flags[0] = EncodeForceKeyFrame
		for _, i := range [...]int{1, 3, 5, 7, 9, 11, 13, 15} {
			p.Flags[i] = EncodeNoUpdateLast | EncodeNoUpdateGolden | EncodeNoUpdateAltRef
		}
		for _, i := range [...]int{2, 6, 10, 14} {
			p.Flags[i] = EncodeNoUpdateAltRef | EncodeNoUpdateGolden
		}
		p.Flags[4] = EncodeNoReferenceLast | EncodeNoUpdateAltRef
		p.Flags[12] = p.Flags[4]
		p.Flags[8] = EncodeNoReferenceLast | EncodeNoReferenceGolden
	case TemporalLayeringTwoLayersWithSync:
		p.Layers = 2
		p.Periodicity = 2
		p.FlagPeriodicity = 8
		p.RateDecimator[0] = 2
		p.RateDecimator[1] = 1
		setTemporalLayerIDs(&p, 0, 1)
		p.Flags[0] = EncodeForceKeyFrame | EncodeNoReferenceGolden | EncodeNoUpdateAltRef
		p.Flags[1] = EncodeNoReferenceGolden | EncodeNoUpdateLast | EncodeNoUpdateAltRef
		p.Flags[2] = EncodeNoReferenceGolden | EncodeNoUpdateGolden | EncodeNoUpdateAltRef
		p.Flags[3] = EncodeNoUpdateAltRef | EncodeNoUpdateLast | EncodeNoUpdateEntropy
		p.Flags[4] = p.Flags[2]
		p.Flags[5] = p.Flags[3]
		p.Flags[6] = p.Flags[4]
		p.Flags[7] = p.Flags[5]
	case TemporalLayeringThreeLayersWithSync:
		p.Layers = 3
		p.Periodicity = 4
		p.FlagPeriodicity = 8
		p.RateDecimator[0] = 4
		p.RateDecimator[1] = 2
		p.RateDecimator[2] = 1
		setTemporalLayerIDs(&p, 0, 2, 1, 2)
		p.Flags[0] = EncodeForceKeyFrame | EncodeNoReferenceGolden | EncodeNoReferenceAltRef | EncodeNoUpdateGolden | EncodeNoUpdateAltRef
		p.Flags[1] = EncodeNoReferenceGolden | EncodeNoReferenceAltRef | EncodeNoUpdateLast | EncodeNoUpdateGolden
		p.Flags[2] = EncodeNoReferenceGolden | EncodeNoReferenceAltRef | EncodeNoUpdateLast | EncodeNoUpdateAltRef
		p.Flags[3] = EncodeNoUpdateLast | EncodeNoUpdateGolden
		p.Flags[5] = p.Flags[3]
		p.Flags[4] = EncodeNoReferenceGolden | EncodeNoReferenceAltRef | EncodeNoUpdateGolden | EncodeNoUpdateAltRef
		p.Flags[6] = EncodeNoReferenceAltRef | EncodeNoUpdateLast | EncodeNoUpdateAltRef
		p.Flags[7] = EncodeNoUpdateLast | EncodeNoUpdateGolden | EncodeNoUpdateAltRef | EncodeNoUpdateEntropy
	case TemporalLayeringThreeLayersAltRefWithSync:
		p.Layers = 3
		p.Periodicity = 4
		p.FlagPeriodicity = 8
		p.RateDecimator[0] = 4
		p.RateDecimator[1] = 2
		p.RateDecimator[2] = 1
		setTemporalLayerIDs(&p, 0, 2, 1, 2)
		p.Flags[0] = EncodeForceKeyFrame | EncodeNoUpdateAltRef | EncodeNoReferenceGolden
		p.Flags[1] = EncodeNoReferenceGolden | EncodeNoUpdateGolden | EncodeNoUpdateAltRef | EncodeNoUpdateLast | EncodeNoUpdateEntropy
		p.Flags[2] = EncodeNoReferenceGolden | EncodeNoUpdateAltRef | EncodeNoUpdateLast
		p.Flags[3] = EncodeNoUpdateGolden | EncodeNoUpdateAltRef | EncodeNoUpdateLast | EncodeNoUpdateEntropy
		p.Flags[4] = EncodeNoUpdateGolden | EncodeNoUpdateAltRef | EncodeNoReferenceGolden
		p.Flags[5] = p.Flags[3]
		p.Flags[6] = EncodeNoUpdateAltRef | EncodeNoUpdateLast
		p.Flags[7] = p.Flags[3]
	case TemporalLayeringThreeLayersOneReference:
		p.Layers = 3
		p.Periodicity = 4
		p.FlagPeriodicity = 4
		p.RateDecimator[0] = 4
		p.RateDecimator[1] = 2
		p.RateDecimator[2] = 1
		setTemporalLayerIDs(&p, 0, 2, 1, 2)
		p.Flags[0] = EncodeNoReferenceGolden | EncodeNoReferenceAltRef | EncodeNoUpdateGolden | EncodeNoUpdateAltRef
		p.Flags[2] = EncodeNoReferenceGolden | EncodeNoReferenceAltRef | EncodeNoUpdateAltRef | EncodeNoUpdateLast
		p.Flags[1] = EncodeNoReferenceGolden | EncodeNoReferenceAltRef | EncodeNoUpdateLast | EncodeNoUpdateGolden
		p.Flags[3] = EncodeNoReferenceLast | EncodeNoReferenceAltRef | EncodeNoUpdateLast | EncodeNoUpdateGolden
	case TemporalLayeringThreeLayersNoSync:
		p.Layers = 3
		p.Periodicity = 4
		p.FlagPeriodicity = 8
		p.RateDecimator[0] = 4
		p.RateDecimator[1] = 2
		p.RateDecimator[2] = 1
		setTemporalLayerIDs(&p, 0, 2, 1, 2)
		p.Flags[0] = EncodeNoUpdateGolden | EncodeNoUpdateAltRef | EncodeNoReferenceGolden
		p.Flags[4] = p.Flags[0]
		p.Flags[2] = EncodeNoUpdateAltRef | EncodeNoUpdateLast
		p.Flags[6] = p.Flags[2]
		p.Flags[1] = EncodeNoUpdateGolden | EncodeNoUpdateAltRef | EncodeNoUpdateLast | EncodeNoUpdateEntropy
		p.Flags[3] = p.Flags[1]
		p.Flags[5] = p.Flags[1]
		p.Flags[7] = p.Flags[1]
	default:
		return temporalPattern{}, false
	}
	return p, true
}

func setTemporalLayerIDs(p *temporalPattern, ids ...int) {
	copy(p.LayerID[:], ids)
}
