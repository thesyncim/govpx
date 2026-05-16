package govpx

import (
	"image"
	"sync"
)

// MaxMultiResLayers is the maximum number of resolutions supported by the
// VP9 multi-resolution encoder. It mirrors libvpx's
// MAX_NUM_ENCODERS in vp9_multi_resolution_encoder.c.
const MaxMultiResLayers = 5

// VP9MultiResolutionLayerOptions configures one resolution of a VP9
// multi-resolution encoder. Each layer becomes an independent internal
// VP9Encoder producing its own VP9 bitstream — the multi-resolution
// encoder does not synthesize a superframe and does not emit an
// access-unit. Use it for libvpx-style simulcast pipelines where each
// resolution is a separate VP9 stream consumed by a separate VP9
// decoder.
//
// Width and Height must be positive and obey VP9 dimension limits;
// validVP9Dimension constrains each axis. Higher-resolution layers
// occupy lower indices; layer index 0 is the highest resolution and
// the layer at LayerCount-1 is the lowest resolution. Per-layer
// resolutions must be strictly decreasing — both width and height
// must non-increase and at least one axis must strictly decrease
// between consecutive layers.
//
// TargetBitrateKbps, MinQuantizer, MaxQuantizer, CQLevel,
// CpuUsed, Deadline, Tuning, AQMode, Sharpness, NoiseSensitivity,
// StaticThreshold, ErrorResilient, ScreenContentMode, and RowMT
// are forwarded into the per-layer VP9Encoder.
type VP9MultiResolutionLayerOptions struct {
	// Width and Height are the coded dimensions for this layer.
	Width  int
	Height int

	// TargetBitrateKbps is the layer-specific target bitrate. It is
	// applied to the layer's internal VP9Encoder.
	TargetBitrateKbps int

	// MinQuantizer and MaxQuantizer bound the public 0..63 VP9
	// quantizer range for this layer. Zero leaves libvpx defaults.
	MinQuantizer int
	MaxQuantizer int
	// CQLevel sets the public libvpx VP9 0..63 constant-quality
	// level for this layer. Zero leaves libvpx defaults.
	CQLevel int

	// AQMode selects the VP9 adaptive-quantization mode for this
	// layer. Zero disables AQ.
	AQMode VP9AQMode

	// CpuUsed selects the layer-local libvpx VP9 cpu-used speed
	// preset. Zero leaves the multi-resolution default of realtime
	// cpu-used 8 (matching VP9Encoder's zero-value behavior).
	CpuUsed int8
	// Deadline selects the layer-local VP9 speed/quality operating
	// mode. Zero leaves the multi-resolution default.
	Deadline Deadline
	// Tuning selects the layer-local VP9 visual quality model.
	Tuning Tuning

	// ScreenContentMode selects layer-local VP9 content tuning.
	ScreenContentMode int8
	// NoiseSensitivity selects layer-local VP9 temporal denoising
	// strength.
	NoiseSensitivity int8
	// Sharpness is the layer-local VP9 loop-filter sharpness level.
	Sharpness uint8
	// StaticThreshold is the layer-local VP9 static-block breakout
	// threshold.
	StaticThreshold int

	// ErrorResilient sets the per-layer error_resilient bit.
	ErrorResilient bool

	// RowMT enables VP9 row-MT inside this layer when its Threads
	// budget allows. The multi-resolution encoder applies the
	// shared VP9MultiResolutionEncoderOptions.Threads count to each
	// layer's internal VP9Encoder.
	RowMT bool

	// MaxKeyframeInterval bounds the gap between key frames for
	// this layer.
	MaxKeyframeInterval int
	// MinKeyframeInterval is the VP9 kf_min_dist control for this
	// layer.
	MinKeyframeInterval int
}

// VP9MultiResolutionEncoderOptions configures a VP9 multi-resolution
// encoder. It mirrors libvpx's vp9_multi_resolution_encoder.c pattern:
// the encoder owns LayerCount independent VP9Encoder instances, one per
// resolution. Encode delivers each layer's bitstream into its own dst
// buffer (one per layer); decoders consume each layer separately.
//
// All layers share the FPS / TimebaseNum / TimebaseDen and operate at
// the same nominal frame rate, and all layers must use the same
// RateControlMode (mixing CBR and VBR across layers is rejected).
// Layer dimensions must be strictly decreasing from index 0 to
// LayerCount-1.
//
// This type is distinct from VP9SpatialSVCEncoder. Spatial SVC produces
// one superframe carrying every spatial layer with shared references;
// multi-resolution produces N independent bitstreams.
type VP9MultiResolutionEncoderOptions struct {
	// LayerCount is the number of resolutions. Valid values are in
	// [1, MaxMultiResLayers].
	LayerCount int
	// Layers holds per-resolution options. Indices >= LayerCount are
	// ignored. Layers[0] is the highest resolution; Layers[LayerCount-1]
	// is the lowest.
	Layers [MaxMultiResLayers]VP9MultiResolutionLayerOptions

	// FPS sets the shared frame rate. Zero leaves the per-layer
	// VP9Encoder default (30 fps).
	FPS int
	// TimebaseNum and TimebaseDen are the shared caller timebase.
	TimebaseNum int
	TimebaseDen int

	// Threads is the total tile-column hint applied to every
	// layer's internal VP9Encoder. The multi-resolution encoder
	// runs each per-layer encode on its own goroutine, so the
	// effective process-wide parallelism is roughly LayerCount
	// times this value when every layer's frame uses multi-column
	// tile bodies. Zero or 1 keep the serial single-tile path
	// inside each layer.
	Threads int

	// RateControlModeSet and RateControlMode select the shared
	// VP9 rate-control mode applied to every layer. When
	// RateControlModeSet is false, every layer keeps its
	// constant-quality default. Mixing CBR with other modes
	// across layers is unsupported and the encoder picks a single
	// mode for all layers.
	RateControlModeSet bool
	RateControlMode    RateControlMode

	// BufferSizeMs, BufferInitialSizeMs, and BufferOptimalSizeMs
	// configure the shared VP9 CBR virtual buffer. They apply to
	// every layer when RateControlMode is RateControlCBR.
	BufferSizeMs        int
	BufferInitialSizeMs int
	BufferOptimalSizeMs int
	// DropFrameAllowed enables VP9 CBR frame dropping on every
	// layer. Only meaningful when RateControlMode is
	// RateControlCBR.
	DropFrameAllowed bool

	// ShareMotionVectors is currently a placeholder. libvpx scales
	// motion vectors from the lowest-resolution encoder up to seed
	// higher-resolution motion search. govpx does not yet thread
	// per-SB MV hints through VP9Encoder; setting this field is
	// accepted but does not yet alter encoding. It is reserved
	// for a follow-up commit that wires the MV-hint slab.
	ShareMotionVectors bool
}

// VP9MultiResolutionEncoder is libvpx's
// vp9_multi_resolution_encoder pattern adapted for govpx. It owns
// LayerCount independent VP9Encoder instances and emits one VP9
// bitstream per resolution into a caller-provided per-layer dst
// buffer. Each per-layer encoder is invoked in parallel on a
// downscaled copy of the input frame.
type VP9MultiResolutionEncoder struct {
	closed             bool
	count              int
	layers             [MaxMultiResLayers]*VP9Encoder
	layerWidths        [MaxMultiResLayers]int
	layerHeights       [MaxMultiResLayers]int
	shareMotionVectors bool

	// scratch holds per-layer downscaled YCbCr images keyed by
	// layer index. The highest-resolution layer (index 0) is
	// encoded directly from the caller-owned source; the lower
	// resolutions hold downscaled copies. The slab is allocated
	// once at construction and reused on every Encode call so
	// the steady-state encode path does not allocate.
	scratch [MaxMultiResLayers]*image.YCbCr

	// resizeScratch is the shared int32 intermediate slab used by
	// the libvpx-aligned 8-tap polyphase downscale filter. The
	// horizontal pass writes 32-bit unrounded coefficients into
	// this slab and the vertical pass reads them out. Sized once
	// at construction to cover the largest (dstWidth × srcHeight)
	// luma plane any layer needs, so the encode-time path stays
	// allocation-free.
	resizeScratch []int32

	// results and resultErrs back parallel per-layer encode
	// goroutines. They are preallocated once to keep the encode
	// path allocation-free.
	results    [MaxMultiResLayers]VP9EncodeResult
	resultErrs [MaxMultiResLayers]error
}

// NewVP9MultiResolutionEncoder constructs a VP9 multi-resolution
// encoder with one internal VP9Encoder per resolution.
func NewVP9MultiResolutionEncoder(opts VP9MultiResolutionEncoderOptions) (*VP9MultiResolutionEncoder, error) {
	if opts.LayerCount < 1 || opts.LayerCount > MaxMultiResLayers {
		return nil, ErrInvalidConfig
	}
	if opts.Threads < 0 {
		return nil, ErrInvalidConfig
	}
	if opts.FPS < 0 || opts.TimebaseNum < 0 || opts.TimebaseDen < 0 {
		return nil, ErrInvalidConfig
	}
	count := opts.LayerCount
	for i := 0; i < count; i++ {
		layer := opts.Layers[i]
		if !validVP9Dimension(layer.Width) || !validVP9Dimension(layer.Height) {
			return nil, ErrInvalidConfig
		}
		if i > 0 {
			prev := opts.Layers[i-1]
			// Both axes must non-increase and at least one must
			// strictly decrease between consecutive layers.
			if layer.Width > prev.Width || layer.Height > prev.Height {
				return nil, ErrInvalidConfig
			}
			if layer.Width == prev.Width && layer.Height == prev.Height {
				return nil, ErrInvalidConfig
			}
		}
	}

	mre := &VP9MultiResolutionEncoder{
		count:              count,
		shareMotionVectors: opts.ShareMotionVectors,
	}
	for i := 0; i < count; i++ {
		layer := opts.Layers[i]
		mre.layerWidths[i] = layer.Width
		mre.layerHeights[i] = layer.Height
		encOpts := vp9MultiResolutionLayerEncoderOptions(opts, layer)
		enc, err := NewVP9Encoder(encOpts)
		if err != nil {
			_ = mre.Close()
			return nil, err
		}
		mre.layers[i] = enc
		// Pre-allocate downscaled-source scratch for every layer
		// except the highest-resolution one, which encodes the
		// caller-owned source directly.
		if i > 0 {
			mre.scratch[i] = image.NewYCbCr(
				image.Rect(0, 0, layer.Width, layer.Height),
				image.YCbCrSubsampleRatio420)
		}
	}
	// Size the resize scratch once: the polyphase filter writes a
	// dstWidth × srcHeight intermediate. srcHeight is bounded above
	// by the highest-resolution layer's height; dstWidth is bounded
	// by the same layer's width since lower-resolution layers
	// strictly shrink. The slab thus only needs to cover the
	// largest possible (dstWidth, srcHeight) pair, which is
	// (layers[1].Width, layers[0].Height) when count > 1. The
	// single-layer case never downscales, so the slab stays empty.
	if count > 1 {
		// layers[0] is highest resolution, so srcHeight is
		// layers[0].Height. The largest dstWidth across all lower
		// layers is layers[1].Width.
		srcHeight := mre.layerHeights[0]
		dstWidth := mre.layerWidths[1]
		mre.resizeScratch = make([]int32,
			vp9MultiResolutionPolyphaseScratchSize(dstWidth, srcHeight))
	}
	return mre, nil
}

// vp9MultiResolutionLayerEncoderOptions builds the per-layer
// VP9EncoderOptions consumed by NewVP9Encoder. Shared fields come from
// opts; per-layer overrides come from layer.
func vp9MultiResolutionLayerEncoderOptions(opts VP9MultiResolutionEncoderOptions,
	layer VP9MultiResolutionLayerOptions,
) VP9EncoderOptions {
	enc := VP9EncoderOptions{
		Width:               layer.Width,
		Height:              layer.Height,
		FPS:                 opts.FPS,
		TimebaseNum:         opts.TimebaseNum,
		TimebaseDen:         opts.TimebaseDen,
		Threads:             opts.Threads,
		RowMT:               layer.RowMT,
		TargetBitrateKbps:   layer.TargetBitrateKbps,
		MinQuantizer:        layer.MinQuantizer,
		MaxQuantizer:        layer.MaxQuantizer,
		CQLevel:             layer.CQLevel,
		AQMode:              layer.AQMode,
		CpuUsed:             layer.CpuUsed,
		Deadline:            layer.Deadline,
		Tuning:              layer.Tuning,
		ScreenContentMode:   layer.ScreenContentMode,
		NoiseSensitivity:    layer.NoiseSensitivity,
		Sharpness:           layer.Sharpness,
		StaticThreshold:     layer.StaticThreshold,
		ErrorResilient:      layer.ErrorResilient,
		MaxKeyframeInterval: layer.MaxKeyframeInterval,
		MinKeyframeInterval: layer.MinKeyframeInterval,
	}
	if opts.RateControlModeSet {
		enc.RateControlModeSet = true
		enc.RateControlMode = opts.RateControlMode
		enc.BufferSizeMs = opts.BufferSizeMs
		enc.BufferInitialSizeMs = opts.BufferInitialSizeMs
		enc.BufferOptimalSizeMs = opts.BufferOptimalSizeMs
		if opts.RateControlMode == RateControlCBR {
			enc.DropFrameAllowed = opts.DropFrameAllowed
		}
	}
	return enc
}

// LayerCount returns the number of configured resolutions.
func (e *VP9MultiResolutionEncoder) LayerCount() int {
	if e == nil {
		return 0
	}
	return e.count
}

// LayerEncoder returns the internal encoder for layer index i so the
// caller can apply VP9 runtime controls (rate-control, deadline,
// realtime target) to one resolution. Do not close the returned
// encoder; Close on the multi-resolution encoder releases every
// layer.
func (e *VP9MultiResolutionEncoder) LayerEncoder(i int) (*VP9Encoder, error) {
	if e == nil || e.closed {
		return nil, ErrClosed
	}
	if i < 0 || i >= e.count {
		return nil, ErrInvalidConfig
	}
	return e.layers[i], nil
}

// ForceKeyFrame requests that the next encode emit a key frame on
// every layer atomically. libvpx's multi-resolution encoder forces
// keyframes across every resolution together so a simulcast pipeline
// can recover all streams with one cue.
func (e *VP9MultiResolutionEncoder) ForceKeyFrame() {
	if e == nil || e.closed {
		return
	}
	for i := 0; i < e.count; i++ {
		e.layers[i].ForceKeyFrame()
	}
}

// IsKeyFrameNext reports whether any layer is due to emit a key
// frame next.
func (e *VP9MultiResolutionEncoder) IsKeyFrameNext() bool {
	if e == nil || e.closed {
		return false
	}
	for i := 0; i < e.count; i++ {
		if e.layers[i].IsKeyFrameNext() {
			return true
		}
	}
	return false
}

// EncodeIntoWithFlagsResult encodes the next source frame into one
// VP9 bitstream per resolution. dsts must contain exactly LayerCount
// caller-owned per-layer output buffers; result[i].Data aliases
// dsts[i]. The flags argument is applied uniformly to every layer
// (mirroring libvpx's multi-resolution encoder, which forces flags
// across every resolution).
//
// The highest-resolution layer encodes the caller-owned source
// directly. Lower-resolution layers encode a downscaled copy
// produced by an integer-arithmetic bilinear filter. The downscaled
// pyramid is allocated once at construction and reused; the encode
// path does not allocate on the steady state.
func (e *VP9MultiResolutionEncoder) EncodeIntoWithFlagsResult(img *image.YCbCr,
	dsts [][]byte, flags EncodeFlags,
) ([]VP9EncodeResult, error) {
	if e == nil || e.closed {
		return nil, ErrClosed
	}
	if img == nil {
		return nil, ErrInvalidConfig
	}
	if len(dsts) != e.count {
		return nil, ErrInvalidConfig
	}
	// Validate the highest-resolution source dimensions. Lower
	// layers consume scratch images sized at construction.
	if err := e.layers[0].validateVP9EncoderSource(img); err != nil {
		return nil, err
	}
	for i := 0; i < e.count; i++ {
		if len(dsts[i]) < vp9MinEncodeIntoBuffer {
			return nil, ErrBufferTooSmall
		}
	}
	// Prepare per-layer source images: layer 0 reuses the caller
	// frame, lower layers downscale into pre-allocated scratch
	// through the libvpx-aligned 8-tap polyphase filter. The
	// downscale loop is sequential because the intermediate
	// scratch slab is shared across layers; the per-layer encode
	// goroutines below get an already-downscaled YCbCr.
	for i := 1; i < e.count; i++ {
		vp9MultiResolutionDownscaleI420(e.scratch[i], img,
			e.layerWidths[i], e.layerHeights[i],
			e.resizeScratch)
	}
	// Reset per-call result storage.
	for i := 0; i < e.count; i++ {
		e.results[i] = VP9EncodeResult{}
		e.resultErrs[i] = nil
	}
	// Each per-layer encoder runs on its own goroutine. The
	// goroutines are spawned in parallel; the calling goroutine
	// participates as the worker for layer 0 so a single-layer
	// configuration does not pay the cost of launching a goroutine.
	var wg sync.WaitGroup
	for i := 1; i < e.count; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			res, err := e.layers[idx].EncodeIntoWithFlagsResult(
				e.scratch[idx], dsts[idx], flags)
			e.results[idx] = res
			e.resultErrs[idx] = err
		}(i)
	}
	// Layer 0 (highest resolution) encodes inline.
	res0, err0 := e.layers[0].EncodeIntoWithFlagsResult(img, dsts[0], flags)
	e.results[0] = res0
	e.resultErrs[0] = err0
	wg.Wait()
	// Collect: return the first non-nil error so the caller can
	// distinguish encoder failure from a normal drop.
	for i := 0; i < e.count; i++ {
		if e.resultErrs[i] != nil {
			return nil, e.resultErrs[i]
		}
	}
	out := make([]VP9EncodeResult, e.count)
	copy(out, e.results[:e.count])
	return out, nil
}

// EncodeIntoWithResult encodes the next frame with no caller flags.
func (e *VP9MultiResolutionEncoder) EncodeIntoWithResult(img *image.YCbCr,
	dsts [][]byte,
) ([]VP9EncodeResult, error) {
	return e.EncodeIntoWithFlagsResult(img, dsts, 0)
}

// FlushIntoWithResult drains one queued lookahead frame per layer
// (when any layer was configured with LookaheadFrames > 0). It
// returns ErrFrameNotReady when no layer has a queued frame.
func (e *VP9MultiResolutionEncoder) FlushIntoWithResult(dsts [][]byte) ([]VP9EncodeResult, error) {
	if e == nil || e.closed {
		return nil, ErrClosed
	}
	if len(dsts) != e.count {
		return nil, ErrInvalidConfig
	}
	for i := 0; i < e.count; i++ {
		if len(dsts[i]) < vp9MinEncodeIntoBuffer {
			return nil, ErrBufferTooSmall
		}
	}
	for i := 0; i < e.count; i++ {
		e.results[i] = VP9EncodeResult{}
		e.resultErrs[i] = nil
	}
	var wg sync.WaitGroup
	for i := 1; i < e.count; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			res, err := e.layers[idx].FlushIntoWithResult(dsts[idx])
			e.results[idx] = res
			e.resultErrs[idx] = err
		}(i)
	}
	res0, err0 := e.layers[0].FlushIntoWithResult(dsts[0])
	e.results[0] = res0
	e.resultErrs[0] = err0
	wg.Wait()
	for i := 0; i < e.count; i++ {
		if e.resultErrs[i] != nil {
			return nil, e.resultErrs[i]
		}
	}
	out := make([]VP9EncodeResult, e.count)
	copy(out, e.results[:e.count])
	return out, nil
}

// Close releases every per-layer encoder. Subsequent encode calls
// return ErrClosed.
func (e *VP9MultiResolutionEncoder) Close() error {
	if e == nil {
		return ErrClosed
	}
	if e.closed {
		return nil
	}
	for i := 0; i < e.count; i++ {
		if e.layers[i] != nil {
			_ = e.layers[i].Close()
		}
	}
	e.closed = true
	return nil
}

// Codec reports the codec this encoder targets.
func (e *VP9MultiResolutionEncoder) Codec() Codec { return CodecVP9 }

// vp9MultiResolutionDownscaleI420 downscales src into dst at the
// destination's declared visible width/height using the libvpx-
// aligned 8-tap 16-phase polyphase resampler in vp9_resize.go. The
// scratch slab carries the horizontal-pass intermediate so the
// vertical pass can read 32-bit unrounded coefficients and apply the
// combined two-pass rounding shift in one place. The caller must
// supply scratch with at least vp9MultiResolutionPolyphaseScratchSize
// (dstWidth, srcHeight) int32 entries; the multi-resolution encoder
// allocates one slab once at construction.
//
// Parity note: the previous implementation used a 2-tap bilinear
// filter which over-smoothed the downscaled image. The 8-tap
// polyphase filter mirrors libvpx vp9_resize.c's interpolation
// kernel and preserves significantly more high-frequency detail.
func vp9MultiResolutionDownscaleI420(dst *image.YCbCr, src *image.YCbCr,
	dstWidth, dstHeight int, scratch []int32,
) {
	vp9MultiResolutionPolyphaseDownscaleI420(dst, src, dstWidth, dstHeight, scratch)
}

