package govpx

import (
	"errors"
	"image"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// Encode is the alloc-returning wrapper around EncodeInto.
func (e *VP9Encoder) Encode(img *image.YCbCr) ([]byte, error) {
	return e.EncodeWithFlags(img, 0)
}

// EncodeWithFlags is the alloc-returning wrapper around EncodeIntoWithFlags.
func (e *VP9Encoder) EncodeWithFlags(img *image.YCbCr, flags EncodeFlags) ([]byte, error) {
	if e == nil || e.closed {
		return nil, ErrClosed
	}
	return e.encodeVP9Allocating(img, flags, false)
}

// EncodeIntraOnlyFrame is the allocating wrapper around
// EncodeIntraOnlyFrameInto.
func (e *VP9Encoder) EncodeIntraOnlyFrame(img *image.YCbCr, flags EncodeFlags) ([]byte, error) {
	if e == nil || e.closed {
		return nil, ErrClosed
	}
	return e.encodeVP9Allocating(img, flags, true)
}

func (e *VP9Encoder) encodeVP9Allocating(img *image.YCbCr, flags EncodeFlags, intraOnly bool) ([]byte, error) {
	size, err := vp9AllocatingEncodeBufferSize(e.opts.Width, e.opts.Height)
	if err != nil {
		return nil, err
	}
	for {
		dst := make([]byte, size)
		var n int
		if intraOnly {
			n, err = e.EncodeIntraOnlyFrameInto(img, dst, flags)
		} else {
			n, err = e.EncodeIntoWithFlags(img, dst, flags)
		}
		if err == nil {
			out := make([]byte, n)
			copy(out, dst[:n])
			return out, nil
		}
		if !vp9EncodeOutputBufferFull(err) {
			return nil, err
		}
		maxInt := int(^uint(0) >> 1)
		if size > maxInt/2 {
			return nil, err
		}
		size *= 2
	}
}

func vp9AllocatingEncodeBufferSize(width, height int) (int, error) {
	if width <= 0 || height <= 0 {
		return 0, ErrInvalidConfig
	}
	maxInt := int(^uint(0) >> 1)
	if width > maxInt/height {
		return 0, ErrInvalidConfig
	}
	y := width * height
	uvWidth := (width + 1) / 2
	uvHeight := (height + 1) / 2
	if uvWidth > maxInt/uvHeight {
		return 0, ErrInvalidConfig
	}
	uv := uvWidth * uvHeight
	if uv > (maxInt-y)/2 {
		return 0, ErrInvalidConfig
	}
	raw420 := y + 2*uv
	const headerSlack = 4096
	if raw420 > (maxInt-headerSlack)/4 {
		return 0, ErrInvalidConfig
	}
	size := max(headerSlack+raw420*4, 65536)
	return size, nil
}

func vp9EncodeOutputBufferFull(err error) bool {
	return errors.Is(err, ErrBufferTooSmall) ||
		errors.Is(err, encoder.ErrPackBufferFull) ||
		errors.Is(err, encoder.ErrTileBufferFull) ||
		errors.Is(err, bitstream.ErrBufferOverflow)
}

// EncodeShowExistingFrameInto writes a VP9 show_existing_frame packet for an
// already refreshed reference slot. The packet has no source image, compressed
// header, or tile body; decoders display the referenced slot directly. Slot must
// be in [0, 7] and valid in the encoder's current VP9 reference map.
func (e *VP9Encoder) EncodeShowExistingFrameInto(dst []byte, slot uint8) (int, error) {
	if e == nil || e.closed {
		return 0, ErrClosed
	}
	if slot >= common.RefFrames {
		return 0, ErrInvalidConfig
	}
	if !e.refValid[slot] || !e.refFrames[slot].valid {
		return 0, ErrInvalidConfig
	}
	if len(dst) == 0 {
		return 0, ErrBufferTooSmall
	}
	var bw encoder.BitWriter
	bw.Init(dst)
	return encoder.WriteShowExistingFrameHeader(&bw, common.Profile0, slot), nil
}

// EncodeShowExistingFrame is the allocating wrapper around
// EncodeShowExistingFrameInto.
func (e *VP9Encoder) EncodeShowExistingFrame(slot uint8) ([]byte, error) {
	if e == nil || e.closed {
		return nil, ErrClosed
	}
	dst := make([]byte, 1)
	n, err := e.EncodeShowExistingFrameInto(dst, slot)
	if err != nil {
		return nil, err
	}
	return dst[:n], nil
}

// Close releases internal state and marks the encoder as no longer
// usable. Subsequent Encode / EncodeInto calls return [ErrClosed].
// Close is idempotent: calling it on an already-closed encoder returns
// [ErrClosed] without re-tearing-down the worker pools.
func (e *VP9Encoder) Close() error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if vp9OracleTraceBuild {
		e.resetVP9OracleTraceState()
	}
	if e.vp9TilePool != nil {
		e.vp9TilePool.shutdownPool()
		e.vp9TilePool = nil
	}
	if e.frameParallel != nil {
		e.frameParallel.release()
		e.frameParallel = nil
	}
	e.closed = true
	return nil
}

// Codec reports the codec this encoder targets.
func (e *VP9Encoder) Codec() Codec { return CodecVP9 }
