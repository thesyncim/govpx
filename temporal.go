package govpx

const (
	MaxTemporalLayers     = 5
	maxTemporalPeriodSize = 16
)

type TemporalLayeringMode int

const (
	TemporalLayeringOneLayer TemporalLayeringMode = iota
	TemporalLayeringTwoLayers
	TemporalLayeringTwoLayersThreeFrame
	TemporalLayeringThreeLayersSixFrame
	TemporalLayeringThreeLayersNoInterLayerPrediction
	TemporalLayeringThreeLayersLayerOnePrediction
	TemporalLayeringThreeLayers
	TemporalLayeringFiveLayers
	TemporalLayeringTwoLayersWithSync
	TemporalLayeringThreeLayersWithSync
	TemporalLayeringThreeLayersAltRefWithSync
	TemporalLayeringThreeLayersOneReference
	TemporalLayeringThreeLayersNoSync
)

type TemporalScalabilityConfig struct {
	Enabled bool
	Mode    TemporalLayeringMode

	// LayerTargetBitrateKbps is cumulative by temporal layer, matching
	// libvpx's ts_target_bitrate[] contract.
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
	if meta.LayerID < 0 || meta.LayerID >= t.pattern.Layers {
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
	if meta.LayerID < 0 || meta.LayerID >= t.pattern.Layers {
		return
	}
	t.accounting[meta.LayerID].InputFrames++
}

func (t *temporalState) accountDroppedFrameBuffer(meta temporalFrame) {
	if meta.LayerID < 0 || meta.LayerID >= t.pattern.Layers {
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
	accounting.BufferLevelBits = saturatingSub(accounting.BufferLevelBits, encodedBits)
	if accounting.BufferLevelBits > accounting.MaximumBufferBits {
		accounting.BufferLevelBits = accounting.MaximumBufferBits
	}
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
	if layerID < 0 || layerID >= t.pattern.Layers {
		return 0
	}
	current := t.config.LayerTargetBitrateKbps[layerID]
	if layerID == 0 {
		return current
	}
	return current - t.config.LayerTargetBitrateKbps[layerID-1]
}

func (t *temporalState) temporalLayerFrameTargetBits(layerID int, timing timingState) int {
	if layerID < 0 || layerID >= t.pattern.Layers {
		return 0
	}
	layerBitrateBits := t.temporalLayerBitrateKbps(layerID) * 1000
	if layerBitrateBits <= 0 || timing.timebaseNum <= 0 || timing.timebaseDen <= 0 || timing.frameDuration <= 0 {
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
	for i := 0; i < MaxTemporalLayers; i++ {
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
	for i := 0; i < layers; i++ {
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
	if layerID < 0 || layerID >= layers {
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
	if num <= 0 || den <= 0 {
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
	for i, id := range ids {
		p.LayerID[i] = id
	}
}
