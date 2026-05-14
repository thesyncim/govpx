package govpx

type vp9RateControlState struct {
	enabled bool
	mode    RateControlMode

	targetBitrateKbps   int
	targetBandwidthBits int
	bitsPerFrame        int
	frameTargetBits     int

	bufferSizeMs        int
	bufferInitialSizeMs int
	bufferOptimalSizeMs int
	bufferSizeBits      int
	bufferInitialBits   int
	bufferOptimalBits   int
	bufferLevelBits     int

	dropFrameAllowed    bool
	dropFramesWaterMark uint8
}

func validateVP9RateControlOptions(opts VP9EncoderOptions) error {
	if opts.BufferSizeMs < 0 || opts.BufferInitialSizeMs < 0 ||
		opts.BufferOptimalSizeMs < 0 || opts.DropFrameWaterMark < 0 {
		return ErrInvalidConfig
	}
	if !opts.RateControlModeSet {
		if opts.BufferSizeMs != 0 || opts.BufferInitialSizeMs != 0 ||
			opts.BufferOptimalSizeMs != 0 || opts.DropFrameAllowed ||
			opts.DropFrameWaterMark != 0 {
			return ErrInvalidConfig
		}
		return nil
	}
	if opts.RateControlMode != RateControlCBR {
		return ErrInvalidConfig
	}
	if opts.TargetBitrateKbps <= 0 {
		return ErrInvalidBitrate
	}
	return nil
}

func (rc *vp9RateControlState) applyOptions(opts VP9EncoderOptions, timing timingState) error {
	*rc = vp9RateControlState{}
	if !opts.RateControlModeSet {
		return nil
	}
	if opts.RateControlMode != RateControlCBR {
		return ErrInvalidConfig
	}
	bufferSize := opts.BufferSizeMs
	if bufferSize == 0 {
		bufferSize = libvpxDefaultBufferSizeMs
	}
	bufferInitial := opts.BufferInitialSizeMs
	if bufferInitial == 0 {
		bufferInitial = libvpxDefaultBufferInitialMs
	}
	bufferOptimal := opts.BufferOptimalSizeMs
	if bufferOptimal == 0 {
		bufferOptimal = libvpxDefaultBufferOptimalMs
	}
	if bufferSize <= 0 || bufferInitial < 0 || bufferOptimal < 0 {
		return ErrInvalidConfig
	}
	waterMark := opts.DropFrameWaterMark
	if opts.DropFrameAllowed && waterMark <= 0 {
		waterMark = defaultDropFramesWaterMark
	}
	if waterMark > 100 {
		waterMark = 100
	}

	rc.enabled = true
	rc.mode = opts.RateControlMode
	rc.bufferSizeMs = bufferSize
	rc.bufferInitialSizeMs = bufferInitial
	rc.bufferOptimalSizeMs = bufferOptimal
	rc.dropFrameAllowed = opts.DropFrameAllowed
	rc.dropFramesWaterMark = uint8(waterMark)
	if err := rc.setBitrateKbps(opts.TargetBitrateKbps, timing); err != nil {
		return err
	}
	rc.bufferLevelBits = rc.bufferInitialBits
	return nil
}

func (rc *vp9RateControlState) setBitrateKbps(kbps int, timing timingState) error {
	if !rc.enabled {
		return nil
	}
	if kbps <= 0 {
		return ErrInvalidBitrate
	}
	targetBits, ok := checkedMul(kbps, 1000)
	if !ok {
		return ErrInvalidBitrate
	}
	bitsPerFrame := computeBitsPerFrame(targetBits, timing)
	if bitsPerFrame <= 0 {
		return ErrInvalidBitrate
	}
	bufferSizeBits, ok := checkedMul(kbps, rc.bufferSizeMs)
	if !ok {
		return ErrInvalidBitrate
	}
	bufferInitialBits, ok := checkedMul(kbps, rc.bufferInitialSizeMs)
	if !ok {
		return ErrInvalidBitrate
	}
	bufferOptimalBits, ok := checkedMul(kbps, rc.bufferOptimalSizeMs)
	if !ok {
		return ErrInvalidBitrate
	}
	rc.targetBitrateKbps = kbps
	rc.targetBandwidthBits = targetBits
	rc.bitsPerFrame = bitsPerFrame
	rc.frameTargetBits = bitsPerFrame
	rc.bufferSizeBits = bufferSizeBits
	rc.bufferInitialBits = bufferInitialBits
	rc.bufferOptimalBits = bufferOptimalBits
	rc.clampBuffer()
	return nil
}

func (rc *vp9RateControlState) beginFrame() {
	if !rc.enabled {
		return
	}
	rc.frameTargetBits = rc.bitsPerFrame
}

func (rc *vp9RateControlState) shouldDropInterFrame() bool {
	return rc.enabled && rc.mode == RateControlCBR &&
		rc.dropFrameAllowed && rc.bufferLevelBits < 0
}

func (rc *vp9RateControlState) postDropFrame() {
	if !rc.enabled {
		return
	}
	rc.bufferLevelBits = saturatingAdd(rc.bufferLevelBits, rc.bitsPerFrame)
	rc.clampBuffer()
}

func (rc *vp9RateControlState) postEncodeFrame(sizeBytes int, showFrame bool) {
	if !rc.enabled {
		return
	}
	rc.bufferLevelBits = vp9PostEncodeBufferLevel(
		rc.bufferLevelBits, rc.bitsPerFrame, rc.bufferSizeBits,
		encodedSizeBits(sizeBytes), showFrame)
}

func (rc *vp9RateControlState) setFrameDropAllowed(enabled bool, waterMark int) {
	if !rc.enabled {
		return
	}
	rc.dropFrameAllowed = enabled
	if !enabled {
		rc.dropFramesWaterMark = 0
		return
	}
	if waterMark <= 0 {
		waterMark = int(rc.dropFramesWaterMark)
	}
	if waterMark <= 0 {
		waterMark = defaultDropFramesWaterMark
	}
	if waterMark > 100 {
		waterMark = 100
	}
	rc.dropFramesWaterMark = uint8(waterMark)
}

func (rc *vp9RateControlState) clampBuffer() {
	if !rc.enabled {
		return
	}
	if rc.bufferLevelBits > rc.bufferSizeBits {
		rc.bufferLevelBits = rc.bufferSizeBits
	}
}

func vp9PostEncodeBufferLevel(level, bitsPerFrame, maxBits, encodedBits int, showFrame bool) int {
	next := int64(level)
	if showFrame {
		next += int64(bitsPerFrame)
	}
	next -= int64(encodedBits)
	if next > int64(maxBits) {
		return maxBits
	}
	if next < int64(-maxInt()) {
		return -maxInt()
	}
	return int(next)
}
