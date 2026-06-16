package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
	"github.com/thesyncim/govpx/internal/vpx/geometry"
)

// SetPreviewPostProcess changes the encoder preview postprocessing flags at
// runtime. This mirrors libvpx's encoder-side VP8_SET_POSTPROC control, which
// configures images returned by vpx_codec_get_preview_frame rather than
// changing encoded VP8 packets.
func (e *VP8Encoder) SetPreviewPostProcess(flags PostProcessFlag, noiseLevel int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	return e.setPreviewPostProcessConfig(flags, e.previewPostProcessDeblockingLevel, e.previewPostProcessDeblockingLevelSet, noiseLevel)
}

// SetPreviewPostProcessConfig changes the full encoder preview postprocess
// configuration at runtime, including deblocking strength. flags must be a
// combination of PostProcess* constants; deblockingLevel and noiseLevel must
// be in [0, 16]. The update is atomic.
func (e *VP8Encoder) SetPreviewPostProcessConfig(flags PostProcessFlag, deblockingLevel int, noiseLevel int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	return e.setPreviewPostProcessConfig(flags, deblockingLevel, true, noiseLevel)
}

func (e *VP8Encoder) setPreviewPostProcessConfig(flags PostProcessFlag, deblockingLevel int, deblockingLevelSet bool, noiseLevel int) error {
	if err := validateVP8PostProcessConfig(flags, deblockingLevel, deblockingLevelSet, noiseLevel); err != nil {
		return err
	}
	if e.previewFrameValid {
		if err := e.ensurePreviewPostProcessState(flags, noiseLevel); err != nil {
			return err
		}
	}
	e.previewPostProcessFlags = flags
	e.previewPostProcessDeblockingLevel = deblockingLevel
	e.previewPostProcessDeblockingLevelSet = deblockingLevelSet
	e.previewPostProcessNoiseLevel = noiseLevel
	e.previewPostprocessedValid = false
	return nil
}

// PreviewFrame returns the encoder's most recent preview image, matching
// libvpx's vpx_codec_get_preview_frame contract. The returned image aliases
// encoder-owned storage and remains valid until the next EncodeInto,
// FlushInto, Reset, Close, or preview postprocess configuration call.
// Hidden alt-ref refresh frames return ok=false, mirroring
// vp8_get_preview_raw_frame's refresh_alt_ref_frame guard.
func (e *VP8Encoder) PreviewFrame() (Image, bool) {
	if e == nil || e.closed || !e.previewFrameValid || e.previewFrameSuppressed {
		return Image{}, false
	}
	if e.previewPostProcessFlags == 0 {
		return publicImageFromVP8(&e.current.Img), true
	}
	if e.previewPostprocessedValid {
		return publicImageFromVP8(&e.preview.Img), true
	}
	if err := e.applyPreviewPostProcess(); err != nil {
		return Image{}, false
	}
	e.previewPostprocessedValid = true
	return publicImageFromVP8(&e.preview.Img), true
}

// CopyPreviewFrame copies the most recent encoder preview into dst. It returns
// ok=false with nil error when libvpx would return no preview image. dst must
// match the encoder dimensions and provide valid I420 strides.
func (e *VP8Encoder) CopyPreviewFrame(dst *Image) (bool, error) {
	if e == nil || e.closed {
		return false, ErrClosed
	}
	img, ok := e.PreviewFrame()
	if !ok {
		return false, nil
	}
	if dst == nil || !dst.validForEncode(img.Width, img.Height) {
		return false, ErrInvalidConfig
	}
	copyPublicPreviewImage(dst, img)
	return true, nil
}

func validateVP8PostProcessConfig(flags PostProcessFlag, deblockingLevel int, deblockingLevelSet bool, noiseLevel int) error {
	return validateDecoderOptions(DecoderOptions{
		PostProcessFlags:              flags,
		PostProcessDeblockingLevel:    deblockingLevel,
		PostProcessDeblockingLevelSet: deblockingLevelSet,
		PostProcessNoiseLevel:         noiseLevel,
	})
}

func (e *VP8Encoder) effectivePreviewPostProcessDeblockingLevel() int {
	if e.previewPostProcessDeblockingLevelSet || e.previewPostProcessDeblockingLevel != 0 {
		return e.previewPostProcessDeblockingLevel
	}
	return vp8dec.DefaultPostProcessDeblockingLevel
}

func (e *VP8Encoder) ensurePreviewPostProcessState(flags PostProcessFlag, noiseLevel int) error {
	if flags&PostProcessMFQE != 0 {
		if err := e.previewPostprocState.EnsureMFQE(e.opts.Width, e.opts.Height); err != nil {
			return ErrInvalidConfig
		}
	}
	if flags&PostProcessAddNoise != 0 && noiseLevel > 0 {
		e.previewPostprocState.EnsureNoise(e.opts.Width)
	}
	return nil
}

func (e *VP8Encoder) applyPreviewPostProcess() error {
	if err := e.ensurePreviewPostProcessState(e.previewPostProcessFlags, e.previewPostProcessNoiseLevel); err != nil {
		return err
	}
	if err := e.preview.Resize(e.opts.Width, e.opts.Height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	cols := geometry.MacroblockCols(e.opts.Width)
	rows := geometry.MacroblockRows(e.opts.Height)
	required := rows * cols
	if cols <= 0 || len(e.reconstructModes) < required {
		return ErrInvalidConfig
	}
	e.previewPostprocScratch = buffers.EnsureLen(e.previewPostprocScratch, cols*24)
	opts := vp8dec.PostProcessOptions{
		Deblock:         e.previewPostProcessFlags&PostProcessDeblock != 0,
		Demacroblock:    e.previewPostProcessFlags&PostProcessDemacroblock != 0,
		MFQE:            e.previewPostProcessFlags&PostProcessMFQE != 0,
		AddNoise:        e.previewPostProcessFlags&PostProcessAddNoise != 0 && e.previewPostProcessNoiseLevel > 0,
		DeblockingLevel: e.effectivePreviewPostProcessDeblockingLevel(),
		NoiseLevel:      e.previewPostProcessNoiseLevel,
		BaseQIndex:      e.previewBaseQIndex,
		CurrentFrame:    e.previewFrameNumber,
		KeyFrame:        e.previewFrameType == vp8common.KeyFrame,
	}
	if err := vp8dec.ApplyPostProcessWithOptions(&e.current.Img, &e.preview, rows, cols, e.reconstructModes[:required], e.previewLoopFilterLevel, e.previewPostprocScratch, opts, &e.previewPostprocState); err != nil {
		return ErrInvalidConfig
	}
	return nil
}

func (e *VP8Encoder) setPreviewFrame(frameType vp8common.FrameType, baseQIndex int, loopFilterLevel uint8, suppressed bool) {
	e.previewFrameValid = true
	e.previewFrameSuppressed = suppressed
	e.previewPostprocessedValid = false
	e.previewFrameType = frameType
	e.previewBaseQIndex = baseQIndex
	e.previewLoopFilterLevel = loopFilterLevel
	e.previewFrameNumber = int(e.frameCount)
}

func (e *VP8Encoder) clearPreviewFrame() {
	e.previewFrameValid = false
	e.previewFrameSuppressed = false
	e.previewPostprocessedValid = false
	e.previewFrameType = 0
	e.previewBaseQIndex = 0
	e.previewLoopFilterLevel = 0
	e.previewFrameNumber = 0
}

func (e *VP8Encoder) fillPreviewInterModesFromPacketModes(required int) {
	if required <= 0 || len(e.interFrameModes) < required || len(e.reconstructModes) < required {
		return
	}
	for i := range required {
		vp8enc.ConvertInterFrameMode(&e.interFrameModes[i], &e.reconstructModes[i])
	}
}

func copyPublicPreviewImage(dst *Image, src Image) {
	buffers.CopyPlane(dst.Y, dst.YStride, src.Y, src.YStride, src.Width, src.Height)
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	buffers.CopyPlane(dst.U, dst.UStride, src.U, src.UStride, uvWidth, uvHeight)
	buffers.CopyPlane(dst.V, dst.VStride, src.V, src.VStride, uvWidth, uvHeight)
}
