package govpx

import (
	"image"

	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

func (e *VP9Encoder) validateVP9EncoderSource(img *image.YCbCr) error {
	if img == nil {
		return ErrInvalidConfig
	}
	if img.Rect.Dx() != e.opts.Width || img.Rect.Dy() != e.opts.Height {
		return ErrInvalidConfig
	}
	if img.SubsampleRatio != image.YCbCrSubsampleRatio420 {
		return ErrInvalidConfig
	}
	if img.YStride < e.opts.Width || img.CStride < (e.opts.Width+1)/2 {
		return ErrInvalidConfig
	}
	if len(img.Y) < buffers.PlaneLen(img.YStride, e.opts.Height, e.opts.Width) {
		return ErrInvalidConfig
	}
	uvWidth, uvHeight := buffers.Chroma420Dimensions(e.opts.Width, e.opts.Height)
	if len(img.Cb) < buffers.PlaneLen(img.CStride, uvHeight, uvWidth) ||
		len(img.Cr) < buffers.PlaneLen(img.CStride, uvHeight, uvWidth) {
		return ErrInvalidConfig
	}
	return nil
}

// LastQuantizer mirrors libvpx's VP9E_GET_LAST_QUANTIZER /
// VP9E_GET_LAST_QUANTIZER_64 controls. It returns the public 0..63
// quantizer and the internal VP9 qindex of the most recently committed
// encoded frame. ok is false on a nil or closed encoder, and before any
// frame has been committed (dropped frames and buffered-by-lookahead
// inputs leave the value untouched).
func (e *VP9Encoder) LastQuantizer() (public int, internal int, ok bool) {
	if e == nil || e.closed || !e.lastQuantizerValid {
		return 0, 0, false
	}
	return e.lastQuantizerPublic, e.lastQuantizerInternal, true
}

// LastLoopFilterLevel mirrors libvpx's VP9E_GET_LOOPFILTER_LEVEL control. It
// returns the final loop-filter level selected for the most recently committed
// encoded frame. ok is false on a nil or closed encoder, and before any frame
// has been committed; dropped or buffered-by-lookahead inputs leave the value
// untouched.
func (e *VP9Encoder) LastLoopFilterLevel() (level uint8, ok bool) {
	if e == nil || e.closed || !e.lastLoopFilterValid {
		return 0, false
	}
	return e.lastLoopFilterLevel, true
}

// IsKeyFrameNext reports whether the next call to EncodeInto would
// emit a key frame. The first frame is always a key; subsequent
// frames key on MaxKeyframeInterval boundaries.
func (e *VP9Encoder) IsKeyFrameNext() bool {
	if e == nil || e.closed {
		return false
	}
	if e.frameIndex == 0 || e.forceKeyFrame {
		return true
	}
	cadence := e.opts.MaxKeyframeInterval
	if cadence <= 0 {
		cadence = 128 // libvpx default kf_max_dist
	}
	return e.frameIndex%cadence == 0
}

func validateVP9KeyFrameIntervalOptions(minFrames, maxFrames int) error {
	if minFrames < 0 || maxFrames < 0 {
		return ErrInvalidConfig
	}
	max := maxFrames
	if max <= 0 {
		max = 128
	}
	if minFrames > max {
		return ErrInvalidConfig
	}
	return nil
}

// ForceKeyFrame requests that the next successfully committed VP9 packet be
// a key frame. Calls on a nil or closed encoder are no-ops.
func (e *VP9Encoder) ForceKeyFrame() {
	if e == nil || e.closed {
		return
	}
	e.forceKeyFrame = true
}

// EncodeInto packs the next profile 0 frame into dst. It is equivalent to
// EncodeIntoWithFlags with no flags.
//
// Returns the number of bytes written into dst. Caller sizes dst; leave room
// for up to 64 KiB to match libvpx's first-partition header bound. When VP9
// CBR rate control drops a frame this returns 0, nil; use
// EncodeIntoWithResult to distinguish a dropped frame from other empty output.
func (e *VP9Encoder) EncodeInto(img *image.YCbCr, dst []byte) (int, error) {
	result, err := e.EncodeIntoWithFlagsResult(img, dst, 0)
	return len(result.Data), err
}

// EncodeIntoWithFlags packs the next profile 0 frame into dst while applying
// the VP9-compatible subset of EncodeFlags: EncodeForceKeyFrame,
// EncodeInvisibleFrame,
// EncodeNoReference{Last,Golden,AltRef}, EncodeNoUpdate{Last,Golden,AltRef},
// EncodeNoUpdateEntropy, EncodeForceGoldenFrame, and EncodeForceAltRefFrame.
//
// The current packet path emits source-backed keyframes and visible
// single-reference LAST / GOLDEN / ALTREF inter frames with DCT_DCT residual
// transforms up to Tx32x32, including bounded rate-aware motion search and
// transform-size selection with quarter-pel refinement. A deterministic prepass
// walks the same tiled mode tree to collect frame counts before the compressed
// header, so the real tile stream is encoded with same-frame counts-driven
// probability updates.
func (e *VP9Encoder) EncodeIntoWithFlags(img *image.YCbCr, dst []byte, flags EncodeFlags) (int, error) {
	result, err := e.EncodeIntoWithFlagsResult(img, dst, flags)
	return len(result.Data), err
}

// EncodeIntoWithResult packs the next profile 0 frame into dst and returns
// packet metadata. It is equivalent to EncodeIntoWithFlagsResult with no
// caller flags.
func (e *VP9Encoder) EncodeIntoWithResult(img *image.YCbCr, dst []byte) (VP9EncodeResult, error) {
	return e.EncodeIntoWithFlagsResult(img, dst, 0)
}

// EncodeIntoWithFlagsResult packs the next profile 0 frame into dst while
// returning packet and temporal-layer metadata.
func (e *VP9Encoder) EncodeIntoWithFlagsResult(img *image.YCbCr, dst []byte, flags EncodeFlags) (VP9EncodeResult, error) {
	if e == nil || e.closed {
		return VP9EncodeResult{}, ErrClosed
	}
	if e.vp9LookaheadEnabled() {
		return e.encodeVP9LookaheadIntoWithFlagsResult(img, dst, flags)
	}
	callerFlags := flags
	temporalFrame := e.temporal.nextFrame(e.vp9TimingState())
	flags |= temporalFrame.Flags
	flags = normalizeVP9EncodeFlags(flags)
	if e.vp9ShouldEncodeKeyFrame(flags) {
		flags &^= (temporalFrame.Flags & vp9NoUpdateRefFlags) &^ callerFlags
	}
	return e.encodeVP9FrameIntoWithFlagsResult(img, dst, flags, false, temporalFrame, false)
}

func (e *VP9Encoder) encodeVP9InterLayerIntoWithFlagsResult(img *image.YCbCr, dst []byte, flags EncodeFlags) (VP9EncodeResult, error) {
	callerFlags := flags
	temporalFrame := e.temporal.nextFrame(e.vp9TimingState())
	temporalFlags := temporalFrame.Flags
	useInterLayerReference := !e.forceKeyFrame &&
		callerFlags&EncodeForceKeyFrame == 0 &&
		e.hasVP9UsableInterReference(flags|temporalFlags)
	if useInterLayerReference {
		if callerUpdateFlags := callerFlags & vp9NoUpdateRefFlags; callerUpdateFlags != 0 {
			flags |= callerUpdateFlags
		} else {
			flags |= EncodeNoUpdateLast | EncodeNoUpdateAltRef
		}
		if temporalFrame.Enabled && temporalFrame.LayerID > 0 {
			flags |= EncodeNoUpdateGolden
		}
		temporalFlags &^= vp9NoUpdateFlagForRefSlot(
			vp9SpatialSVCLayerReferenceSlot(e.opts.SpatialScalability.LayerID))
	}
	if e.frameIndex == 0 && !e.forceKeyFrame &&
		callerFlags&EncodeForceKeyFrame == 0 {
		temporalFlags &^= EncodeForceKeyFrame
	}
	flags |= temporalFlags
	flags = normalizeVP9EncodeFlags(flags)
	if !useInterLayerReference && e.vp9ShouldEncodeKeyFrame(flags) {
		flags &^= (temporalFrame.Flags & vp9NoUpdateRefFlags) &^ callerFlags
	}
	return e.encodeVP9FrameIntoWithFlagsResultInternal(img, dst, flags, false,
		temporalFrame, true, false)
}

func (e *VP9Encoder) encodeVP9SpatialSVCBaseIntoWithFlagsResult(img *image.YCbCr, dst []byte, flags EncodeFlags) (VP9EncodeResult, error) {
	callerFlags := flags
	temporalFrame := e.temporal.nextFrame(e.vp9TimingState())
	temporalFlags := temporalFrame.Flags
	if temporalFrame.Enabled && temporalFrame.LayerID > 0 &&
		!e.forceKeyFrame && callerFlags&EncodeForceKeyFrame == 0 {
		temporalFlags &^= EncodeNoUpdateAltRef
		temporalFlags |= EncodeNoUpdateGolden
	}
	flags |= temporalFlags
	flags = normalizeVP9EncodeFlags(flags)
	if e.vp9ShouldEncodeKeyFrame(flags) {
		flags &^= (temporalFrame.Flags & vp9NoUpdateRefFlags) &^ callerFlags
	}
	return e.encodeVP9FrameIntoWithFlagsResultInternal(img, dst, flags, false,
		temporalFrame, false, false)
}

// EncodeIntraOnlyFrameInto packs a hidden VP9 intra-only frame into dst.
// Intra-only frames are non-key VP9 packets with sync code and frame size but
// no inter prediction; by VP9 syntax they are always invisible. The VP9 stream
// must already be initialized by a coded frame. Use EncodeShowExistingFrameInto
// to display a refreshed slot after this call.
func (e *VP9Encoder) EncodeIntraOnlyFrameInto(img *image.YCbCr, dst []byte, flags EncodeFlags) (int, error) {
	result, err := e.encodeVP9FrameIntoWithFlagsResult(img, dst, flags, true, temporalFrame{LayerID: 0, LayerCount: 1}, false)
	return len(result.Data), err
}

func (e *VP9Encoder) encodeVP9FrameIntoWithFlagsResult(img *image.YCbCr, dst []byte, flags EncodeFlags, forceIntraOnly bool, temporalFrame temporalFrame, isSrcFrameAltRef bool) (result VP9EncodeResult, err error) {
	return e.encodeVP9FrameIntoWithFlagsResultInternal(img, dst, flags,
		forceIntraOnly, temporalFrame, false, isSrcFrameAltRef)
}
